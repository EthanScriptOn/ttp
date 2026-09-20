package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

var (
	ErrProviderNotConfigured        = errors.New("runtime provider is not configured")
	ErrClusterManagementUnsupported = errors.New("runtime provider does not support cluster management")
	ErrClusterRegistrationFailed    = errors.New("runtime cluster registration failed")
	ErrReleaseDeploymentUnsupported = errors.New("runtime provider does not support release deployment")
	ErrImageBuildUnsupported        = errors.New("image build and push provider is not configured")
)

// Service is the application-facing facade over a runtime provider. Keeping
// this small makes it straightforward to add authorization/audit context in a
// higher layer without coupling handlers to Kubernetes types.
type Service struct{ provider Provider }

func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

func (s *Service) ListPods(ctx context.Context, clusterID, projectID string) ([]Pod, error) {
	if s == nil || s.provider == nil {
		return nil, ErrProviderNotConfigured
	}
	return s.provider.ListPods(ctx, clusterID, projectID)
}

// ListPodsInNamespace narrows a project query to its configured namespace
// when the provider supports it. The fallback keeps older providers usable
// while the Kubernetes implementation adopts namespace-aware queries.
func (s *Service) ListPodsInNamespace(ctx context.Context, clusterID, namespace, projectID string) ([]Pod, error) {
	if s == nil || s.provider == nil {
		return nil, ErrProviderNotConfigured
	}
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
	if s == nil || s.provider == nil {
		return PodDetail{}, ErrProviderNotConfigured
	}
	return s.provider.GetPod(ctx, ref)
}
func (s *Service) GetPodLogs(ctx context.Context, request PodLogRequest) (string, error) {
	if s == nil || s.provider == nil {
		return "", ErrProviderNotConfigured
	}
	return s.provider.GetPodLogs(ctx, request)
}
func (s *Service) UpdatePodConfig(ctx context.Context, ref PodRef, update PodConfigUpdate) (PodDetail, error) {
	if s == nil || s.provider == nil {
		return PodDetail{}, ErrProviderNotConfigured
	}
	return s.provider.UpdatePodConfig(ctx, ref, update)
}

func (s *Service) ExecPodCommand(ctx context.Context, request PodExecRequest) (PodExecResult, error) {
	if s == nil || s.provider == nil {
		return PodExecResult{}, ErrPodExecUnsupported
	}
	executor, ok := s.provider.(PodExecutor)
	if !ok {
		return PodExecResult{}, ErrPodExecUnsupported
	}
	return executor.ExecPodCommand(ctx, request)
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

func (s *Service) CheckReleaseAccess(ctx context.Context, clusterID, namespace string) error {
	if s == nil || s.provider == nil {
		return ErrProviderNotConfigured
	}
	checker, ok := s.provider.(ReleaseAccessChecker)
	if !ok {
		// Observation-only/custom providers predate the optional preflight
		// contract. The Kubernetes provider implements this check; providers
		// that can publish must implement it to get the same gate.
		return nil
	}
	return checker.CheckReleaseAccess(ctx, clusterID, namespace)
}

// EnsureNamespace delegates generated namespace creation/ownership checks to a
// runtime that supports them. Observation-only providers remain compatible and
// report ErrNamespaceManagementUnsupported to callers that want to skip the
// optional write.
func (s *Service) EnsureNamespace(ctx context.Context, clusterID, namespace string, labels map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.provider == nil {
		return ErrNamespaceManagementUnsupported
	}
	manager, ok := s.provider.(NamespaceManager)
	if !ok {
		return ErrNamespaceManagementUnsupported
	}
	return manager.EnsureNamespace(ctx, clusterID, namespace, labels)
}

// EnsureNamespaceWithQuota prefers the quota-aware runtime contract while
// retaining compatibility with providers that only know how to create
// namespaces. The latter still get ownership checks, but cannot enforce a
// live Kubernetes budget.
func (s *Service) EnsureNamespaceWithQuota(ctx context.Context, clusterID, namespace string, labels map[string]string, quota domain.NamespaceQuota) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.provider == nil {
		return ErrNamespaceManagementUnsupported
	}
	if manager, ok := s.provider.(NamespaceQuotaManager); ok {
		return manager.EnsureNamespaceWithQuota(ctx, clusterID, namespace, labels, quota)
	}
	if manager, ok := s.provider.(NamespaceManager); ok {
		return manager.EnsureNamespace(ctx, clusterID, namespace, labels)
	}
	return ErrNamespaceManagementUnsupported
}

func (s *Service) CheckMonitoring(ctx context.Context, clusterID string) (MonitoringStatus, error) {
	if s == nil || s.provider == nil {
		return MonitoringStatus{}, ErrProviderNotConfigured
	}
	installer, ok := s.provider.(MonitoringInstaller)
	if !ok {
		return MonitoringStatus{
			Component:   "prometheus",
			DisplayName: "Prometheus",
			Installable: false,
			Message:     "当前运行时不支持自动安装监控组件",
		}, nil
	}
	return installer.CheckMonitoring(ctx, clusterID)
}

func (s *Service) InstallMonitoring(ctx context.Context, clusterID string) (MonitoringStatus, error) {
	return s.InstallMonitoringWithRetention(ctx, clusterID, 7)
}

