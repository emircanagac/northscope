package server

import (
	"context"
	"encoding/json"
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
