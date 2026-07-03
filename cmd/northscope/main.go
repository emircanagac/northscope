package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emircanagac/northscope/internal/k8s"
	"github.com/emircanagac/northscope/internal/server"
	"github.com/emircanagac/northscope/ui"
)

func main() {
	var (
		addr       = flag.String("addr", ":8080", "HTTP listen address")
		kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file. Defaults to in-cluster config, then ~/.kube/config.")
	)
	flag.Parse()

	log.Printf("NorthScope starting")

	staticFS, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		log.Fatalf("load embedded ui: %v", err)
	}

	config, err := k8s.BuildConfig(*kubeconfig)
	if err != nil {
		log.Fatalf("load kubernetes config: %v", err)
	}

	watcher, err := k8s.NewWatcher(config)
	if err != nil {
		log.Fatalf("create kubernetes watcher: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("NorthScope application started; waiting for Kubernetes topology cache")

	go func() {
		if err := watcher.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("kubernetes watcher stopped: %v", err)
		}
	}()

	httpServer := server.New(*addr, watcher, staticFS)

	go func() {
		log.Printf("NorthScope started: HTTP server listening on %s; readiness endpoint is /readyz", *addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server failed: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown failed: %v", err)
	}
}
