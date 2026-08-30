package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuebuy/cicd-platform/backend/internal/config"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
)

// newRuntimeProvider keeps the choice between the local fixture and a real
// Kubernetes client at the process boundary. The rest of the API only depends
// on runtime.Provider, so switching modes does not change the UI contract.
func newRuntimeProvider(cfg config.Config) (runtime.Provider, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.RuntimeProvider))
	if mode == "" {
		mode = "auto"
	}
	if mode == "demo" || (mode == "auto" && cfg.DemoMode) {
		return runtime.NewDemoProvider(), nil
	}
	if mode != "auto" && mode != "kubernetes" && mode != "k8s" {
		return nil, fmt.Errorf("unsupported CICD_RUNTIME_PROVIDER %q; use demo, auto, or kubernetes", cfg.RuntimeProvider)
	}

	options := make([]runtime.KubernetesOption, 0, 2)
	if cfg.KubeTimeout > 0 {
		options = append(options, runtime.WithKubernetesTimeout(cfg.KubeTimeout))
	}
	if cfg.KubeRolloutTimeout > 0 {
		options = append(options, runtime.WithKubernetesRolloutTimeout(cfg.KubeRolloutTimeout))
	}
	if strings.EqualFold(strings.TrimSpace(cfg.KubeProjectLabel), "off") {
		// Existing namespaces may not have TTP's project label yet. In this
		// explicit mode, the namespace is the project boundary for observation.
		options = append(options, runtime.WithProjectLabelKey(""))
	} else if strings.TrimSpace(cfg.KubeProjectLabel) != "" {
		options = append(options, runtime.WithProjectLabelKey(strings.TrimSpace(cfg.KubeProjectLabel)))
	}
	provider := runtime.NewKubernetesProvider(options...)
	clusterID := strings.TrimSpace(cfg.KubeClusterID)
	if clusterID == "" {
		clusterID = "default-cluster"
	}

	kubeconfig := strings.TrimSpace(cfg.KubeconfigPath)
	if kubeconfig == "" {
		kubeconfig = strings.TrimSpace(os.Getenv("KUBECONFIG"))
	}
	if kubeconfig != "" {
		if err := provider.RegisterKubeconfigWithContext(clusterID, kubeconfig, strings.TrimSpace(cfg.KubeContext)); err != nil {
			return nil, fmt.Errorf("register Kubernetes cluster %q: %w", clusterID, err)
		}
		return provider, nil
	}
	if cfg.KubeInCluster || inClusterEnvironment() {
		if err := provider.RegisterInCluster(clusterID); err != nil {
			return nil, fmt.Errorf("register in-cluster Kubernetes client %q: %w", clusterID, err)
		}
		return provider, nil
	}

	// A local kubeconfig is convenient for development, but only use it when it
	// actually exists. In production, an explicit path or in-cluster identity
	// makes the connection choice auditable.
	if home, err := os.UserHomeDir(); err == nil {
		defaultPath := filepath.Join(home, ".kube", "config")
		if _, statErr := os.Stat(defaultPath); statErr == nil {
			if err := provider.RegisterKubeconfigWithContext(clusterID, defaultPath, strings.TrimSpace(cfg.KubeContext)); err != nil {
				return nil, fmt.Errorf("register Kubernetes cluster %q: %w", clusterID, err)
			}
			return provider, nil
		}
	}
	return nil, fmt.Errorf("Kubernetes runtime is selected but no kubeconfig was found; set CICD_KUBECONFIG or CICD_KUBE_IN_CLUSTER=true")
}

func inClusterEnvironment() bool {
	return strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST")) != "" && strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_PORT")) != ""
}
