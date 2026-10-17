package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/emircanagac/northscope/internal/auth"
	"github.com/emircanagac/northscope/internal/k8s"
)

type Server struct {
	httpServer *http.Server
	watcher    *k8s.Watcher
	staticFS   fs.FS
	version    string
	upgrader   websocket.Upgrader
	auth       *auth.Authenticator
}

// Option configures optional Server behavior.
type Option func(*Server)

// WithAuth requires a NorthScope sign-in for the UI, API, and WebSocket.
func WithAuth(authenticator *auth.Authenticator) Option {
	return func(s *Server) {
		s.auth = authenticator
	}
}

const (
	websocketWriteWait  = 10 * time.Second
	websocketPongWait   = 60 * time.Second
	websocketPingPeriod = websocketPongWait * 9 / 10
)

func New(addr string, watcher *k8s.Watcher, staticFS fs.FS, version string, opts ...Option) *Server {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "dev"
	}

	s := &Server{
		watcher:  watcher,
		staticFS: staticFS,
		version:  version,
		upgrader: websocket.Upgrader{
			CheckOrigin: sameOrigin,
		},
	}
	for _, opt := range opts {
		opt(s)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/topology", s.handleTopology)
	mux.HandleFunc("/ws", s.handleTopologyStream)
	mux.HandleFunc("/ws/topology", s.handleTopologyStream)
	mux.Handle("/", s.staticHandler())

	var handler http.Handler = mux
	if s.auth != nil {
		mux.Handle(auth.LoginPath, s.auth.LoginHandler())
		mux.Handle(auth.LogoutPath, s.auth.LogoutHandler())
		handler = s.auth.Middleware(mux)
	}

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           securityHeaders(handler),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return s
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	return strings.EqualFold(u.Host, r.Host)
}

