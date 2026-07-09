package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/emircanagac/northscope/internal/k8s"
)

type Server struct {
	httpServer *http.Server
	watcher    *k8s.Watcher
	staticFS   fs.FS
	upgrader   websocket.Upgrader
}

func New(addr string, watcher *k8s.Watcher, staticFS fs.FS) *Server {
	s := &Server{
		watcher:  watcher,
		staticFS: staticFS,
		upgrader: websocket.Upgrader{
			CheckOrigin: sameOrigin,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/api/topology", s.handleTopology)
	mux.HandleFunc("/ws", s.handleTopologyStream)
	mux.HandleFunc("/ws/topology", s.handleTopologyStream)
	mux.Handle("/", s.staticHandler())

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
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

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !s.watcher.Ready() {
		http.Error(w, "topology snapshot is not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.watcher.Latest())
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
	_, _ = fmt.Fprintf(w, "# HELP northscope_snapshot_build_duration_seconds Duration of the last topology snapshot build.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_snapshot_build_duration_seconds gauge\n")
	_, _ = fmt.Fprintf(w, "northscope_snapshot_build_duration_seconds %.6f\n", metrics.LastSnapshotBuildDurationSeconds)
	_, _ = fmt.Fprintf(w, "# HELP northscope_websocket_clients Current topology websocket subscribers.\n")
	_, _ = fmt.Fprintf(w, "# TYPE northscope_websocket_clients gauge\n")
	_, _ = fmt.Fprintf(w, "northscope_websocket_clients %d\n", metrics.WebsocketSubscribers)
}

func (s *Server) handleTopologyStream(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	updates, unsubscribe := s.watcher.Subscribe(8)
	defer unsubscribe()

	for {
		select {
		case <-r.Context().Done():
			return
		case snapshot, ok := <-updates:
			if !ok {
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
