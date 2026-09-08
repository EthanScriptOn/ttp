package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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
