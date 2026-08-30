package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

const (
	// ProjectLabelKey is the default label used to associate workloads with a
	// CI/CD project. A deployment can use another qualified label key through
	// WithProjectLabelKey.
	ProjectLabelKey = "cicd.yuebuy.com/project"

	configMapSuffix            = "-cicd-config"
	rolloutVersionKey          = "cicd.yuebuy.com/config-version"
	defaultKubeTimeout         = 15 * time.Second
	defaultRolloutTimeout      = 5 * time.Minute
	defaultRolloutPollInterval = 2 * time.Second
	maxPodLogBytes             = 8 << 20
	maxConfigValueBytes        = 1 << 20
	maxConfigEntries           = 256
)

var (
	ErrInvalidKubernetesInput   = errors.New("invalid kubernetes runtime input")
	ErrDeploymentNotFound       = errors.New("runtime deployment not found")
	ErrDeploymentRolloutFailed  = errors.New("kubernetes deployment rollout failed")
	ErrDeploymentRolloutTimeout = errors.New("kubernetes deployment rollout timed out")
	ErrRuntimeResponseTooLarge  = errors.New("kubernetes runtime response is too large")
)

// KubernetesOption configures a KubernetesProvider before it is used.
type KubernetesOption func(*KubernetesProvider)

// NamespaceAwareProvider is an optional extension of Provider. Callers can
// type-assert this interface when a project has an explicit namespace while
// keeping existing Provider implementations source-compatible.
type NamespaceAwareProvider interface {
	Provider
	ListPodsInNamespace(ctx context.Context, clusterID, namespace, projectID string) ([]Pod, error)
	GetProjectMetricsInNamespace(ctx context.Context, clusterID, namespace, projectID string) (ProjectMetrics, error)
}

// WithKubernetesTimeout sets the upper bound for each Kubernetes API call.
func WithKubernetesTimeout(timeout time.Duration) KubernetesOption {
	return func(provider *KubernetesProvider) {
		if timeout > 0 {
			provider.timeout = timeout
		}
	}
}

// WithKubernetesRolloutTimeout sets the maximum time a release may wait for a
// Deployment to become ready. API calls still use WithKubernetesTimeout.
func WithKubernetesRolloutTimeout(timeout time.Duration) KubernetesOption {
	return func(provider *KubernetesProvider) {
		if timeout > 0 {
			provider.rolloutTimeout = timeout
		}
	}
}

// WithKubernetesRolloutPollInterval changes how often a Deployment status is
// refreshed while waiting for its rollout.
func WithKubernetesRolloutPollInterval(interval time.Duration) KubernetesOption {
	return func(provider *KubernetesProvider) {
		if interval > 0 {
			provider.rolloutPollInterval = interval
		}
	}
}

// WithKubernetesRolloutWait controls whether DeployRelease waits for the
// Kubernetes controller to report a healthy Deployment. It is useful for
// apply-only tests; production providers should keep the default enabled.
func WithKubernetesRolloutWait(enabled bool) KubernetesOption {
	return func(provider *KubernetesProvider) {
		provider.rolloutWait = enabled
	}
}

// WithProjectLabelKey changes the qualified label key used by project-scoped
// pod and metrics queries.
func WithProjectLabelKey(key string) KubernetesOption {
	return func(provider *KubernetesProvider) {
		if strings.TrimSpace(key) == "" {
			provider.projectLabelKey = ""
			return
		}
		if len(validation.IsQualifiedName(key)) == 0 {
			provider.projectLabelKey = key
		}
	}
}

// WithKubernetesTargetContainer selects the container whose Deployment
// template is changed by UpdatePodConfig. The default is the first container.
func WithKubernetesTargetContainer(name string) KubernetesOption {
	return func(provider *KubernetesProvider) {
		if len(validation.IsDNS1123Label(name)) == 0 {
			provider.targetContainer = name
		}
	}
}

// KubernetesProvider is a client-go backed, read-only-at-Pod runtime
// provider. Pod configuration changes are applied to the owning Deployment
// and therefore result in a normal Kubernetes rollout.
type KubernetesProvider struct {
	mu                  sync.RWMutex
	clients             map[string]kubernetes.Interface
	metricsClients      map[string]metricsclientset.Interface
	timeout             time.Duration
	rolloutTimeout      time.Duration
	rolloutPollInterval time.Duration
	rolloutWait         bool
	projectLabelKey     string
	targetContainer     string
	configMapSuffix     string
	now                 func() time.Time
}

