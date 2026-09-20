package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

func TestKubernetesProviderListsGetsLogsAndMetrics(t *testing.T) {
	client := fake.NewSimpleClientset(testDeployment(), testPod(), testConfigMap())
	provider := NewKubernetesProvider()
	var _ NamespaceAwareProvider = provider
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}

	pods, err := provider.ListPodsInNamespace(context.Background(), "cluster-a", "lab", "checkout")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].ProjectID != "checkout" || !pods[0].Ready || pods[0].Phase != PodRunning {
		t.Fatalf("unexpected pod list: %#v", pods)
	}

	detail, err := provider.GetPod(context.Background(), PodRef{ClusterID: "cluster-a", Namespace: "lab", Name: "checkout-abc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.GetPod(context.Background(), PodRef{ClusterID: "cluster-a", Namespace: "lab", Name: "checkout-abc", ProjectID: "another-project"}); !errors.Is(err, ErrPodNotFound) {
		t.Fatalf("cross-project pod lookup error = %v", err)
	}
	if detail.Containers["api"].Image != "example/checkout:v1" || detail.Environment["LOG_LEVEL"] != "info" || detail.Config["CHECKOUT_MODE"] != "safe" {
		t.Fatalf("unexpected pod detail: %#v", detail)
	}
	if detail.RestartCount != 2 {
		t.Fatalf("unexpected pod restart count: %d", detail.RestartCount)
	}

	logs, err := provider.GetPodLogs(context.Background(), PodLogRequest{PodRef: PodRef{ClusterID: "cluster-a", Namespace: "lab", Name: "checkout-abc"}, Container: "api", TailLines: 20})
	if err != nil {
		t.Fatal(err)
	}
	if logs != "fake logs" {
		t.Fatalf("logs = %q, want fake logs", logs)
	}

	clusterMetrics, err := provider.GetClusterMetrics(context.Background(), "cluster-a")
	if err != nil || clusterMetrics.PodCount != 1 || clusterMetrics.HealthyPodCount != 1 || clusterMetrics.CPUUsedPercent != 0 || clusterMetrics.MemoryUsedPercent != 0 {
		t.Fatalf("unexpected cluster metrics: %#v, %v", clusterMetrics, err)
	}
	projectMetrics, err := provider.GetProjectMetrics(context.Background(), "cluster-a", "checkout")
	if err != nil || projectMetrics.PodCount != 1 || projectMetrics.HealthyPodCount != 1 || projectMetrics.CPUUsedPercent != 0 || projectMetrics.MemoryUsedPercent != 0 {
		t.Fatalf("unexpected project metrics: %#v, %v", projectMetrics, err)
	}
}