func (s *Service) InstallMonitoringWithRetention(ctx context.Context, clusterID string, retentionDays int) (MonitoringStatus, error) {
	if s == nil || s.provider == nil {
		return MonitoringStatus{}, ErrProviderNotConfigured
	}
	installer, ok := s.provider.(MonitoringInstaller)
	if !ok {
		return MonitoringStatus{}, ErrClusterManagementUnsupported
	}
	if retentionInstaller, ok := s.provider.(MonitoringRetentionInstaller); ok {
		return retentionInstaller.InstallMonitoringWithRetention(ctx, clusterID, retentionDays)
	}
	return installer.InstallMonitoring(ctx, clusterID)
}

// CleanupEnvironment refuses to claim success when the configured provider
// cannot prove that the target's Kubernetes resources were removed.
func (s *Service) CleanupEnvironment(ctx context.Context, clusterID, namespace, projectID, targetID string) error {
	if s == nil || s.provider == nil {
		return ErrEnvironmentCleanupUnsupported
	}
	cleaner, ok := s.provider.(EnvironmentCleaner)
	if !ok {
		return ErrEnvironmentCleanupUnsupported
	}
	return cleaner.CleanupEnvironment(ctx, clusterID, namespace, projectID, targetID)
}

func (s *Service) SupportsABExperiment() bool {
	if s == nil || s.provider == nil {
		return false
	}
	_, ok := s.provider.(ABExperimentDeployer)
	return ok
}

func (s *Service) DeployABExperiment(ctx context.Context, deployment ABExperimentDeployment) error {
	if s == nil || s.provider == nil {
		return ErrReleaseDeploymentUnsupported
	}
	deployer, ok := s.provider.(ABExperimentDeployer)
	if !ok {
		return ErrReleaseDeploymentUnsupported
	}
	return deployer.DeployABExperiment(ctx, deployment)
}

func (s *Service) UpdateABExperimentTraffic(ctx context.Context, clusterID, namespace, projectID, experimentID string, aTraffic, bTraffic int) error {
	if s == nil || s.provider == nil {
		return ErrReleaseDeploymentUnsupported
	}
	deployer, ok := s.provider.(ABExperimentDeployer)
	if !ok {
		return ErrReleaseDeploymentUnsupported
	}
	return deployer.UpdateABExperimentTraffic(ctx, clusterID, namespace, projectID, experimentID, aTraffic, bTraffic)
}

func (s *Service) StopABExperiment(ctx context.Context, clusterID, namespace, projectID, experimentID, keepVariant string) error {
	if s == nil || s.provider == nil {
		return ErrReleaseDeploymentUnsupported
	}
	deployer, ok := s.provider.(ABExperimentDeployer)
	if !ok {
		return ErrReleaseDeploymentUnsupported
	}
	return deployer.StopABExperiment(ctx, clusterID, namespace, projectID, experimentID, keepVariant)
}

func (s *Service) ListABExperimentPods(ctx context.Context, clusterID, namespace, projectID, experimentID string) ([]Pod, error) {
	if s == nil || s.provider == nil {
		return nil, ErrReleaseDeploymentUnsupported
	}
	deployer, ok := s.provider.(ABExperimentDeployer)
	if !ok {
		return nil, ErrReleaseDeploymentUnsupported
	}
	return deployer.ListABExperimentPods(ctx, clusterID, namespace, projectID, experimentID)
}

func (s *Service) CleanupABExperiment(ctx context.Context, clusterID, namespace, projectID, experimentID string) error {
	if s == nil || s.provider == nil {
		return ErrReleaseDeploymentUnsupported
	}
	if cleaner, ok := s.provider.(ABExperimentCleaner); ok {
		return cleaner.CleanupABExperiment(ctx, clusterID, namespace, projectID, experimentID)
	}
	// Keep providers that predate the explicit cleaner usable. An empty
	// keepVariant is reserved for setup rollback and removes both variants.
	deployer, ok := s.provider.(ABExperimentDeployer)
	if !ok {
		return ErrReleaseDeploymentUnsupported
	}
	return deployer.StopABExperiment(ctx, clusterID, namespace, projectID, experimentID, "")
}

func (s *Service) GetABExperimentMetrics(ctx context.Context, clusterID, namespace, projectID, experimentID string) (ABExperimentMetrics, error) {
	if s == nil || s.provider == nil {
		return ABExperimentMetrics{}, ErrReleaseDeploymentUnsupported
	}
	provider, ok := s.provider.(ABExperimentMetricsProvider)
	if !ok {
		return ABExperimentMetrics{}, ErrReleaseDeploymentUnsupported
	}
	return provider.GetABExperimentMetrics(ctx, clusterID, namespace, projectID, experimentID)
}

func (s *Service) GetClusterMetrics(ctx context.Context, clusterID string) (ClusterMetrics, error) {
	if s == nil || s.provider == nil {
		return ClusterMetrics{}, ErrProviderNotConfigured
	}
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
	if s == nil || s.provider == nil {
		return ProjectMetrics{}, ErrProviderNotConfigured
	}
	return s.provider.GetProjectMetrics(ctx, clusterID, projectID)
}

// GetProjectMetricsInNamespace is the namespace-aware counterpart used by
// project pages. Providers without a native implementation still get a
// correctly scoped count from their Pod list; resource metrics remain the
// provider's responsibility.
func (s *Service) GetProjectMetricsInNamespace(ctx context.Context, clusterID, namespace, projectID string) (ProjectMetrics, error) {
	if s == nil || s.provider == nil {
		return ProjectMetrics{}, ErrProviderNotConfigured
	}
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