var _ Provider = (*KubernetesProvider)(nil)

// NewKubernetesProvider creates an initially empty cluster registry. Register
// each cluster with RegisterClient, RegisterKubeconfig, or RegisterInCluster.
func NewKubernetesProvider(options ...KubernetesOption) *KubernetesProvider {
	provider := &KubernetesProvider{
		clients:             make(map[string]kubernetes.Interface),
		metricsClients:      make(map[string]metricsclientset.Interface),
		timeout:             defaultKubeTimeout,
		rolloutTimeout:      defaultRolloutTimeout,
		rolloutPollInterval: defaultRolloutPollInterval,
		rolloutWait:         true,
		projectLabelKey:     ProjectLabelKey,
		configMapSuffix:     configMapSuffix,
		now:                 func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(provider)
		}
	}
	return provider
}

// RegisterClient registers an already-created client. It is useful for tests
// and for applications that create rest.Config objects themselves.
func (p *KubernetesProvider) RegisterClient(clusterID string, client kubernetes.Interface) error {
	return p.registerClient(clusterID, client, nil)
}

func (p *KubernetesProvider) registerClient(clusterID string, client kubernetes.Interface, metricsClient metricsclientset.Interface) error {
	if err := validateClusterID(clusterID); err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("%w: kubernetes client is required", ErrInvalidKubernetesInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients[clusterID] = client
	if metricsClient != nil {
		p.metricsClients[clusterID] = metricsClient
	} else {
		delete(p.metricsClients, clusterID)
	}
	return nil
}

// RegisterKubeconfig loads a kubeconfig file using its current-context and
// registers its client under clusterID. Credentials remain inside client-go
// and are never logged. Use RegisterKubeconfigWithContext to select a named
// context explicitly.
func (p *KubernetesProvider) RegisterKubeconfig(clusterID, kubeconfigPath string) error {
	return p.RegisterKubeconfigWithContext(clusterID, kubeconfigPath, "")
}

// RegisterKubeconfigWithContext loads kubeconfigPath and selects kubeContext
// when it is non-empty. An empty context preserves the kubeconfig's current
// context and therefore keeps RegisterKubeconfig behavior unchanged.
func (p *KubernetesProvider) RegisterKubeconfigWithContext(clusterID, kubeconfigPath, kubeContext string) error {
	if err := validateClusterID(clusterID); err != nil {
		return err
	}
	if strings.TrimSpace(kubeconfigPath) == "" || strings.ContainsAny(kubeconfigPath, "\x00\r\n") {
		return fmt.Errorf("%w: kubeconfig path is required", ErrInvalidKubernetesInput)
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.ExplicitPath = kubeconfigPath
	overrides := &clientcmd.ConfigOverrides{}
	if strings.TrimSpace(kubeContext) != "" {
		overrides.CurrentContext = strings.TrimSpace(kubeContext)
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}
	metricsClient, err := metricsclientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create kubernetes metrics client: %w", err)
	}
	return p.registerClient(clusterID, client, metricsClient)
}

// RegisterInCluster registers a client using the service account and API
// endpoint supplied by the Kubernetes in-cluster environment.
func (p *KubernetesProvider) RegisterInCluster(clusterID string) error {
	if err := validateClusterID(clusterID); err != nil {
		return err
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("load in-cluster config: %w", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}
	metricsClient, err := metricsclientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create kubernetes metrics client: %w", err)
	}
	return p.registerClient(clusterID, client, metricsClient)
}

func (p *KubernetesProvider) CheckCluster(ctx context.Context, clusterID string) (ClusterConnection, error) {
	if err := ctx.Err(); err != nil {
		return ClusterConnection{}, err
	}
	client, err := p.clientFor(clusterID)
	if err != nil {
		return ClusterConnection{}, err
	}
	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return ClusterConnection{}, mapKubernetesNotFound(err, ErrClusterNotFound)
	}
	return ClusterConnection{Version: version.GitVersion}, nil
}

func (p *KubernetesProvider) ListPods(ctx context.Context, clusterID, projectID string) ([]Pod, error) {
	return p.ListPodsInNamespace(ctx, clusterID, "", projectID)
}

