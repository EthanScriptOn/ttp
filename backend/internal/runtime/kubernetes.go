package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
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

// WithKubernetesRuntimeServiceAccount selects the ServiceAccount that TTP
// binds into each generated namespace. The RBAC template creates this account
// in ttp-system by default, but self-hosted deployments can rename it.
func WithKubernetesRuntimeServiceAccount(namespace, name string) KubernetesOption {
	return func(provider *KubernetesProvider) {
		provider.runtimeServiceAccountNamespace = strings.TrimSpace(namespace)
		provider.runtimeServiceAccountName = strings.TrimSpace(name)
	}
}

// KubernetesProvider is a client-go backed, read-only-at-Pod runtime
// provider. Pod configuration changes are applied to the owning Deployment
// and therefore result in a normal Kubernetes rollout.
type KubernetesProvider struct {
	mu                             sync.RWMutex
	clients                        map[string]kubernetes.Interface
	configs                        map[string]*rest.Config
	timeout                        time.Duration
	rolloutTimeout                 time.Duration
	rolloutPollInterval            time.Duration
	rolloutWait                    bool
	projectLabelKey                string
	targetContainer                string
	configMapSuffix                string
	runtimeServiceAccountNamespace string
	runtimeServiceAccountName      string
	now                            func() time.Time
}

var _ Provider = (*KubernetesProvider)(nil)

// NewKubernetesProvider creates an initially empty cluster registry. Register
// each cluster with RegisterClient, RegisterKubeconfig, or RegisterInCluster.
func NewKubernetesProvider(options ...KubernetesOption) *KubernetesProvider {
	provider := &KubernetesProvider{
		clients:                        make(map[string]kubernetes.Interface),
		configs:                        make(map[string]*rest.Config),
		timeout:                        defaultKubeTimeout,
		rolloutTimeout:                 defaultRolloutTimeout,
		rolloutPollInterval:            defaultRolloutPollInterval,
		rolloutWait:                    true,
		projectLabelKey:                ProjectLabelKey,
		configMapSuffix:                configMapSuffix,
		runtimeServiceAccountNamespace: defaultRuntimeServiceAccountNamespace,
		runtimeServiceAccountName:      defaultRuntimeServiceAccountName,
		now:                            func() time.Time { return time.Now().UTC() },
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
	if err := validateClusterID(clusterID); err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("%w: kubernetes client is required", ErrInvalidKubernetesInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients[clusterID] = client
	p.configs[clusterID] = nil
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
	if err := p.RegisterClient(clusterID, client); err != nil {
		return err
	}
	p.mu.Lock()
	p.configs[clusterID] = config
	p.mu.Unlock()
	return nil
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
	if err := p.RegisterClient(clusterID, client); err != nil {
		return err
	}
	p.mu.Lock()
	p.configs[clusterID] = config
	p.mu.Unlock()
	return nil
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

// EnsureNamespace creates a generated environment namespace when it does not
// exist. Existing namespaces are usable only when their TTP ownership labels
// match the requested space/environment; an unmanaged or differently owned
// namespace is never silently adopted.
func (p *KubernetesProvider) EnsureNamespace(ctx context.Context, clusterID, namespace string, labels map[string]string) error {
	if err := validateClusterID(clusterID); err != nil {
		return err
	}
	if err := validateNamespace(namespace); err != nil {
		return err
	}
	client, err := p.clientFor(clusterID)
	if err != nil {
		return err
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()

	namespaces := client.CoreV1().Namespaces()
	existing, err := namespaces.Get(requestContext, namespace, metav1.GetOptions{})
	if err == nil {
		if namespaceLabelsMatch(existing.Labels, labels) {
			return nil
		}
		return fmt.Errorf("%w: namespace %s", ErrNamespaceOwnershipConflict, namespace)
	}
	if !apierrors.IsNotFound(err) {
		return mapKubernetesNotFound(err, ErrClusterNotFound)
	}

	desiredLabels := cloneStringMap(labels)
	if desiredLabels == nil {
		desiredLabels = map[string]string{}
	}
	if strings.TrimSpace(desiredLabels[NamespaceManagedByLabel]) == "" {
		desiredLabels[NamespaceManagedByLabel] = NamespaceManagedByValue
	}
	_, err = namespaces.Create(requestContext, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: desiredLabels},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		current, getErr := namespaces.Get(requestContext, namespace, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		if namespaceLabelsMatch(current.Labels, labels) {
			return nil
		}
		return fmt.Errorf("%w: namespace %s", ErrNamespaceOwnershipConflict, namespace)
	}
	if err != nil {
		return err
	}
	return nil
}

// EnsureNamespaceWithQuota creates or verifies a TTP-owned namespace and
// reconciles the two namespace-scoped policy objects that enforce its budget:
// ResourceQuota for aggregate capacity and LimitRange for per-container
// defaults. Objects with the same fixed names but without matching TTP
// ownership labels are never adopted.
func (p *KubernetesProvider) EnsureNamespaceWithQuota(ctx context.Context, clusterID, namespace string, labels map[string]string, quota domain.NamespaceQuota) error {
	parsed, err := parseNamespaceQuota(quota)
	if err != nil {
		return err
	}
	if err := p.EnsureNamespace(ctx, clusterID, namespace, labels); err != nil {
		return err
	}
	client, err := p.clientFor(clusterID)
	if err != nil {
		return err
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()

	managedLabels := cloneStringMap(labels)
	if managedLabels == nil {
		managedLabels = map[string]string{}
	}
	if strings.TrimSpace(managedLabels[NamespaceManagedByLabel]) == "" {
		managedLabels[NamespaceManagedByLabel] = NamespaceManagedByValue
	}
	if err := p.reconcileNamespaceRuntimeAccess(requestContext, client, namespace, managedLabels); err != nil {
		return err
	}

	desiredQuota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespaceResourceQuotaName,
			Namespace: namespace,
			Labels:    cloneStringMap(managedLabels),
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU:              parsed.cpuRequest,
				corev1.ResourceLimitsCPU:                parsed.cpuLimit,
				corev1.ResourceRequestsMemory:           parsed.memoryRequest,
				corev1.ResourceLimitsMemory:             parsed.memoryLimit,
				corev1.ResourceRequestsEphemeralStorage: parsed.ephemeralStorageRequest,
				corev1.ResourceLimitsEphemeralStorage:   parsed.ephemeralStorageLimit,
				corev1.ResourceRequestsStorage:          parsed.storage,
				corev1.ResourcePods:                     *resource.NewQuantity(int64(quota.Pods), resource.DecimalSI),
				corev1.ResourcePersistentVolumeClaims:   *resource.NewQuantity(int64(quota.PersistentVolumeClaims), resource.DecimalSI),
			},
		},
	}
	if err := p.reconcileResourceQuota(requestContext, client, namespace, desiredQuota, managedLabels); err != nil {
		return err
	}

	desiredLimitRange := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespaceLimitRangeName,
			Namespace: namespace,
			Labels:    cloneStringMap(managedLabels),
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{{
				Type: corev1.LimitTypeContainer,
				Default: corev1.ResourceList{
					corev1.ResourceCPU:              parsed.defaultCPULimit,
					corev1.ResourceMemory:           parsed.defaultMemoryLimit,
					corev1.ResourceEphemeralStorage: parsed.defaultEphemeralStorageLimit,
				},
				DefaultRequest: corev1.ResourceList{
					corev1.ResourceCPU:              parsed.defaultCPURequest,
					corev1.ResourceMemory:           parsed.defaultMemoryRequest,
					corev1.ResourceEphemeralStorage: parsed.defaultEphemeralStorageRequest,
				},
			}},
		},
	}
	return p.reconcileLimitRange(requestContext, client, namespace, desiredLimitRange, managedLabels)
}

