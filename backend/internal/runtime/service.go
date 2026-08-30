package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrClusterManagementUnsupported = errors.New("runtime provider does not support cluster management")
	ErrClusterRegistrationFailed    = errors.New("runtime cluster registration failed")
	ErrReleaseDeploymentUnsupported = errors.New("runtime provider does not support release deployment")
)

// Service is the application-facing facade over a runtime provider. Keeping
// this small makes it straightforward to add authorization/audit context in a
// higher layer without coupling handlers to Kubernetes types.
type Service struct{ provider Provider }

func NewService(provider Provider) *Service {
	if provider == nil {
		provider = NewDemoProvider()
	}
	return &Service{provider: provider}
}

func (s *Service) ListPods(ctx context.Context, clusterID, projectID string) ([]Pod, error) {
	return s.provider.ListPods(ctx, clusterID, projectID)
}

// ListPodsInNamespace narrows a project query to its configured namespace
// when the provider supports it. The fallback keeps older providers usable
// while the Kubernetes implementation adopts namespace-aware queries.
func (s *Service) ListPodsInNamespace(ctx context.Context, clusterID, namespace, projectID string) ([]Pod, error) {
	provider, ok := s.provider.(interface {
		ListPodsInNamespace(context.Context, string, string, string) ([]Pod, error)
	})
	if ok {
		return provider.ListPodsInNamespace(ctx, clusterID, namespace, projectID)
	}
	items, err := s.provider.ListPods(ctx, clusterID, projectID)
	if err != nil {
		return nil, err
	}
	if namespace == "" {
		return items, nil
	}
	filtered := make([]Pod, 0, len(items))
	for _, item := range items {
		if item.Namespace == namespace {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}
func (s *Service) GetPod(ctx context.Context, ref PodRef) (PodDetail, error) {
	return s.provider.GetPod(ctx, ref)
}
func (s *Service) GetPodLogs(ctx context.Context, request PodLogRequest) (string, error) {
	return s.provider.GetPodLogs(ctx, request)
}
func (s *Service) UpdatePodConfig(ctx context.Context, ref PodRef, update PodConfigUpdate) (PodDetail, error) {
	return s.provider.UpdatePodConfig(ctx, ref, update)
}

// DeployRelease delegates a release deployment to the runtime provider. A
// provider that can only observe a cluster must fail explicitly; silently
// treating a skipped write as a successful release is unsafe.
func (s *Service) DeployRelease(ctx context.Context, deployment ReleaseDeployment) error {
	if s == nil || s.provider == nil {
		return ErrReleaseDeploymentUnsupported
	}
	deployer, ok := s.provider.(ReleaseDeployer)
	if !ok {
		return ErrReleaseDeploymentUnsupported
	}
	return deployer.DeployRelease(ctx, deployment)
}

func (s *Service) GetClusterMetrics(ctx context.Context, clusterID string) (ClusterMetrics, error) {
	return s.provider.GetClusterMetrics(ctx, clusterID)
}

// RegisterCluster installs a new connection without exposing provider-specific
// client details to the HTTP layer.
func (s *Service) RegisterCluster(ctx context.Context, clusterID, mode, kubeconfigPath, kubeContext string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.provider == nil {
		return ErrClusterManagementUnsupported
	}
	registrar, ok := s.provider.(ClusterRegistrar)
	if !ok {
		return ErrClusterManagementUnsupported
	}
	var err error
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "kubeconfig":
		err = registrar.RegisterKubeconfigWithContext(clusterID, kubeconfigPath, kubeContext)
	case "in_cluster":
		err = registrar.RegisterInCluster(clusterID)
	default:
		return ErrClusterManagementUnsupported
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrClusterRegistrationFailed, err)
	}
	return nil
}

func (s *Service) CheckCluster(ctx context.Context, clusterID string) (ClusterConnection, error) {
	if s == nil || s.provider == nil {
		return ClusterConnection{}, ErrClusterManagementUnsupported
	}
	checker, ok := s.provider.(ClusterChecker)
	if !ok {
		return ClusterConnection{}, ErrClusterManagementUnsupported
	}
	return checker.CheckCluster(ctx, clusterID)
}
func (s *Service) GetProjectMetrics(ctx context.Context, clusterID, projectID string) (ProjectMetrics, error) {
	return s.provider.GetProjectMetrics(ctx, clusterID, projectID)
}

// GetProjectMetricsInNamespace is the namespace-aware counterpart used by
// project pages. Providers without a native implementation still get a
// correctly scoped count from their Pod list; resource metrics remain the
// provider's responsibility.
func (s *Service) GetProjectMetricsInNamespace(ctx context.Context, clusterID, namespace, projectID string) (ProjectMetrics, error) {
	provider, ok := s.provider.(interface {
		GetProjectMetricsInNamespace(context.Context, string, string, string) (ProjectMetrics, error)
	})
	if ok {
		return provider.GetProjectMetricsInNamespace(ctx, clusterID, namespace, projectID)
	}
	items, err := s.ListPodsInNamespace(ctx, clusterID, namespace, projectID)
	if err != nil {
		return ProjectMetrics{}, err
	}
	healthy := 0
	for _, item := range items {
		if item.Ready {
			healthy++
		}
	}
	base, err := s.provider.GetProjectMetrics(ctx, clusterID, projectID)
	if err != nil {
		// A known cluster with an empty namespace is a valid pre-release state.
		// Preserve a not-found error for providers that return it when there are
		// actually no Pods, but expose a zero snapshot to the project dashboard.
		if len(items) == 0 && errors.Is(err, ErrProjectNotFound) {
			return ProjectMetrics{
				ClusterID:        clusterID,
				ProjectID:        projectID,
				MetricsAvailable: false,
				MetricsMessage:   "当前命名空间暂无运行中的 Pod",
				ObservedAt:       time.Now().UTC(),
			}, nil
		}
		return ProjectMetrics{}, err
	}
	base.ProjectID = projectID
	base.PodCount = len(items)
	base.HealthyPodCount = healthy
	return base, nil
}
