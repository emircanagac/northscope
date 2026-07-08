package k8s

import (
	"testing"

	"k8s.io/client-go/rest"
)

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