func (p *KubernetesProvider) reconcileResourceQuota(ctx context.Context, client kubernetes.Interface, namespace string, desired *corev1.ResourceQuota, labels map[string]string) error {
	quotas := client.CoreV1().ResourceQuotas(namespace)
	existing, err := quotas.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		created, createErr := quotas.Create(ctx, desired, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(createErr) {
			existing, err = quotas.Get(ctx, desired.Name, metav1.GetOptions{})
		} else if createErr == nil {
			existing = created
			err = nil
		} else {
			return createErr
		}
	}
	if err != nil {
		return err
	}
	if !namespaceLabelsMatch(existing.Labels, labels) {
		return fmt.Errorf("%w: ResourceQuota %s/%s", ErrNamespaceOwnershipConflict, namespace, desired.Name)
	}
	desired.ResourceVersion = existing.ResourceVersion
	if _, err := quotas.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return nil
}

func (p *KubernetesProvider) reconcileLimitRange(ctx context.Context, client kubernetes.Interface, namespace string, desired *corev1.LimitRange, labels map[string]string) error {
	limits := client.CoreV1().LimitRanges(namespace)
	existing, err := limits.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		created, createErr := limits.Create(ctx, desired, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(createErr) {
			existing, err = limits.Get(ctx, desired.Name, metav1.GetOptions{})
		} else if createErr == nil {
			existing = created
			err = nil
		} else {
			return createErr
		}
	}
	if err != nil {
		return err
	}
	if !namespaceLabelsMatch(existing.Labels, labels) {
		return fmt.Errorf("%w: LimitRange %s/%s", ErrNamespaceOwnershipConflict, namespace, desired.Name)
	}
	desired.ResourceVersion = existing.ResourceVersion
	if _, err := limits.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return nil
}

func namespaceLabelsMatch(actual, expected map[string]string) bool {
	if actual == nil || expected == nil {
		return false
	}
	for key, value := range expected {
		if strings.TrimSpace(actual[key]) != strings.TrimSpace(value) {
			return false
		}
	}
	return strings.TrimSpace(actual[NamespaceManagedByLabel]) == NamespaceManagedByValue
}

func (p *KubernetesProvider) CheckReleaseAccess(ctx context.Context, clusterID, namespace string) error {
	client, err := p.clientFor(clusterID)
	if err != nil {
		return err
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	checks := make([]authorizationv1.ResourceAttributes, 0, 64)
	addChecks := func(group, resource string, verbs ...string) {
		for _, verb := range verbs {
			checks = append(checks, authorizationv1.ResourceAttributes{Namespace: namespace, Group: group, Resource: resource, Verb: verb})
		}
	}
	namespacedWriteVerbs := []string{"get", "create", "update", "patch", "delete"}
	addChecks("", "pods", "get", "list", "watch", "delete")
	addChecks("", "pods/log", "get")
	addChecks("apps", "replicasets", "get", "list", "watch", "delete")
	for _, resource := range []string{"services", "configmaps", "secrets", "persistentvolumeclaims", "resourcequotas", "limitranges"} {
		addChecks("", resource, namespacedWriteVerbs...)
	}
	for _, resource := range []string{"deployments", "statefulsets", "daemonsets"} {
		addChecks("apps", resource, namespacedWriteVerbs...)
	}
	for _, resource := range []string{"jobs", "cronjobs"} {
		addChecks("batch", resource, namespacedWriteVerbs...)
	}
	addChecks("networking.k8s.io", "ingresses", namespacedWriteVerbs...)
	addChecks("autoscaling", "horizontalpodautoscalers", namespacedWriteVerbs...)
	for _, attributes := range checks {
		result, checkErr := client.AuthorizationV1().SelfSubjectAccessReviews().Create(requestContext, &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &attributes}}, metav1.CreateOptions{})
		if checkErr != nil {
			return fmt.Errorf("%w: check %s %s in namespace %s: %v", ErrReleaseAccessDenied, attributes.Verb, attributes.Resource, namespace, checkErr)
		}
		if !result.Status.Allowed {
			return fmt.Errorf("%w: Kubernetes identity cannot %s %s in namespace %s", ErrReleaseAccessDenied, attributes.Verb, attributes.Resource, namespace)
		}
	}
	return nil
}

