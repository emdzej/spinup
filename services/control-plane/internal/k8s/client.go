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
// When kubecontext is non-empty it overrides the kubeconfig's current-context
// — useful for local dev where ~/.kube/config points at some other cluster
// (rancher-desktop, kind, …) but we want to talk to tve.
func New(kubeconfig, kubecontext string) (*Clients, error) {
	cfg, err := restConfig(kubeconfig, kubecontext)
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

func restConfig(kubeconfig, kubecontext string) (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loader.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if kubecontext != "" {
		overrides.CurrentContext = kubecontext
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w (SPINUP_KUBECONFIG=%q, KUBECONFIG=%q, SPINUP_KUBECONTEXT=%q)", err, kubeconfig, os.Getenv("KUBECONFIG"), kubecontext)
	}
	return cfg, nil
}