// ListPodsInNamespace provides the namespace-aware form of ListPods while the
// existing Provider interface remains compatible with the demo provider.
func (p *KubernetesProvider) ListPodsInNamespace(ctx context.Context, clusterID, namespace, projectID string) ([]Pod, error) {
	client, err := p.clientFor(clusterID)
	if err != nil {
		return nil, err
	}
	if namespace != "" {
		if err := validateNamespace(namespace); err != nil {
			return nil, err
		}
	}
	if err := validateProjectID(projectID); err != nil {
		return nil, err
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	list, err := client.CoreV1().Pods(namespace).List(requestContext, metav1.ListOptions{LabelSelector: p.projectSelector(projectID)})
	if err != nil {
		return nil, mapKubernetesNotFound(err, ErrClusterNotFound)
	}
	result := make([]Pod, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, p.podFromKubernetes(clusterID, item, projectID))
	}
	p.attachPodMetrics(requestContext, clusterID, namespace, projectID, result)
	return result, nil
}

func (p *KubernetesProvider) GetPod(ctx context.Context, ref PodRef) (PodDetail, error) {
	client, err := p.clientFor(ref.ClusterID)
	if err != nil {
		return PodDetail{}, err
	}
	if err := validatePodRef(ref); err != nil {
		return PodDetail{}, err
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	pod, err := client.CoreV1().Pods(ref.Namespace).Get(requestContext, ref.Name, metav1.GetOptions{})
	if err != nil {
		return PodDetail{}, mapKubernetesNotFound(err, ErrPodNotFound)
	}
	if !p.podBelongsToProject(pod, ref.ProjectID) {
		return PodDetail{}, ErrPodNotFound
	}
	return p.podDetail(requestContext, client, ref.ClusterID, pod)
}

func (p *KubernetesProvider) GetPodLogs(ctx context.Context, request PodLogRequest) (string, error) {
	client, err := p.clientFor(request.ClusterID)
	if err != nil {
		return "", err
	}
	if err := validatePodRef(request.PodRef); err != nil {
		return "", err
	}
	if request.TailLines < 0 || request.TailLines > 10000 {
		return "", fmt.Errorf("%w: tail lines must be between 0 and 10000", ErrInvalidKubernetesInput)
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	pod, err := client.CoreV1().Pods(request.Namespace).Get(requestContext, request.Name, metav1.GetOptions{})
	if err != nil {
		return "", mapKubernetesNotFound(err, ErrPodNotFound)
	}
	if !p.podBelongsToProject(pod, request.ProjectID) {
		return "", ErrPodNotFound
	}
	container := strings.TrimSpace(request.Container)
	if container == "" {
		if len(pod.Spec.Containers) != 1 {
			return "", ErrContainerNotFound
		}
		container = pod.Spec.Containers[0].Name
	}
	if err := validateContainerName(container); err != nil {
		return "", err
	}
	options := &corev1.PodLogOptions{Container: container}
	if request.TailLines > 0 {
		tailLines := int64(request.TailLines)
		options.TailLines = &tailLines
	}
	stream, err := client.CoreV1().Pods(request.Namespace).GetLogs(request.Name, options).Stream(requestContext)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", ErrPodNotFound
		}
		return "", err
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, maxPodLogBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxPodLogBytes {
		return "", ErrRuntimeResponseTooLarge
	}
	return string(data), nil
}

// UpdatePodConfig intentionally does not update the Pod object. Pods are
// disposable workload instances; the owning Deployment template is the
// persistent source of truth and its template annotation triggers a rollout.
func (p *KubernetesProvider) UpdatePodConfig(ctx context.Context, ref PodRef, update PodConfigUpdate) (PodDetail, error) {
	if err := validatePodRef(ref); err != nil {
		return PodDetail{}, err
	}
	if err := validateConfigUpdate(update); err != nil {
		return PodDetail{}, err
	}
	client, err := p.clientFor(ref.ClusterID)
	if err != nil {
		return PodDetail{}, err
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	pod, err := client.CoreV1().Pods(ref.Namespace).Get(requestContext, ref.Name, metav1.GetOptions{})
	if err != nil {
		return PodDetail{}, mapKubernetesNotFound(err, ErrPodNotFound)
	}
	if !p.podBelongsToProject(pod, ref.ProjectID) {
		return PodDetail{}, ErrPodNotFound
	}
	deployment, err := p.deploymentForPod(requestContext, client, pod)
	if err != nil {
		return PodDetail{}, err
	}
	target, err := p.targetContainerIndex(deployment.Spec.Template.Spec.Containers)
	if err != nil {
		return PodDetail{}, err
	}
	if len(update.Config) > 0 {
		projectID := p.projectIDFromWorkload(pod, deployment)
		if err := p.upsertConfigMap(requestContext, client, deployment, projectID, update.Config); err != nil {
			return PodDetail{}, err
		}
		configName := p.configMapName(deployment.Name)
		if !hasConfigMapEnvFrom(deployment.Spec.Template.Spec.Containers[target], configName) {
			deployment.Spec.Template.Spec.Containers[target].EnvFrom = append(deployment.Spec.Template.Spec.Containers[target].EnvFrom, corev1.EnvFromSource{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName}}})
		}
	}
	deployment.Spec.Template.Spec.Containers[target].Env = setEnvironment(deployment.Spec.Template.Spec.Containers[target].Env, update.Environment)
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations[rolloutVersionKey] = p.now().UTC().Format(time.RFC3339Nano)
	if _, err := client.AppsV1().Deployments(ref.Namespace).Update(requestContext, deployment, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return PodDetail{}, ErrDeploymentNotFound
		}
		return PodDetail{}, err
	}

	// Return the current Pod with the desired values overlaid. Kubernetes may
	// keep serving the old Pod until the Deployment controller replaces it.
	detail, err := p.podDetail(requestContext, client, ref.ClusterID, pod)
	if err != nil {
		return PodDetail{}, err
	}
	if detail.Environment == nil {
		detail.Environment = make(map[string]string)
	}
	for key, value := range update.Environment {
		detail.Environment[key] = value
	}
	if len(update.Config) > 0 {
		if detail.Config == nil {
			detail.Config = make(map[string]string)
		}
		for key, value := range update.Config {
			detail.Config[key] = value
		}
	}
	return detail, nil
}