func (p *KubernetesProvider) ListPods(ctx context.Context, clusterID, projectID string) ([]Pod, error) {
	return p.ListPodsInNamespace(ctx, clusterID, "", projectID)
}

// ListPodsInNamespace provides the namespace-aware form of ListPods while the
// existing Provider interface remains compatible with read-only providers.
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

func (p *KubernetesProvider) ExecPodCommand(ctx context.Context, request PodExecRequest) (PodExecResult, error) {
	client, err := p.clientFor(request.ClusterID)
	if err != nil {
		return PodExecResult{}, err
	}
	if err := validatePodRef(request.PodRef); err != nil {
		return PodExecResult{}, err
	}
	command := strings.TrimSpace(request.Command)
	if command == "" || len(command) > 4096 || strings.ContainsAny(command, "\x00\r\n") {
		return PodExecResult{}, fmt.Errorf("%w: command must be between 1 and 4096 characters", ErrInvalidKubernetesInput)
	}

	p.mu.RLock()
	config := p.configs[request.ClusterID]
	p.mu.RUnlock()
	if config == nil {
		return PodExecResult{}, ErrPodExecUnsupported
	}

	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	pod, err := client.CoreV1().Pods(request.Namespace).Get(requestContext, request.Name, metav1.GetOptions{})
	if err != nil {
		return PodExecResult{}, mapKubernetesNotFound(err, ErrPodNotFound)
	}
	if !p.podBelongsToProject(pod, request.ProjectID) {
		return PodExecResult{}, ErrPodNotFound
	}
	container := strings.TrimSpace(request.Container)
	if container == "" {
		if len(pod.Spec.Containers) != 1 {
			return PodExecResult{}, ErrContainerNotFound
		}
		container = pod.Spec.Containers[0].Name
	}
	if err := validateContainerName(container); err != nil {
		return PodExecResult{}, err
	}

	execURL := client.CoreV1().RESTClient().Post().Resource("pods").Name(request.Name).Namespace(request.Namespace).SubResource("exec").VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{"/bin/sh", "-c", command},
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec).URL()
	executor, err := remotecommand.NewSPDYExecutor(config, http.MethodPost, execURL)
	if err != nil {
		return PodExecResult{}, err
	}
	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(requestContext, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr})
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" && !strings.HasSuffix(output, "\n") {
			output += "\n"
		}
		output += stderr.String()
	}
	if len(output) > maxPodLogBytes {
		return PodExecResult{}, ErrRuntimeResponseTooLarge
	}
	return PodExecResult{Output: output}, err
}

// StreamPodTerminal attaches a real interactive shell to a running Pod. The
// browser owns the lifetime of ctx; no Kubernetes request timeout is applied
// here because a terminal is intentionally long-lived.
func (p *KubernetesProvider) StreamPodTerminal(ctx context.Context, request PodTerminalRequest, stdin io.Reader, stdout, stderr io.Writer, sizes <-chan TerminalSize) error {
	client, err := p.clientFor(request.ClusterID)
	if err != nil {
		return err
	}
	if err := validatePodRef(request.PodRef); err != nil {
		return err
	}
	if stdin == nil || stdout == nil {
		return fmt.Errorf("%w: terminal streams are required", ErrInvalidKubernetesInput)
	}

	p.mu.RLock()
	config := p.configs[request.ClusterID]
	p.mu.RUnlock()
	if config == nil {
		return ErrPodExecUnsupported
	}
	if ctx == nil {
		ctx = context.Background()
	}
	pod, err := client.CoreV1().Pods(request.Namespace).Get(ctx, request.Name, metav1.GetOptions{})
	if err != nil {
		return mapKubernetesNotFound(err, ErrPodNotFound)
	}
	if !p.podBelongsToProject(pod, request.ProjectID) {
		return ErrPodNotFound
	}
	if pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("%w: Pod is not running", ErrInvalidKubernetesInput)
	}
	container := strings.TrimSpace(request.Container)
	if container == "" {
		if len(pod.Spec.Containers) != 1 {
			return ErrContainerNotFound
		}
		container = pod.Spec.Containers[0].Name
	}
	if err := validateContainerName(container); err != nil {
		return err
	}

	execURL := client.CoreV1().RESTClient().Post().Resource("pods").Name(request.Name).Namespace(request.Namespace).SubResource("exec").VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{"/bin/sh"},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec).URL()
	options := remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               true,
		TerminalSizeQueue: terminalSizeQueue{ctx: ctx, sizes: sizes},
	}
	executor, err := remotecommand.NewWebSocketExecutor(config, http.MethodPost, execURL.String())
	streamErr := err
	if err == nil {
		streamErr = executor.StreamWithContext(ctx, options)
		if streamErr == nil || ctx.Err() != nil {
			return streamErr
		}
	}
	// Older API servers and some K3s releases expose exec over SPDY only. Keep
	// the browser-facing protocol unchanged while falling back to that native
	// transport when the WebSocket handshake is rejected.
	spdyExecutor, spdyErr := remotecommand.NewSPDYExecutor(config, http.MethodPost, execURL)
	if spdyErr != nil {
		return streamErr
	}
	return spdyExecutor.StreamWithContext(ctx, options)
}

