package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"k8s.io/klog/v2"

	"github.com/emircanagac/northscope/internal/auth"
	"github.com/emircanagac/northscope/internal/buildinfo"
	"github.com/emircanagac/northscope/internal/k8s"
	"github.com/emircanagac/northscope/internal/server"
	"github.com/emircanagac/northscope/ui"
)

func main() {
	var (
		addr        = flag.String("addr", ":8080", "HTTP listen address")
		kubeconfig  = flag.String("kubeconfig", "", "Path to kubeconfig file. Defaults to in-cluster config, then ~/.kube/config.")
		namespace   = flag.String("namespace", "", "Watch one namespace. Empty watches all namespaces.")
		gatewayAPI  = flag.Bool("gateway-api", true, "Discover Gateway API resources when their CRDs are installed")
		f5          = flag.Bool("f5", true, "Discover F5 CIS custom resources when their CRDs are installed")
		showVersion = flag.Bool("version", false, "Print the NorthScope version and exit")
		logFormat   = flag.String("log-format", "text", "Log format: text, or json for one JSON object per line, including Kubernetes client messages")

		authDir            = flag.String("auth-dir", "", "Directory with username, password, and session-key files, such as a mounted Secret. Empty disables authentication.")
		authCookieSecure   = flag.String("auth-cookie-secure", "auto", "Session cookie Secure attribute: auto (when the request is HTTPS or X-Forwarded-Proto is https), true, or false")
		authProtectMetrics = flag.Bool("auth-protect-metrics", false, "Require a session for /metrics. Prometheus cannot sign in, so scrapes then fail.")
		authSessionTTL     = flag.Duration("auth-session-ttl", auth.DefaultSessionTTL, "How long a sign-in stays valid")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}
	if err := configureLogging(*logFormat, os.Stderr); err != nil {
		fatal("Configure logging failed", err)
	}

	slog.Info("NorthScope starting", "version", buildinfo.Version)

	var serverOptions []server.Option
	if *authDir == "" {
		slog.Warn("Authentication is disabled because -auth-dir is not set; anyone who can reach NorthScope can view cluster topology")
	} else {
		authenticator, err := newAuthenticator(*authDir, *authCookieSecure, *authProtectMetrics, *authSessionTTL)
		if err != nil {
			fatal("Configure authentication failed", err)
		}
		slog.Info("Authentication enabled",
			"user", authenticator.Username(), "cookieSecure", *authCookieSecure, "metricsProtected", *authProtectMetrics)
		serverOptions = append(serverOptions, server.WithAuth(authenticator))
	}

	staticFS, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		fatal("Load embedded UI failed", err)
	}

	config, err := k8s.BuildConfig(*kubeconfig)
	if err != nil {
		fatal("Load Kubernetes config failed", err)
	}

	watcher, err := k8s.NewWatcherWithOptions(config, k8s.WatcherOptions{
		GatewayAPI: *gatewayAPI,
		F5:         *f5,
		Namespace:  *namespace,
	})
	if err != nil {
		fatal("Create Kubernetes watcher failed", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := watcher.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Kubernetes watcher stopped", "error", err)
		}
	}()

	httpServer := server.New(*addr, watcher, staticFS, buildinfo.Version, serverOptions...)

	go func() {
		slog.Info("HTTP server listening", "addr", *addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("HTTP server failed", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP shutdown failed", "error", err)
	}
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

// configureLogging keeps the standard text log for "text". For "json" it sends
// NorthScope's slog and log package output and the Kubernetes client's klog
// output through one slog JSON handler, so log collectors can parse every line
// and read NorthScope's fields (resource, error, version, ...) as JSON keys.
func configureLogging(format string, output io.Writer) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		return nil
	case "json":
		logger := slog.New(slog.NewJSONHandler(output, nil))
		slog.SetDefault(logger)
		klog.SetSlogLogger(logger)
		return nil
	}
	return fmt.Errorf("invalid log format %q: use text or json", format)
}

func newAuthenticator(dir, cookieSecure string, protectMetrics bool, sessionTTL time.Duration) (*auth.Authenticator, error) {
	secureMode, err := auth.ParseCookieSecureMode(cookieSecure)
	if err != nil {
		return nil, err
	}
	creds, err := auth.LoadCredentials(dir)
	if err != nil {
		return nil, err
	}
	return auth.New(creds, auth.Options{
		CookieSecure:   secureMode,
		ProtectMetrics: protectMetrics,
		SessionTTL:     sessionTTL,
	})
}