func (p *KubernetesProvider) GetClusterMetrics(ctx context.Context, clusterID string) (ClusterMetrics, error) {
	client, err := p.clientFor(clusterID)
	if err != nil {
		return ClusterMetrics{}, err
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	list, err := client.CoreV1().Pods(metav1.NamespaceAll).List(requestContext, metav1.ListOptions{})
	if err != nil {
		return ClusterMetrics{}, mapKubernetesNotFound(err, ErrClusterNotFound)
	}
	total, healthy := countPodHealth(list.Items)
	return ClusterMetrics{
		ClusterID:         clusterID,
		CPUUsedPercent:    0,
		MemoryUsedPercent: 0,
		MetricsAvailable:  false,
		MetricsMessage:    "metrics-server 未接入",
		PodCount:          total,
		HealthyPodCount:   healthy,
		ObservedAt:        p.now().UTC(),
	}, nil
}

func (p *KubernetesProvider) GetProjectMetrics(ctx context.Context, clusterID, projectID string) (ProjectMetrics, error) {
	return p.GetProjectMetricsInNamespace(ctx, clusterID, "", projectID)
}

// GetProjectMetricsInNamespace is the namespace-aware metrics extension. CPU
// and memory remain zero when metrics-server is not queried; Pod counts and
// readiness are always derived from the Kubernetes Pod list.
func (p *KubernetesProvider) GetProjectMetricsInNamespace(ctx context.Context, clusterID, namespace, projectID string) (ProjectMetrics, error) {
	client, err := p.clientFor(clusterID)
	if err != nil {
		return ProjectMetrics{}, err
	}
	if namespace != "" {
		if err := validateNamespace(namespace); err != nil {
			return ProjectMetrics{}, err
		}
	}
	if err := validateProjectID(projectID); err != nil {
		return ProjectMetrics{}, err
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	list, err := client.CoreV1().Pods(namespace).List(requestContext, metav1.ListOptions{LabelSelector: p.projectSelector(projectID)})
	if err != nil {
		return ProjectMetrics{}, mapKubernetesNotFound(err, ErrClusterNotFound)
	}
	total, healthy := countPodHealth(list.Items)
	return ProjectMetrics{
		ClusterID:         clusterID,
		ProjectID:         projectID,
		CPUUsedPercent:    0,
		MemoryUsedPercent: 0,
		MetricsAvailable:  false,
		MetricsMessage:    "metrics-server 未接入",
		PodCount:          total,
		HealthyPodCount:   healthy,
		ObservedAt:        p.now().UTC(),
	}, nil
}

func (p *KubernetesProvider) clientFor(clusterID string) (kubernetes.Interface, error) {
	if err := validateClusterID(clusterID); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	client, ok := p.clients[clusterID]
	if !ok {
		return nil, ErrClusterNotFound
	}
	return client, nil
}

func (p *KubernetesProvider) metricsClientFor(clusterID string) (metricsclientset.Interface, error) {
	if err := validateClusterID(clusterID); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	client, ok := p.metricsClients[clusterID]
	if !ok {
		return nil, ErrClusterNotFound
	}
	return client, nil
}

func (p *KubernetesProvider) attachPodMetrics(ctx context.Context, clusterID, namespace, projectID string, pods []Pod) {
	if len(pods) == 0 {
		return
	}
	client, err := p.metricsClientFor(clusterID)
	if err != nil {
		return
	}
	// Metrics-server versions differ in whether PodMetrics preserves workload
	// labels. The Pod list has already been scoped by project and namespace, so
	// fetch metrics for that scope and match them back by the stable Pod key.
	metrics, err := client.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}

	usageByPod := make(map[string][2]int64, len(metrics.Items))
	for _, item := range metrics.Items {
		var cpuMilli, memoryBytes int64
		for _, container := range item.Containers {
			cpu := container.Usage[corev1.ResourceCPU]
			memory := container.Usage[corev1.ResourceMemory]
			cpuMilli += cpu.MilliValue()
			memoryBytes += memory.Value()
		}
		usageByPod[item.Namespace+"\x00"+item.Name] = [2]int64{cpuMilli, memoryBytes}
	}
	for index := range pods {
		usage, ok := usageByPod[pods[index].Namespace+"\x00"+pods[index].Name]
		if !ok {
			continue
		}
		pods[index].CPUUsageMilli = usage[0]
		pods[index].MemoryUsageBytes = usage[1]
		pods[index].MetricsAvailable = true
		pods[index].MetricsSource = "metrics-server"
	}
}

func (p *KubernetesProvider) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultKubeTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (p *KubernetesProvider) projectSelector(projectID string) string {
	if projectID == "" || p.projectLabelKey == "" {
		return ""
	}
	return labels.Set{p.projectLabelKey: projectID}.AsSelector().String()
}

func (p *KubernetesProvider) podDetail(ctx context.Context, client kubernetes.Interface, clusterID string, pod *corev1.Pod) (PodDetail, error) {
	detail := PodDetail{Pod: p.podFromKubernetes(clusterID, *pod, ""), Containers: make(map[string]ContainerStatus), Config: make(map[string]string), Environment: make(map[string]string)}
	statusByName := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses))
	for _, status := range pod.Status.ContainerStatuses {
		statusByName[status.Name] = status
	}
	for _, container := range pod.Spec.Containers {
		status := statusByName[container.Name]
		detail.Containers[container.Name] = ContainerStatus{Name: container.Name, Image: container.Image, Ready: status.Ready, RestartCount: int(status.RestartCount)}
		for _, env := range container.Env {
			if env.Value != "" {
				detail.Environment[env.Name] = env.Value
			}
			if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
				value, found, err := configMapValue(ctx, client, pod.Namespace, env.ValueFrom.ConfigMapKeyRef)
				if err != nil {
					return PodDetail{}, err
				}
				if found {
					detail.Environment[env.Name] = value
					detail.Config[env.Name] = value
				}
			}
		}
		for _, source := range container.EnvFrom {
			if source.ConfigMapRef == nil {
				continue
			}
			configMap, err := client.CoreV1().ConfigMaps(pod.Namespace).Get(ctx, source.ConfigMapRef.Name, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) && source.ConfigMapRef.Optional != nil && *source.ConfigMapRef.Optional {
					continue
				}
				return PodDetail{}, err
			}
			for key, value := range configMap.Data {
				detail.Config[key] = value
				detail.Environment[key] = value
			}
		}
	}
	pods := []Pod{detail.Pod}
	p.attachPodMetrics(ctx, clusterID, pod.Namespace, "", pods)
	detail.Pod = pods[0]
	return detail, nil
}