type terminalSizeQueue struct {
	ctx   context.Context
	sizes <-chan TerminalSize
}

func (q terminalSizeQueue) Next() *remotecommand.TerminalSize {
	if q.sizes == nil {
		return nil
	}
	select {
	case <-q.ctx.Done():
		return nil
	case size, ok := <-q.sizes:
		if !ok {
			return nil
		}
		if size.Columns == 0 || size.Rows == 0 {
			return q.Next()
		}
		return &remotecommand.TerminalSize{Width: size.Columns, Height: size.Rows}
	}
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
		if err := p.upsertConfigMap(requestContext, client, deployment, projectID, deployment.Labels[targetLabelKey], update.Config); err != nil {
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
	restarts, pending, failed, crashLoops, oomKilled := podStatusSummary(list.Items)
	result := ClusterMetrics{
		ClusterID:        clusterID,
		MetricsAvailable: false,
		MetricsMessage:   "Prometheus 未接入",
		PodCount:         total,
		HealthyPodCount:  healthy,
		PodRestartCount:  restarts,
		PendingPodCount:  pending,
		FailedPodCount:   failed,
		CrashLoopCount:   crashLoops,
		OOMKilledCount:   oomKilled,
		ObservedAt:       p.now().UTC(),
	}
	if nodes, nodesErr := client.CoreV1().Nodes().List(requestContext, metav1.ListOptions{}); nodesErr == nil {
		result.Nodes = nodeStatusOnly(nodes.Items)
		result.NodeCount = len(result.Nodes)
		for _, node := range result.Nodes {
			if node.Ready {
				result.ReadyNodeCount++
			}
		}
	}
	attachRunningPods(result.Nodes, list.Items, clusterID, p)
	monitoring, monitoringErr := p.CheckMonitoring(requestContext, clusterID)
	result.Monitoring = &monitoring
	if monitoringErr != nil || !monitoring.Available {
		if monitoringErr == nil && strings.TrimSpace(monitoring.Message) != "" {
			result.MetricsMessage = monitoring.Message
		}
		return result, nil
	}
	nodeMetrics, metricsErr := p.nodeMetrics(requestContext, client)
	if metricsErr != nil {
		result.MetricsMessage = "Prometheus 已接入，但暂时无法读取资源指标"
		return result, nil
	}
	result.MetricsAvailable = true
	result.MetricsMessage = "资源指标已接入"
	result.MetricsSource = "prometheus"
	result.Nodes = mergeNodeMetrics(result.Nodes, nodeMetrics)
	attachRunningPods(result.Nodes, list.Items, clusterID, p)
	result.NodeCount = len(result.Nodes)
	result.ReadyNodeCount = 0
	if aggregate, ok := aggregateNodeMetrics(nodeMetrics); ok {
		result.CPUUsedPercent = aggregate.CPUUsedPercent
		result.MemoryUsedPercent = aggregate.MemoryUsedPercent
		result.SwapUsedPercent = aggregate.SwapUsedPercent
		result.DiskUsedPercent = aggregate.DiskUsedPercent
		result.DiskReadMbps = aggregate.DiskReadMbps
		result.DiskWriteMbps = aggregate.DiskWriteMbps
		result.NetworkReceiveMbps = aggregate.NetworkReceiveMbps
		result.NetworkTransmitMbps = aggregate.NetworkTransmitMbps
		result.Load1 = aggregate.Load1
		result.Load5 = aggregate.Load5
		result.Load15 = aggregate.Load15
	}
	result.Series = []MetricPoint{clusterMetricPoint(result)}
	if monitoring.HistoryAvailable {
		if history, historyErr := p.prometheusHistory(requestContext, client, "", time.Hour); historyErr == nil && len(history) > 0 {
			result.Series = history
		} else if historyErr != nil {
			result.MetricsMessage = "实时指标已接入，但 Prometheus 历史数据暂不可用"
		}
	} else {
		result.Series = nil
	}
	return result, nil
}

func (p *KubernetesProvider) GetProjectMetrics(ctx context.Context, clusterID, projectID string) (ProjectMetrics, error) {
	return p.GetProjectMetricsInNamespace(ctx, clusterID, "", projectID)
}

// GetProjectMetricsInNamespace is the namespace-aware metrics extension. Pod
// status comes from Kubernetes, while resource values come from Prometheus.
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
	restarts, pending, failed, crashLoops, oomKilled := podStatusSummary(list.Items)
	result := ProjectMetrics{
		ClusterID:        clusterID,
		ProjectID:        projectID,
		MetricsAvailable: false,
		MetricsMessage:   "Prometheus 未接入",
		PodCount:         total,
		HealthyPodCount:  healthy,
		PodRestartCount:  restarts,
		PendingPodCount:  pending,
		FailedPodCount:   failed,
		CrashLoopCount:   crashLoops,
		OOMKilledCount:   oomKilled,
		ObservedAt:       p.now().UTC(),
	}
	monitoring, monitoringErr := p.CheckMonitoring(requestContext, clusterID)
	result.Monitoring = &monitoring
	if monitoringErr != nil || !monitoring.Available {
		if monitoringErr == nil && strings.TrimSpace(monitoring.Message) != "" {
			result.MetricsMessage = monitoring.Message
		}
		return result, nil
	}
	result.MetricsAvailable = true
	result.MetricsMessage = "资源指标已接入"
	result.MetricsSource = "prometheus"
	nodeMetrics, metricsErr := p.nodeMetrics(requestContext, client)
	if metricsErr != nil {
		result.MetricsAvailable = false
		result.MetricsMessage = "Prometheus 已接入，但暂时无法读取资源指标"
		return result, nil
	}
	projectNodes := make(map[string]bool)
	for _, pod := range list.Items {
		if pod.Spec.NodeName != "" {
			projectNodes[pod.Spec.NodeName] = true
		}
	}
	for _, node := range nodeMetrics {
		if projectNodes[node.Name] {
			result.Nodes = append(result.Nodes, node)
			result.CPUUsedPercent += node.CPUUsedPercent
			result.MemoryUsedPercent += node.MemoryUsedPercent
			result.NodeCount++
			if node.Ready {
				result.ReadyNodeCount++
			}
		}
	}
	if aggregate, ok := aggregateNodeMetrics(result.Nodes); ok {
		result.CPUUsedPercent = aggregate.CPUUsedPercent
		result.MemoryUsedPercent = aggregate.MemoryUsedPercent
		result.SwapUsedPercent = aggregate.SwapUsedPercent
		result.DiskUsedPercent = aggregate.DiskUsedPercent
		result.DiskReadMbps = aggregate.DiskReadMbps
		result.DiskWriteMbps = aggregate.DiskWriteMbps
		result.NetworkReceiveMbps = aggregate.NetworkReceiveMbps
		result.NetworkTransmitMbps = aggregate.NetworkTransmitMbps
		result.Load1 = aggregate.Load1
		result.Load5 = aggregate.Load5
		result.Load15 = aggregate.Load15
	}
	result.Series = []MetricPoint{projectMetricPoint(result)}
	if monitoring.HistoryAvailable {
		if history, historyErr := p.prometheusHistory(requestContext, client, namespace, time.Hour); historyErr == nil && len(history) > 0 {
			result.Series = history
		} else if historyErr != nil {
			result.MetricsMessage = "实时指标已接入，但 Prometheus 历史数据暂不可用"
		}
	} else {
		result.Series = nil
	}
	return result, nil
}

