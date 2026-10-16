package k8s

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	DefaultKubeClientQPS   float32 = 30
	DefaultKubeClientBurst int     = 60
)

type warningHandlerAdapter struct {
	handler rest.WarningHandler
}

func (h warningHandlerAdapter) HandleWarningHeaderWithContext(_ context.Context, code int, agent, text string) {
	h.handler.HandleWarningHeader(code, agent, text)
}

type kubernetesWarningHandler struct {
	delegate            rest.WarningHandlerWithContext
	legacyEndpointsOnce sync.Once
	legacyEndpointsLog  func()
}

func (h *kubernetesWarningHandler) HandleWarningHeaderWithContext(ctx context.Context, code int, agent, text string) {
	if isLegacyEndpointsDeprecationWarning(text) {
		h.legacyEndpointsOnce.Do(h.legacyEndpointsLog)
		return
	}
	h.delegate.HandleWarningHeaderWithContext(ctx, code, agent, text)
}

func BuildConfig(kubeconfig string) (*rest.Config, error) {
	var cfg *rest.Config
	var err error

	if kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
		tuneClientConfig(cfg)
		return cfg, nil
	}

	cfg, err = rest.InClusterConfig()
	if err == nil {
		tuneClientConfig(cfg)
		return cfg, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cfg, err = clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
	if err != nil {
		return nil, err
	}
	tuneClientConfig(cfg)
	return cfg, nil
}

func tuneClientConfig(cfg *rest.Config) {
	if cfg.QPS <= 0 {
		cfg.QPS = DefaultKubeClientQPS
	}
	if cfg.Burst <= 0 {
		cfg.Burst = DefaultKubeClientBurst
	}
	if _, configured := cfg.WarningHandlerWithContext.(*kubernetesWarningHandler); configured {
		return
	}

	delegate := cfg.WarningHandlerWithContext
	if delegate == nil && cfg.WarningHandler != nil {
		delegate = warningHandlerAdapter{handler: cfg.WarningHandler}
	}
	if delegate == nil {
		delegate = rest.WarningLogger{}
	}

	cfg.WarningHandler = nil
	cfg.WarningHandlerWithContext = &kubernetesWarningHandler{
		delegate: delegate,
		legacyEndpointsLog: func() {
			log.Printf("NorthScope compatibility: EndpointSlice is preferred; legacy Endpoints fallback remains enabled")
		},
	}
}

func isLegacyEndpointsDeprecationWarning(text string) bool {
	normalized := strings.ToLower(text)
	return strings.Contains(normalized, "endpoints is deprecated") &&
		strings.Contains(normalized, "endpointslice")
}