func (p *KubernetesProvider) deploymentForPod(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod) (*appsv1.Deployment, error) {
	for _, owner := range pod.OwnerReferences {
		switch owner.Kind {
		case "Deployment":
			deployment, err := client.AppsV1().Deployments(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
			if err == nil {
				return deployment, nil
			}
			if apierrors.IsNotFound(err) {
				return nil, ErrDeploymentNotFound
			}
			return nil, err
		case "ReplicaSet":
			replicaSet, err := client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					return nil, ErrDeploymentNotFound
				}
				return nil, err
			}
			for _, replicaOwner := range replicaSet.OwnerReferences {
				if replicaOwner.Kind != "Deployment" {
					continue
				}
				deployment, err := client.AppsV1().Deployments(pod.Namespace).Get(ctx, replicaOwner.Name, metav1.GetOptions{})
				if err == nil {
					return deployment, nil
				}
				if apierrors.IsNotFound(err) {
					return nil, ErrDeploymentNotFound
				}
				return nil, err
			}
		}
	}
	return nil, ErrDeploymentNotFound
}

func (p *KubernetesProvider) targetContainerIndex(containers []corev1.Container) (int, error) {
	if len(containers) == 0 {
		return 0, ErrContainerNotFound
	}
	if p.targetContainer == "" {
		return 0, nil
	}
	for index, container := range containers {
		if container.Name == p.targetContainer {
			return index, nil
		}
	}
	return 0, ErrContainerNotFound
}