func clusterMetricPoint(metrics ClusterMetrics) MetricPoint {
	return MetricPoint{
		Timestamp:           metrics.ObservedAt,
		CPUUsedPercent:      metrics.CPUUsedPercent,
		MemoryUsedPercent:   metrics.MemoryUsedPercent,
		SwapUsedPercent:     metrics.SwapUsedPercent,
		DiskUsedPercent:     metrics.DiskUsedPercent,
		DiskReadMbps:        metrics.DiskReadMbps,
		DiskWriteMbps:       metrics.DiskWriteMbps,
		NetworkReceiveMbps:  metrics.NetworkReceiveMbps,
		NetworkTransmitMbps: metrics.NetworkTransmitMbps,
		Load1:               metrics.Load1,
		Load5:               metrics.Load5,
		Load15:              metrics.Load15,
		PodCount:            metrics.PodCount,
		HealthyPodCount:     metrics.HealthyPodCount,
		PodRestartCount:     metrics.PodRestartCount,
	}
}

func projectMetricPoint(metrics ProjectMetrics) MetricPoint {
	return MetricPoint{
		Timestamp:           metrics.ObservedAt,
		CPUUsedPercent:      metrics.CPUUsedPercent,
		MemoryUsedPercent:   metrics.MemoryUsedPercent,
		SwapUsedPercent:     metrics.SwapUsedPercent,
		DiskUsedPercent:     metrics.DiskUsedPercent,
		DiskReadMbps:        metrics.DiskReadMbps,
		DiskWriteMbps:       metrics.DiskWriteMbps,
		NetworkReceiveMbps:  metrics.NetworkReceiveMbps,
		NetworkTransmitMbps: metrics.NetworkTransmitMbps,
		Load1:               metrics.Load1,
		Load5:               metrics.Load5,
		Load15:              metrics.Load15,
		PodCount:            metrics.PodCount,
		HealthyPodCount:     metrics.HealthyPodCount,
		PodRestartCount:     metrics.PodRestartCount,
	}
}

type prometheusQueryResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		Result []struct {
			Values [][]json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

type prometheusInstantResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (p *KubernetesProvider) prometheusHistory(ctx context.Context, client kubernetes.Interface, namespace string, duration time.Duration) ([]MetricPoint, error) {
	end := time.Now().UTC()
	start := end.Add(-duration)
	type historyQuery struct {
		query    string
		required bool
		assign   func(*MetricPoint, float64)
	}
	queries := []historyQuery{
		{
			query:    `100 * (1 - avg(rate(node_cpu_seconds_total{job="node-exporter",mode="idle"}[5m])))`,
			required: true,
			assign:   func(point *MetricPoint, value float64) { point.CPUUsedPercent = value },
		},
		{
			query:    `100 * (1 - sum(node_memory_MemAvailable_bytes{job="node-exporter"}) / sum(node_memory_MemTotal_bytes{job="node-exporter"}))`,
			required: true,
			assign:   func(point *MetricPoint, value float64) { point.MemoryUsedPercent = value },
		},
		{
			query:  `100 * (1 - sum(node_memory_SwapFree_bytes{job="node-exporter"}) / sum(node_memory_SwapTotal_bytes{job="node-exporter"}))`,
			assign: func(point *MetricPoint, value float64) { point.SwapUsedPercent = value },
		},
		{
			query:  `100 * (1 - sum(node_filesystem_avail_bytes{job="node-exporter",mountpoint="/"}) / sum(node_filesystem_size_bytes{job="node-exporter",mountpoint="/"}))`,
			assign: func(point *MetricPoint, value float64) { point.DiskUsedPercent = value },
		},
		{
			query:  `sum(rate(node_disk_read_bytes_total{job="node-exporter",device!~"^(loop|ram|fd)"}[5m])) / 1024 / 1024`,
			assign: func(point *MetricPoint, value float64) { point.DiskReadMbps = value },
		},
		{
			query:  `sum(rate(node_disk_written_bytes_total{job="node-exporter",device!~"^(loop|ram|fd)"}[5m])) / 1024 / 1024`,
			assign: func(point *MetricPoint, value float64) { point.DiskWriteMbps = value },
		},
		{
			query:  `sum(rate(node_network_receive_bytes_total{job="node-exporter",device!="lo"}[5m])) / 1024 / 1024`,
			assign: func(point *MetricPoint, value float64) { point.NetworkReceiveMbps = value },
		},
		{
			query:  `sum(rate(node_network_transmit_bytes_total{job="node-exporter",device!="lo"}[5m])) / 1024 / 1024`,
			assign: func(point *MetricPoint, value float64) { point.NetworkTransmitMbps = value },
		},
		{
			query:  `sum(node_load1{job="node-exporter"})`,
			assign: func(point *MetricPoint, value float64) { point.Load1 = value },
		},
		{
			query:  `sum(node_load5{job="node-exporter"})`,
			assign: func(point *MetricPoint, value float64) { point.Load5 = value },
		},
		{
			query:  `sum(node_load15{job="node-exporter"})`,
			assign: func(point *MetricPoint, value float64) { point.Load15 = value },
		},
		{
			query:  `sum(kube_pod_info{job="kube-state-metrics"})`,
			assign: func(point *MetricPoint, value float64) { point.PodCount = int(value) },
		},
		{
			query:  `sum(kube_pod_status_ready{job="kube-state-metrics",condition="true"})`,
			assign: func(point *MetricPoint, value float64) { point.HealthyPodCount = int(value) },
		},
		{
			query:  `sum(increase(kube_pod_container_status_restarts_total{job="kube-state-metrics"}[5m]))`,
			assign: func(point *MetricPoint, value float64) { point.PodRestartCount = int(value) },
		},
	}
	if namespace != "" {
		queries = queries[:0]
		queries = append(queries,
			historyQuery{
				query:    `100 * sum(rate(container_cpu_usage_seconds_total{job="kubernetes-nodes-cadvisor",container!="",container!="POD",pod!="",namespace="` + namespace + `"}` + `[5m])) / sum(machine_cpu_cores)`,
				required: true,
				assign:   func(point *MetricPoint, value float64) { point.CPUUsedPercent = value },
			},
			historyQuery{
				query:    `100 * sum(container_memory_working_set_bytes{job="kubernetes-nodes-cadvisor",container!="",container!="POD",pod!="",namespace="` + namespace + `"}` + `) / sum(machine_memory_bytes)`,
				required: true,
				assign:   func(point *MetricPoint, value float64) { point.MemoryUsedPercent = value },
			},
			historyQuery{
				query:  `sum(kube_pod_info{job="kube-state-metrics",namespace="` + namespace + `"})`,
				assign: func(point *MetricPoint, value float64) { point.PodCount = int(value) },
			},
			historyQuery{
				query:  `sum(kube_pod_status_ready{job="kube-state-metrics",condition="true",namespace="` + namespace + `"})`,
				assign: func(point *MetricPoint, value float64) { point.HealthyPodCount = int(value) },
			},
			historyQuery{
				query:  `sum(increase(kube_pod_container_status_restarts_total{job="kube-state-metrics",namespace="` + namespace + `"}` + `[5m]))`,
				assign: func(point *MetricPoint, value float64) { point.PodRestartCount = int(value) },
			},
		)
	}
	points := map[int64]*MetricPoint{}
	for _, item := range queries {
		values, err := p.queryPrometheus(ctx, client, item.query, start, end)
		if err != nil {
			if item.required {
				return nil, err
			}
			continue
		}
		for _, value := range values {
			timestamp, parsed, ok := parsePrometheusSample(value)
			if !ok {
				continue
			}
			key := int64(timestamp)
			if points[key] == nil {
				points[key] = &MetricPoint{Timestamp: time.Unix(key, 0).UTC()}
			}
			item.assign(points[key], parsed)
		}
	}
	result := make([]MetricPoint, 0, len(points))
	for _, point := range points {
		result = append(result, *point)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Timestamp.Before(result[right].Timestamp) })
	return result, nil
}