func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// handleReadyz reports whether the first topology snapshot exists. Watch
// errors after that do not fail readiness, because restarting or unrouting
// NorthScope does not fix RBAC or API server problems; ?verbose lists the
// degraded resources instead.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if _, verbose := r.URL.Query()["verbose"]; verbose {
		s.writeVerboseReadyz(w)
		return
	}
	if !s.watcher.Ready() {
		http.Error(w, "topology snapshot is not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeVerboseReadyz(w http.ResponseWriter) {
	status := s.watcher.Status()
	// Optional Gateway API and F5 informers never gate readiness.
	var unsynced []string
	for _, resource := range status.Resources {
		if !resource.Optional && !resource.Synced {
			unsynced = append(unsynced, resource.Resource)
		}
	}
	var body strings.Builder
	if len(unsynced) == 0 {
		body.WriteString("[+]informer-sync ok\n")
	} else {
		fmt.Fprintf(&body, "[-]informer-sync waiting for: %s\n", strings.Join(unsynced, ", "))
	}
	if status.Ready {
		fmt.Fprintf(&body, "[+]topology-snapshot ok (v%d)\n", status.SnapshotVersion)
	} else {
		body.WriteString("[-]topology-snapshot not generated yet\n")
	}
	if len(status.Degraded) == 0 {
		body.WriteString("[+]watch-health ok\n")
	} else {
		fmt.Fprintf(&body, "[!]watch-health degraded: %s (does not affect readiness; see /api/status)\n", strings.Join(status.Degraded, ", "))
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !status.Ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		body.WriteString("readyz check failed\n")
	} else {
		body.WriteString("readyz check passed\n")
	}
	_, _ = io.WriteString(w, body.String())
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.watcher.Status())
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.watcher.Latest())
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Version string `json:"version"`
	}{Version: s.version})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	response := struct {
		AuthEnabled bool       `json:"authEnabled"`
		Username    string     `json:"username,omitempty"`
		ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	}{}
	if s.auth != nil {
		session, ok := auth.SessionFromContext(r.Context())
		if !ok {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		response.AuthEnabled = true
		response.Username = session.Username
		response.ExpiresAt = &session.ExpiresAt
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := s.watcher.Metrics()
	ready := 0
	if metrics.Ready {
		ready = 1
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, "# HELP northscope_ready Whether NorthScope has generated at least one topology snapshot.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_ready gauge\n")
	_, _ = fmt.Fprintf(w, "northscope_ready %d\n", ready)
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_version Latest topology snapshot version.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_version gauge\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_version %d\n", metrics.SnapshotVersion)
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_nodes Current topology node count.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_nodes gauge\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_nodes %d\n", metrics.SnapshotNodes)
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_edges Current topology edge count.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_edges gauge\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_edges %d\n", metrics.SnapshotEdges)
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_builds_total Successful topology snapshot builds.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_builds_total counter\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_builds_total %d\n", metrics.SnapshotBuildsTotal)
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_build_errors_total Failed topology snapshot builds.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_build_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_build_errors_total %d\n", metrics.SnapshotBuildErrorsTotal)
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_publishes_total Topology snapshots published to clients.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_publishes_total counter\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_publishes_total %d\n", metrics.SnapshotPublishesTotal)
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_unchanged_total Successful builds skipped because topology and watch health were unchanged.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_unchanged_total counter\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_unchanged_total %d\n", metrics.SnapshotUnchangedTotal)
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_build_duration_seconds Duration of the last topology snapshot build.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_build_duration_seconds gauge\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_build_duration_seconds %.6f\n", metrics.LastSnapshotBuildDurationSeconds)
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_generated_timestamp_seconds Unix time when the published topology snapshot was generated.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_generated_timestamp_seconds gauge\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_generated_timestamp_seconds %s\n", unixSeconds(metrics.SnapshotGeneratedAt))
	_, _ = fmt.Fprintf(w, "# HELP northscope_websocket_clients Current topology websocket subscribers.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_websocket_clients gauge\n")
	_, _ = fmt.Fprintf(w, "northscope_websocket_clients %d\n", metrics.WebsocketSubscribers)

	_, _ = fmt.Fprintf(w, "# HELP northscope_informer_synced Whether the informer cache for a watched resource completed its initial list.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_informer_synced gauge\n")
	for _, resource := range metrics.Resources {
		_, _ = fmt.Fprintf(w, "northscope_informer_synced{resource=\"%s\"} %d\n", labelValue(resource.Resource), boolValue(resource.Synced))
	}
	_, _ = fmt.Fprintf(w, "# HELP northscope_informer_degraded Whether the latest list or watch for a resource failed, so its cached data may be stale.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_informer_degraded gauge\n")
	for _, resource := range metrics.Resources {
		_, _ = fmt.Fprintf(w, "northscope_informer_degraded{resource=\"%s\"} %d\n", labelValue(resource.Resource), boolValue(resource.Degraded))
	}
	_, _ = fmt.Fprintf(w, "# HELP northscope_watch_errors_total Failed list and watch attempts per watched resource.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_watch_errors_total counter\n")
	for _, resource := range metrics.Resources {
		_, _ = fmt.Fprintf(w, "northscope_watch_errors_total{resource=\"%s\"} %d\n", labelValue(resource.Resource), resource.ErrorCount)
	}
	_, _ = fmt.Fprintf(w, "# HELP northscope_last_watch_success_timestamp_seconds Unix time of the latest successful list, watch start, or watch event per resource.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_last_watch_success_timestamp_seconds gauge\n")
	for _, resource := range metrics.Resources {
		lastSuccess := time.Time{}
		if resource.LastSuccessAt != nil {
			lastSuccess = *resource.LastSuccessAt
		}
		_, _ = fmt.Fprintf(w, "northscope_last_watch_success_timestamp_seconds{resource=\"%s\"} %s\n", labelValue(resource.Resource), unixSeconds(lastSuccess))
	}
}

var labelValueEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func labelValue(value string) string {
	return labelValueEscaper.Replace(value)
}

func boolValue(value bool) int {
	if value {
		return 1
	}
	return 0
}

func unixSeconds(value time.Time) string {
	if value.IsZero() {
		return "0"
	}
	return strconv.FormatFloat(float64(value.UnixMilli())/1000, 'f', 3, 64)
}

func (s *Server) handleTopologyStream(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}
	defer func() { _ = conn.Close() }()

	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(websocketPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(websocketPongWait))
	})

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	updates, unsubscribe := s.watcher.Subscribe(8)
	defer unsubscribe()
	pingTicker := time.NewTicker(websocketPingPeriod)
	defer pingTicker.Stop()

	// An open stream must not outlive the session that authorized it.
	var sessionExpired <-chan time.Time
	if session, ok := auth.SessionFromContext(r.Context()); ok {
		expiryTimer := time.NewTimer(time.Until(session.ExpiresAt))
		defer expiryTimer.Stop()
		sessionExpired = expiryTimer.C
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-readDone:
			return
		case <-sessionExpired:
			closeMessage := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session expired")
			_ = conn.WriteControl(websocket.CloseMessage, closeMessage, time.Now().Add(websocketWriteWait))
			return
		case <-pingTicker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(websocketWriteWait)); err != nil {
				return
			}
		case snapshot, ok := <-updates:
			if !ok {
				return
			}
			if err := conn.SetWriteDeadline(time.Now().Add(websocketWriteWait)); err != nil {
				return
			}
			if err := conn.WriteJSON(snapshot); err != nil {
				return
			}
		}
	}
}

func (s *Server) staticHandler() http.HandlerFunc {
	fileServer := http.FileServer(http.FS(s.staticFS))

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(s.staticFS, path); err != nil {
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	}
}