func (p *KubernetesProvider) configMapName(deploymentName string) string {
	name := deploymentName + p.configMapSuffix
	if len(validation.IsDNS1123Subdomain(name)) == 0 && len(name) <= 253 {
		return name
	}
	maxBaseLength := 253 - len(p.configMapSuffix)
	if maxBaseLength < 1 {
		return "cicd-config"
	}
	base := deploymentName
	if len(base) > maxBaseLength {
		base = strings.TrimRight(base[:maxBaseLength], "-")
	}
	if base == "" {
		base = "cicd"
	}
	return base + p.configMapSuffix
}

func (p *KubernetesProvider) upsertConfigMap(ctx context.Context, client kubernetes.Interface, deployment *appsv1.Deployment, projectID string, values map[string]string) error {
	name := p.configMapName(deployment.Name)
	configMaps := client.CoreV1().ConfigMaps(deployment.Namespace)
	configMap, err := configMaps.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		data := cloneStringMap(values)
		labels := make(map[string]string)
		if projectID != "" {
			labels[p.projectLabelKey] = projectID
		}
		object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: deployment.Namespace, Labels: labels}, Data: data}
		if deployment.UID != "" {
			object.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(deployment, appsv1.SchemeGroupVersion.WithKind("Deployment"))}
		}
		_, err = configMaps.Create(ctx, object, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if configMap.Labels == nil {
		configMap.Labels = make(map[string]string)
	}
	if projectID != "" {
		configMap.Labels[p.projectLabelKey] = projectID
	} else {
		delete(configMap.Labels, p.projectLabelKey)
	}
	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	for key, value := range values {
		configMap.Data[key] = value
	}
	_, err = configMaps.Update(ctx, configMap, metav1.UpdateOptions{})
	return err
}

func (p *KubernetesProvider) projectIDFromWorkload(pod *corev1.Pod, deployment *appsv1.Deployment) string {
	if pod != nil && pod.Labels != nil {
		if projectID := strings.TrimSpace(pod.Labels[p.projectLabelKey]); projectID != "" {
			return projectID
		}
	}
	if deployment != nil {
		if projectID := strings.TrimSpace(deployment.Labels[p.projectLabelKey]); projectID != "" {
			return projectID
		}
		if projectID := strings.TrimSpace(deployment.Spec.Template.Labels[p.projectLabelKey]); projectID != "" {
			return projectID
		}
	}
	return ""
}

func (p *KubernetesProvider) podBelongsToProject(pod *corev1.Pod, projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || p.projectLabelKey == "" {
		return true
	}
	return pod != nil && strings.TrimSpace(pod.Labels[p.projectLabelKey]) == projectID
}

func hasConfigMapEnvFrom(container corev1.Container, name string) bool {
	for _, source := range container.EnvFrom {
		if source.ConfigMapRef != nil && source.ConfigMapRef.Name == name {
			return true
		}
	}
	return false
}

func setEnvironment(environment []corev1.EnvVar, values map[string]string) []corev1.EnvVar {
	indices := make(map[string]int, len(environment))
	for index, item := range environment {
		indices[item.Name] = index
	}
	for key, value := range values {
		if index, ok := indices[key]; ok {
			environment[index].Value = value
			environment[index].ValueFrom = nil
			continue
		}
		environment = append(environment, corev1.EnvVar{Name: key, Value: value})
		indices[key] = len(environment) - 1
	}
	return environment
}