func (p *KubernetesProvider) queryPrometheus(ctx context.Context, client kubernetes.Interface, query string, start, end time.Time) ([][]json.RawMessage, error) {
	request := client.CoreV1().RESTClient().Get().AbsPath("/api/v1/namespaces/" + prometheusNamespace + "/services/http:" + prometheusName + ":9090/proxy/api/v1/query_range")
	request = request.Param("query", query).
		Param("start", fmt.Sprintf("%.0f", float64(start.Unix()))).
		Param("end", fmt.Sprintf("%.0f", float64(end.Unix()))).
		Param("step", "60")
	response, err := request.Timeout(10 * time.Second).Do(ctx).Raw()
	if err != nil {
		return nil, err
	}
	var payload prometheusQueryResponse
	if err := json.Unmarshal(response, &payload); err != nil {
		return nil, err
	}
	if payload.Status != "" && payload.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed: %s", payload.Error)
	}
	if len(payload.Data.Result) == 0 {
		return nil, fmt.Errorf("prometheus returned no series")
	}
	return payload.Data.Result[0].Values, nil
}

func (p *KubernetesProvider) queryPrometheusInstant(ctx context.Context, client kubernetes.Interface, query string) ([]struct {
	Metric map[string]string
	Value  []json.RawMessage
}, error) {
	request := client.CoreV1().RESTClient().Get().AbsPath("/api/v1/namespaces/" + prometheusNamespace + "/services/http:" + prometheusName + ":9090/proxy/api/v1/query")
	response, err := request.Param("query", query).Timeout(10 * time.Second).Do(ctx).Raw()
	if err != nil {
		return nil, err
	}
	var payload prometheusInstantResponse
	if err := json.Unmarshal(response, &payload); err != nil {
		return nil, err
	}
	if payload.Status != "" && payload.Status != "success" {
		return nil, fmt.Errorf("prometheus instant query failed: %s", payload.Error)
	}
	result := make([]struct {
		Metric map[string]string
		Value  []json.RawMessage
	}, len(payload.Data.Result))
	for index, item := range payload.Data.Result {
		result[index].Metric = item.Metric
		result[index].Value = item.Value
	}
	return result, nil
}

func parsePrometheusSample(value []json.RawMessage) (float64, float64, bool) {
	if len(value) != 2 {
		return 0, 0, false
	}
	var timestamp float64
	var raw string
	if json.Unmarshal(value[0], &timestamp) != nil || json.Unmarshal(value[1], &raw) != nil {
		return 0, 0, false
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, 0, false
	}
	return timestamp, parsed, true
}

func (p *KubernetesProvider) nodeMetrics(ctx context.Context, client kubernetes.Interface) ([]NodeMetrics, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := make(map[string]*NodeMetrics, len(nodes.Items))
	for _, node := range nodes.Items {
		result[node.Name] = &NodeMetrics{Name: node.Name, Ready: nodeReady(node)}
	}
	queries := []struct {
		key      string
		query    string
		required bool
		assign   func(*NodeMetrics, float64)
	}{
		{key: "cpu", required: true, query: `100 * (1 - avg by (kubernetes_node) (rate(node_cpu_seconds_total{job="node-exporter",mode="idle"}[5m])))`, assign: func(node *NodeMetrics, value float64) { node.CPUUsedPercent = value }},
		{key: "memory", required: true, query: `100 * (1 - node_memory_MemAvailable_bytes{job="node-exporter"} / node_memory_MemTotal_bytes{job="node-exporter"})`, assign: func(node *NodeMetrics, value float64) { node.MemoryUsedPercent = value }},
		{key: "swap", query: `100 * (1 - node_memory_SwapFree_bytes{job="node-exporter"} / node_memory_SwapTotal_bytes{job="node-exporter"})`, assign: func(node *NodeMetrics, value float64) { node.SwapUsedPercent = value }},
		{key: "disk", query: `100 * (1 - node_filesystem_avail_bytes{job="node-exporter",mountpoint="/"} / node_filesystem_size_bytes{job="node-exporter",mountpoint="/"})`, assign: func(node *NodeMetrics, value float64) { node.DiskUsedPercent = value }},
		{key: "disk_read", query: `sum by (kubernetes_node) (rate(node_disk_read_bytes_total{job="node-exporter",device!~"^(loop|ram|fd)"}[5m])) / 1024 / 1024`, assign: func(node *NodeMetrics, value float64) { node.DiskReadMbps = value }},
		{key: "disk_write", query: `sum by (kubernetes_node) (rate(node_disk_written_bytes_total{job="node-exporter",device!~"^(loop|ram|fd)"}[5m])) / 1024 / 1024`, assign: func(node *NodeMetrics, value float64) { node.DiskWriteMbps = value }},
		{key: "network_receive", query: `sum by (kubernetes_node) (rate(node_network_receive_bytes_total{job="node-exporter",device!="lo"}[5m])) / 1024 / 1024`, assign: func(node *NodeMetrics, value float64) { node.NetworkReceiveMbps = value }},
		{key: "network_transmit", query: `sum by (kubernetes_node) (rate(node_network_transmit_bytes_total{job="node-exporter",device!="lo"}[5m])) / 1024 / 1024`, assign: func(node *NodeMetrics, value float64) { node.NetworkTransmitMbps = value }},
		{key: "load1", query: `node_load1{job="node-exporter"}`, assign: func(node *NodeMetrics, value float64) { node.Load1 = value }},
		{key: "load5", query: `node_load5{job="node-exporter"}`, assign: func(node *NodeMetrics, value float64) { node.Load5 = value }},
		{key: "load15", query: `node_load15{job="node-exporter"}`, assign: func(node *NodeMetrics, value float64) { node.Load15 = value }},
	}
	for _, item := range queries {
		series, queryErr := p.queryPrometheusInstant(ctx, client, item.query)
		if queryErr != nil {
			if item.required {
				return nil, queryErr
			}
			continue
		}
		matched := 0
		for _, sample := range series {
			_, value, ok := parsePrometheusSample(sample.Value)
			if !ok {
				continue
			}
			nodeName := sample.Metric["kubernetes_node"]
			if nodeName == "" {
				nodeName = sample.Metric["node"]
			}
			node := result[nodeName]
			if node == nil {
				continue
			}
			item.assign(node, value)
			matched++
		}
		if item.required && matched == 0 {
			return nil, fmt.Errorf("Prometheus returned no %s node metrics", item.key)
		}
	}
	output := make([]NodeMetrics, 0, len(result))
	for _, node := range result {
		output = append(output, *node)
	}
	sort.Slice(output, func(left, right int) bool { return output[left].Name < output[right].Name })
	return output, nil
}