func TestKubernetesProviderUpdatesDeploymentAndNotPod(t *testing.T) {
	client := fake.NewSimpleClientset(testDeployment(), testPod(), testConfigMap())
	provider := NewKubernetesProvider(WithKubernetesTimeout(time.Second))
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	ref := PodRef{ClusterID: "cluster-a", Namespace: "lab", Name: "checkout-abc"}
	detail, err := provider.UpdatePodConfig(context.Background(), ref, PodConfigUpdate{
		Config:      map[string]string{"CHECKOUT_MODE": "fast"},
		Environment: map[string]string{"LOG_LEVEL": "debug", "TRACE": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Environment["LOG_LEVEL"] != "debug" || detail.Environment["TRACE"] != "1" || detail.Config["CHECKOUT_MODE"] != "fast" {
		t.Fatalf("returned detail does not include desired values: %#v", detail)
	}

	deployment, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if !hasConfigMapEnvFrom(container, "checkout-cicd-config") {
		t.Fatalf("deployment is missing config map env source: %#v", container.EnvFrom)
	}
	if got := envValue(container.Env, "LOG_LEVEL"); got != "debug" {
		t.Fatalf("LOG_LEVEL = %q, want debug", got)
	}
	if got := envValue(container.Env, "TRACE"); got != "1" {
		t.Fatalf("TRACE = %q, want 1", got)
	}
	if deployment.Spec.Template.Annotations[rolloutVersionKey] == "" {
		t.Fatal("deployment template was not annotated for rollout")
	}
	configMap, err := client.CoreV1().ConfigMaps("lab").Get(context.Background(), "checkout-cicd-config", metav1.GetOptions{})
	if err != nil || configMap.Data["CHECKOUT_MODE"] != "fast" {
		t.Fatalf("unexpected config map: %#v, %v", configMap, err)
	}

	unchangedPod, err := client.CoreV1().Pods("lab").Get(context.Background(), ref.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if envValue(unchangedPod.Spec.Containers[0].Env, "LOG_LEVEL") != "info" {
		t.Fatal("pod itself was mutated; configuration must live on the Deployment")
	}
}

func TestKubernetesProviderUsesCustomProjectLabelForConfigMap(t *testing.T) {
	const customLabel = "platform.example.com/project"
	deployment := testDeployment()
	deployment.Spec.Template.Labels = map[string]string{customLabel: "checkout"}
	client := fake.NewSimpleClientset(deployment, testPod(), testConfigMap())
	provider := NewKubernetesProvider(WithProjectLabelKey(customLabel))
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}

	if _, err := provider.UpdatePodConfig(context.Background(), PodRef{ClusterID: "cluster-a", Namespace: "lab", Name: "checkout-abc"}, PodConfigUpdate{Config: map[string]string{"CHECKOUT_MODE": "fast"}}); err != nil {
		t.Fatal(err)
	}
	configMap, err := client.CoreV1().ConfigMaps("lab").Get(context.Background(), "checkout-cicd-config", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if configMap.Labels[customLabel] != "checkout" {
		t.Fatalf("custom project label = %q, want checkout; labels=%#v", configMap.Labels[customLabel], configMap.Labels)
	}
	if _, ok := configMap.Labels[ProjectLabelKey]; ok {
		t.Fatalf("default project label should not be written when custom key is configured: %#v", configMap.Labels)
	}
}

func TestKubernetesProviderProjectMetricsIsNamespaceAware(t *testing.T) {
	otherNamespacePod := testPod()
	otherNamespacePod.Name = "checkout-other"
	otherNamespacePod.Namespace = "other"
	client := fake.NewSimpleClientset(testPod(), otherNamespacePod)
	provider := NewKubernetesProvider()
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}

	metrics, err := provider.GetProjectMetricsInNamespace(context.Background(), "cluster-a", "lab", "checkout")
	if err != nil {
		t.Fatal(err)
	}
	if metrics.PodCount != 1 || metrics.HealthyPodCount != 1 || metrics.ProjectID != "checkout" || metrics.ClusterID != "cluster-a" {
		t.Fatalf("namespace project metrics = %#v", metrics)
	}

	allMetrics, err := provider.GetProjectMetrics(context.Background(), "cluster-a", "checkout")
	if err != nil {
		t.Fatal(err)
	}
	if allMetrics.PodCount != 2 {
		t.Fatalf("non-namespace project metrics = %#v, want both pods", allMetrics)
	}
}

func TestKubernetesProviderValidationAndErrors(t *testing.T) {
	provider := NewKubernetesProvider()
	if err := provider.RegisterClient("cluster-a", fake.NewSimpleClientset()); err != nil {
		t.Fatal(err)
	}
	_, err := provider.ListPodsInNamespace(context.Background(), "cluster-a", "bad_namespace", "checkout")
	if !errors.Is(err, ErrInvalidKubernetesInput) {
		t.Fatalf("invalid namespace error = %v", err)
	}
	_, err = provider.GetPod(context.Background(), PodRef{ClusterID: "missing", Namespace: "lab", Name: "api"})
	if !errors.Is(err, ErrClusterNotFound) {
		t.Fatalf("missing cluster error = %v", err)
	}
	_, err = provider.UpdatePodConfig(context.Background(), PodRef{ClusterID: "cluster-a", Namespace: "lab", Name: "checkout-abc"}, PodConfigUpdate{Environment: map[string]string{"BAD/NAME": "x"}})
	if !errors.Is(err, ErrInvalidKubernetesInput) {
		t.Fatalf("invalid environment key error = %v", err)
	}
	_, err = provider.GetPodLogs(context.Background(), PodLogRequest{PodRef: PodRef{ClusterID: "cluster-a", Namespace: "lab", Name: "api"}, TailLines: -1})
	if !errors.Is(err, ErrInvalidKubernetesInput) {
		t.Fatalf("invalid tail error = %v", err)
	}
}

func TestKubernetesProviderEnsuresOwnedNamespace(t *testing.T) {
	client := fake.NewSimpleClientset()
	provider := NewKubernetesProvider()
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	labels := NamespaceLabels("space-lab", "dev")
	if err := provider.EnsureNamespace(context.Background(), "cluster-a", "ttp-lab-dev", labels); err != nil {
		t.Fatal(err)
	}
	created, err := client.CoreV1().Namespaces().Get(context.Background(), "ttp-lab-dev", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !namespaceLabelsMatch(created.Labels, labels) {
		t.Fatalf("namespace labels = %#v, want %#v", created.Labels, labels)
	}
	if err := provider.EnsureNamespace(context.Background(), "cluster-a", "ttp-lab-dev", labels); err != nil {
		t.Fatalf("re-ensuring owned namespace: %v", err)
	}
	if err := provider.EnsureNamespace(context.Background(), "cluster-a", "ttp-lab-dev", NamespaceLabels("space-other", "dev")); !errors.Is(err, ErrNamespaceOwnershipConflict) {
		t.Fatalf("foreign namespace ownership error = %v", err)
	}
}

func TestKubernetesProviderEnsuresNamespaceQuotaAndLimitRange(t *testing.T) {
	client := fake.NewSimpleClientset()
	provider := NewKubernetesProvider(WithKubernetesRuntimeServiceAccount("platform-system", "platform-runtime"))
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	quota := testNamespaceQuota()
	labels := NamespaceLabels("space-lab", "dev")
	if err := provider.EnsureNamespaceWithQuota(context.Background(), "cluster-a", "ttp-lab-dev", labels, quota); err != nil {
		t.Fatal(err)
	}

	resourceQuota, err := client.CoreV1().ResourceQuotas("ttp-lab-dev").Get(context.Background(), namespaceResourceQuotaName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cpuLimit := resourceQuota.Spec.Hard[corev1.ResourceLimitsCPU]
	if got := cpuLimit.String(); got != "4" {
		t.Fatalf("cpu limit = %q, want 4", got)
	}
	storageQuota := resourceQuota.Spec.Hard[corev1.ResourceRequestsStorage]
	if got := storageQuota.String(); got != "50Gi" {
		t.Fatalf("storage quota = %q, want 50Gi", got)
	}
	podQuota := resourceQuota.Spec.Hard[corev1.ResourcePods]
	if got := podQuota.Value(); got != 20 {
		t.Fatalf("pod quota = %d, want 20", got)
	}
	if !namespaceLabelsMatch(resourceQuota.Labels, labels) {
		t.Fatalf("resource quota labels = %#v, want %#v", resourceQuota.Labels, labels)
	}
	roleBinding, err := client.RbacV1().RoleBindings("ttp-lab-dev").Get(context.Background(), namespaceAccessRoleBindingName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !namespaceLabelsMatch(roleBinding.Labels, labels) {
		t.Fatalf("role binding labels = %#v, want %#v", roleBinding.Labels, labels)
	}
	if roleBinding.RoleRef.Kind != "ClusterRole" || roleBinding.RoleRef.Name != namespaceAccessClusterRoleName {
		t.Fatalf("role binding roleRef = %#v", roleBinding.RoleRef)
	}
	if len(roleBinding.Subjects) != 1 || roleBinding.Subjects[0].Namespace != "platform-system" || roleBinding.Subjects[0].Name != "platform-runtime" {
		t.Fatalf("role binding subjects = %#v", roleBinding.Subjects)
	}

	limitRange, err := client.CoreV1().LimitRanges("ttp-lab-dev").Get(context.Background(), namespaceLimitRangeName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(limitRange.Spec.Limits) != 1 {
		t.Fatalf("limit range entries = %d, want 1", len(limitRange.Spec.Limits))
	}
	item := limitRange.Spec.Limits[0]
	defaultCPULimit := item.Default[corev1.ResourceCPU]
	if got := defaultCPULimit.String(); got != "500m" {
		t.Fatalf("default cpu limit = %q, want 500m", got)
	}
	defaultMemoryRequest := item.DefaultRequest[corev1.ResourceMemory]
	if got := defaultMemoryRequest.String(); got != "128Mi" {
		t.Fatalf("default memory request = %q, want 128Mi", got)
	}

	updated := quota
	updated.CPULimit = "6"
	if err := provider.EnsureNamespaceWithQuota(context.Background(), "cluster-a", "ttp-lab-dev", labels, updated); err != nil {
		t.Fatal(err)
	}
	resourceQuota, err = client.CoreV1().ResourceQuotas("ttp-lab-dev").Get(context.Background(), namespaceResourceQuotaName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cpuLimit = resourceQuota.Spec.Hard[corev1.ResourceLimitsCPU]
	if got := cpuLimit.String(); got != "6" {
		t.Fatalf("updated cpu limit = %q, want 6", got)
	}
}

func TestKubernetesProviderRefusesForeignNamespaceRuntimeBinding(t *testing.T) {
	labels := NamespaceLabels("space-lab", "dev")
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ttp-lab-dev", Labels: labels}}
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: namespaceAccessRoleBindingName, Namespace: "ttp-lab-dev"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "other"},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Namespace: "other", Name: "other"}},
	}
	client := fake.NewSimpleClientset(namespace, binding)
	provider := NewKubernetesProvider()
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	if err := provider.EnsureNamespaceWithQuota(context.Background(), "cluster-a", "ttp-lab-dev", labels, testNamespaceQuota()); !errors.Is(err, ErrNamespaceOwnershipConflict) {
		t.Fatalf("foreign role binding error = %v, want ownership conflict", err)
	}
}

func testNamespaceQuota() domain.NamespaceQuota {
	return domain.NamespaceQuota{
		CPURequest:                     "2",
		CPULimit:                       "4",
		MemoryRequest:                  "2Gi",
		MemoryLimit:                    "4Gi",
		EphemeralStorageRequest:        "10Gi",
		EphemeralStorageLimit:          "20Gi",
		Storage:                        "50Gi",
		Pods:                           20,
		PersistentVolumeClaims:         10,
		DefaultCPURequest:              "100m",
		DefaultCPULimit:                "500m",
		DefaultMemoryRequest:           "128Mi",
		DefaultMemoryLimit:             "512Mi",
		DefaultEphemeralStorageRequest: "256Mi",
		DefaultEphemeralStorageLimit:   "1Gi",
	}
}

func testDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "checkout", Namespace: "lab", UID: "deployment-uid"},
		Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "example/checkout:v1", Env: []corev1.EnvVar{{Name: "LOG_LEVEL", Value: "info"}}, EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "checkout-settings"}}}}}}}}},
	}
}

func testPod() *corev1.Pod {
	return testPodWithLabel(ProjectLabelKey, "checkout")
}

func testPodWithLabel(labelKey, projectID string) *corev1.Pod {
	ready := corev1.ConditionTrue
	started := metav1.NewTime(time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "checkout-abc", Namespace: "lab", Labels: map[string]string{labelKey: projectID}, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "checkout", UID: "deployment-uid"}}},
		Spec:       corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "api", Image: "example/checkout:v1", Env: []corev1.EnvVar{{Name: "LOG_LEVEL", Value: "info"}}, EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "checkout-settings"}}}}}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.4", StartTime: &started, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}}, ContainerStatuses: []corev1.ContainerStatus{{Name: "api", Ready: true, RestartCount: 2}}},
	}
}

func testConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "checkout-settings", Namespace: "lab"}, Data: map[string]string{"CHECKOUT_MODE": "safe"}}
}

func envValue(values []corev1.EnvVar, name string) string {
	for _, item := range values {
		if item.Name == name {
			return item.Value
		}
	}
	return ""
}
