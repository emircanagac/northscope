package k8s

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	DefaultKubeClientQPS   float32 = 30
	DefaultKubeClientBurst int     = 60
)

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
}