func nodeStatusOnly(nodes []corev1.Node) []NodeMetrics {
	result := make([]NodeMetrics, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, NodeMetrics{Name: node.Name, Ready: nodeReady(node)})
	}
	return result
}

func nodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func aggregateNodeMetrics(nodes []NodeMetrics) (NodeMetrics, bool) {
	if len(nodes) == 0 {
		return NodeMetrics{}, false
	}
	var result NodeMetrics
	for _, node := range nodes {
		result.CPUUsedPercent += node.CPUUsedPercent
		result.MemoryUsedPercent += node.MemoryUsedPercent
		result.SwapUsedPercent += node.SwapUsedPercent
		result.DiskUsedPercent += node.DiskUsedPercent
		result.DiskReadMbps += node.DiskReadMbps
		result.DiskWriteMbps += node.DiskWriteMbps
		result.NetworkReceiveMbps += node.NetworkReceiveMbps
		result.NetworkTransmitMbps += node.NetworkTransmitMbps
		result.Load1 += node.Load1
		result.Load5 += node.Load5
		result.Load15 += node.Load15
	}
	count := float64(len(nodes))
	result.CPUUsedPercent /= count
	result.MemoryUsedPercent /= count
	result.SwapUsedPercent /= count
	result.DiskUsedPercent /= count
	result.Load1 /= count
	result.Load5 /= count
	result.Load15 /= count
	return result, true
}

func attachRunningPods(nodes []NodeMetrics, pods []corev1.Pod, clusterID string, provider *KubernetesProvider) {
	byNode := make(map[string][]Pod)
	for _, item := range pods {
		if item.Status.Phase != corev1.PodRunning || item.Spec.NodeName == "" {
			continue
		}
		byNode[item.Spec.NodeName] = append(byNode[item.Spec.NodeName], provider.podFromKubernetes(clusterID, item, ""))
	}
	for index := range nodes {
		nodes[index].Pods = byNode[nodes[index].Name]
		nodes[index].PodCount = len(nodes[index].Pods)
	}
}

func mergeNodeMetrics(statusNodes, metricNodes []NodeMetrics) []NodeMetrics {
	byName := make(map[string]NodeMetrics, len(metricNodes))
	for _, node := range metricNodes {
		byName[node.Name] = node
	}
	result := make([]NodeMetrics, 0, len(statusNodes)+len(metricNodes))
	seen := make(map[string]bool, len(statusNodes)+len(metricNodes))
	for _, node := range statusNodes {
		if metric, ok := byName[node.Name]; ok {
			metric.Ready = node.Ready
			result = append(result, metric)
		} else {
			result = append(result, node)
		}
		seen[node.Name] = true
	}
	for _, node := range metricNodes {
		if !seen[node.Name] {
			result = append(result, node)
		}
	}
	return result
}

func percentage(used, capacity float64) float64 {
	if capacity <= 0 {
		return 0
	}
	return used / capacity * 100
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
	if projectID == "" {
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

func (p *KubernetesProvider) upsertConfigMap(ctx context.Context, client kubernetes.Interface, deployment *appsv1.Deployment, projectID, targetID string, values map[string]string) error {
	name := p.configMapName(deployment.Name)
	configMaps := client.CoreV1().ConfigMaps(deployment.Namespace)
	configMap, err := configMaps.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		data := cloneStringMap(values)
		labels := make(map[string]string)
		if projectID != "" {
			labels[p.projectLabelKey] = projectID
		}
		if targetID != "" {
			labels[targetLabelKey] = targetID
			labels[managedByLabelKey] = managedByLabelValue
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
	if targetID != "" {
		configMap.Labels[targetLabelKey] = targetID
		configMap.Labels[managedByLabelKey] = managedByLabelValue
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
	if projectID == "" {
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

func podStatusSummary(pods []corev1.Pod) (restarts, pending, failed, crashLoops, oomKilled int) {
	for _, pod := range pods {
		switch pod.Status.Phase {
		case corev1.PodPending:
			pending++
		case corev1.PodFailed:
			failed++
		}
		for _, status := range pod.Status.ContainerStatuses {
			restarts += int(status.RestartCount)
			if status.State.Waiting != nil && status.State.Waiting.Reason == "CrashLoopBackOff" {
				crashLoops++
			}
			if status.State.Terminated != nil && status.State.Terminated.Reason == "OOMKilled" {
				oomKilled++
			}
			if status.LastTerminationState.Terminated != nil && status.LastTerminationState.Terminated.Reason == "OOMKilled" {
				oomKilled++
			}
		}
	}
	return restarts, pending, failed, crashLoops, oomKilled
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
