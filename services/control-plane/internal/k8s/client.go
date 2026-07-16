package k8s

import (
	"fmt"
	"os"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Clients bundles the k8s clients the control plane needs.
type Clients struct {
	Config  *rest.Config
	Dynamic dynamic.Interface
	Typed   kubernetes.Interface
}

// New builds both dynamic and typed clients, preferring in-cluster config and
// falling back to a kubeconfig file (SPINUP_KUBECONFIG or default paths).
func New(kubeconfig string) (*Clients, error) {
	cfg, err := restConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("typed client: %w", err)
	}
	return &Clients{Config: cfg, Dynamic: dyn, Typed: typed}, nil
}

func restConfig(kubeconfig string) (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loader.ExplicitPath = kubeconfig
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w (SPINUP_KUBECONFIG=%q, KUBECONFIG=%q)", err, kubeconfig, os.Getenv("KUBECONFIG"))
	}
	return cfg, nil
}