func configMapValue(ctx context.Context, client kubernetes.Interface, namespace string, reference *corev1.ConfigMapKeySelector) (string, bool, error) {
	configMap, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, reference.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) && reference.Optional != nil && *reference.Optional {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	value, ok := configMap.Data[reference.Key]
	return value, ok, nil
}

func (p *KubernetesProvider) podFromKubernetes(clusterID string, pod corev1.Pod, fallbackProject string) Pod {
	projectID := pod.Labels[p.projectLabelKey]
	if projectID == "" {
		projectID = pod.Labels["app.kubernetes.io/part-of"]
	}
	if projectID == "" {
		projectID = fallbackProject
	}
	startedAt := time.Time{}
	if pod.Status.StartTime != nil {
		startedAt = pod.Status.StartTime.Time
	}
	restartCount := 0
	for _, status := range pod.Status.ContainerStatuses {
		restartCount += int(status.RestartCount)
	}
	return Pod{PodRef: PodRef{ClusterID: clusterID, Namespace: pod.Namespace, Name: pod.Name}, ProjectID: projectID, NodeName: pod.Spec.NodeName, PodIP: pod.Status.PodIP, Phase: PodPhase(pod.Status.Phase), Ready: podReady(pod), RestartCount: restartCount, Labels: cloneStringMap(pod.Labels), StartedAt: startedAt}
}

func podReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func countPodHealth(pods []corev1.Pod) (int, int) {
	healthy := 0
	for _, pod := range pods {
		if podReady(pod) {
			healthy++
		}
	}
	return len(pods), healthy
}

func validateClusterID(value string) error {
	return validateSimpleIdentifier(value, "cluster ID")
}

func validateNamespace(value string) error {
	if len(validation.IsDNS1123Subdomain(value)) != 0 {
		return fmt.Errorf("%w: namespace is invalid", ErrInvalidKubernetesInput)
	}
	return nil
}

func validateContainerName(value string) error {
	if len(validation.IsDNS1123Label(value)) != 0 {
		return fmt.Errorf("%w: container name is invalid", ErrInvalidKubernetesInput)
	}
	return nil
}

func validatePodRef(ref PodRef) error {
	if err := validateClusterID(ref.ClusterID); err != nil {
		return err
	}
	if err := validateNamespace(ref.Namespace); err != nil {
		return err
	}
	if len(validation.IsDNS1123Subdomain(ref.Name)) != 0 {
		return fmt.Errorf("%w: pod name is invalid", ErrInvalidKubernetesInput)
	}
	return nil
}

func validateProjectID(value string) error {
	if value != "" && len(validation.IsValidLabelValue(value)) != 0 {
		return fmt.Errorf("%w: project label value is invalid", ErrInvalidKubernetesInput)
	}
	return nil
}

func validateConfigUpdate(update PodConfigUpdate) error {
	if len(update.Config) == 0 && len(update.Environment) == 0 {
		return fmt.Errorf("%w: at least one config or environment value is required", ErrInvalidKubernetesInput)
	}
	if len(update.Config)+len(update.Environment) > maxConfigEntries {
		return fmt.Errorf("%w: too many config entries", ErrInvalidKubernetesInput)
	}
	for key, value := range update.Config {
		if len(validation.IsEnvVarName(key)) != 0 {
			return fmt.Errorf("%w: config key is invalid", ErrInvalidKubernetesInput)
		}
		if len(value) > maxConfigValueBytes || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%w: config value is invalid", ErrInvalidKubernetesInput)
		}
	}
	for key, value := range update.Environment {
		if len(validation.IsEnvVarName(key)) != 0 {
			return fmt.Errorf("%w: environment key is invalid", ErrInvalidKubernetesInput)
		}
		if len(value) > maxConfigValueBytes || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%w: environment value is invalid", ErrInvalidKubernetesInput)
		}
	}
	return nil
}

func validateSimpleIdentifier(value, label string) error {
	if strings.TrimSpace(value) == "" || len(value) > 128 || strings.ContainsAny(value, "/\\\x00\r\n\t ") {
		return fmt.Errorf("%w: %s is invalid", ErrInvalidKubernetesInput, label)
	}
	return nil
}

func mapKubernetesNotFound(err error, fallback error) error {
	if apierrors.IsNotFound(err) {
		return fallback
	}
	return err
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
