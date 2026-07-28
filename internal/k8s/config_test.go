package k8s

import (
	"context"
	"testing"

	"k8s.io/client-go/rest"
)

type recordingWarningHandler struct {
	messages []string
}

func (h *recordingWarningHandler) HandleWarningHeaderWithContext(_ context.Context, _ int, _ string, text string) {
	h.messages = append(h.messages, text)
}

func TestTuneClientConfigSetsDefaults(t *testing.T) {
	cfg := &rest.Config{}

	tuneClientConfig(cfg)

	if cfg.QPS != DefaultKubeClientQPS {
		t.Fatalf("expected QPS %v, got %v", DefaultKubeClientQPS, cfg.QPS)
	}
	if cfg.Burst != DefaultKubeClientBurst {
		t.Fatalf("expected Burst %d, got %d", DefaultKubeClientBurst, cfg.Burst)
	}
}

func TestTuneClientConfigKeepsExplicitValues(t *testing.T) {
	cfg := &rest.Config{
		QPS:   12,
		Burst: 34,
	}

	tuneClientConfig(cfg)

	if cfg.QPS != 12 {
		t.Fatalf("expected explicit QPS to be preserved, got %v", cfg.QPS)
	}
	if cfg.Burst != 34 {
		t.Fatalf("expected explicit Burst to be preserved, got %d", cfg.Burst)
	}
}

func TestKubernetesWarningHandlerKeepsLegacyEndpointsFallbackQuiet(t *testing.T) {
	delegate := &recordingWarningHandler{}
	notices := 0
	handler := &kubernetesWarningHandler{
		delegate: delegate,
		legacyEndpointsLog: func() {
			notices++
		},
	}
	const warning = "v1 Endpoints is deprecated in v1.33+; use discovery.k8s.io/v1 EndpointSlice"

	handler.HandleWarningHeaderWithContext(context.Background(), 299, "kube-apiserver", warning)
	handler.HandleWarningHeaderWithContext(context.Background(), 299, "kube-apiserver", warning)

	if notices != 1 {
		t.Fatalf("expected one legacy Endpoints compatibility notice, got %d", notices)
	}
	if len(delegate.messages) != 0 {
		t.Fatalf("expected legacy Endpoints warning to be handled locally, got %#v", delegate.messages)
	}
}

func TestKubernetesWarningHandlerForwardsOtherWarnings(t *testing.T) {
	delegate := &recordingWarningHandler{}
	handler := &kubernetesWarningHandler{
		delegate:           delegate,
		legacyEndpointsLog: func() {},
	}
	const warning = "an unrelated Kubernetes API warning"

	handler.HandleWarningHeaderWithContext(context.Background(), 299, "kube-apiserver", warning)

	if len(delegate.messages) != 1 || delegate.messages[0] != warning {
		t.Fatalf("expected unrelated warning to be forwarded, got %#v", delegate.messages)
	}
}

func TestTuneClientConfigPreservesConfiguredWarningHandler(t *testing.T) {
	delegate := &recordingWarningHandler{}
	cfg := &rest.Config{WarningHandlerWithContext: delegate}

	tuneClientConfig(cfg)

	handler, ok := cfg.WarningHandlerWithContext.(*kubernetesWarningHandler)
	if !ok {
		t.Fatalf("expected NorthScope warning handler, got %T", cfg.WarningHandlerWithContext)
	}
	const warning = "an unrelated Kubernetes API warning"
	handler.HandleWarningHeaderWithContext(context.Background(), 299, "kube-apiserver", warning)
	if len(delegate.messages) != 1 || delegate.messages[0] != warning {
		t.Fatalf("expected configured warning handler to remain in the chain, got %#v", delegate.messages)
	}
}
