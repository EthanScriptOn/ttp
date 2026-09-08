package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	cleanupClusterID = "cluster-cleanup"
	cleanupNamespace = "cleanup"
	cleanupProjectID = "project-cleanup"
	cleanupTargetID  = "target-cleanup"
	otherTargetID    = "target-other"
)

func TestKubernetesProviderCleanupEnvironmentDeletesOnlyOwnedResources(t *testing.T) {
	client := fake.NewSimpleClientset(cleanupObjects(cleanupTargetID, true)...)
	provider := NewKubernetesProvider(
		WithKubernetesTimeout(time.Second),
		WithKubernetesRolloutPollInterval(time.Millisecond),
	)
	if err := provider.RegisterClient(cleanupClusterID, client); err != nil {
		t.Fatal(err)
	}

	if err := provider.CleanupEnvironment(context.Background(), cleanupClusterID, cleanupNamespace, cleanupProjectID, cleanupTargetID); err != nil {
		t.Fatalf("cleanup environment: %v", err)
	}

	assertNotFound := func(name string, get func() error) {
		t.Helper()
		if err := get(); !apierrors.IsNotFound(err) {
			t.Fatalf("%s error = %v, want NotFound", name, err)
		}
	}
	assertNotFound("Deployment/owned", func() error {
		_, err := client.AppsV1().Deployments(cleanupNamespace).Get(context.Background(), "owned", metav1.GetOptions{})
		return err
	})
	assertNotFound("Pod/owned", func() error {
		_, err := client.CoreV1().Pods(cleanupNamespace).Get(context.Background(), "owned-pod", metav1.GetOptions{})
		return err
	})
	assertNotFound("ReplicaSet/owned", func() error {
		_, err := client.AppsV1().ReplicaSets(cleanupNamespace).Get(context.Background(), "owned-rs", metav1.GetOptions{})
		return err
	})
	assertNotFound("HPA/owned", func() error {
		_, err := client.AutoscalingV2().HorizontalPodAutoscalers(cleanupNamespace).Get(context.Background(), "owned-hpa", metav1.GetOptions{})
		return err
	})
	assertNotFound("Ingress/owned", func() error {
		_, err := client.NetworkingV1().Ingresses(cleanupNamespace).Get(context.Background(), "owned-ingress", metav1.GetOptions{})
		return err
	})
	assertNotFound("Service/owned", func() error {
		_, err := client.CoreV1().Services(cleanupNamespace).Get(context.Background(), "owned-service", metav1.GetOptions{})
		return err
	})
	assertNotFound("ConfigMap/owned", func() error {
		_, err := client.CoreV1().ConfigMaps(cleanupNamespace).Get(context.Background(), "owned-config", metav1.GetOptions{})
		return err
	})
	assertNotFound("Secret/owned", func() error {
		_, err := client.CoreV1().Secrets(cleanupNamespace).Get(context.Background(), "owned-secret", metav1.GetOptions{})
		return err
	})

	if _, err := client.AppsV1().Deployments(cleanupNamespace).Get(context.Background(), "other", metav1.GetOptions{}); err != nil {
		t.Fatalf("resource from another target was deleted: %v", err)
	}
	if _, err := client.CoreV1().Services(cleanupNamespace).Get(context.Background(), "other-service", metav1.GetOptions{}); err != nil {
		t.Fatalf("service from another target was deleted: %v", err)
	}
}

func TestKubernetesProviderCleanupEnvironmentRejectsUnscopedResourcesBeforeDeleting(t *testing.T) {
	objects := cleanupObjects(cleanupTargetID, false)
	objects = append(objects,
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "legacy-config", Namespace: cleanupNamespace,
			Labels: map[string]string{ProjectLabelKey: cleanupProjectID},
		}},
	)
	client := fake.NewSimpleClientset(objects...)
	provider := NewKubernetesProvider(WithKubernetesTimeout(time.Second))
	if err := provider.RegisterClient(cleanupClusterID, client); err != nil {
		t.Fatal(err)
	}

	err := provider.CleanupEnvironment(context.Background(), cleanupClusterID, cleanupNamespace, cleanupProjectID, cleanupTargetID)
	if !errors.Is(err, ErrEnvironmentCleanupFailed) {
		t.Fatalf("cleanup error = %v, want ErrEnvironmentCleanupFailed", err)
	}
	if _, err := client.AppsV1().Deployments(cleanupNamespace).Get(context.Background(), "owned", metav1.GetOptions{}); err != nil {
		t.Fatalf("preflight failure deleted owned Deployment: %v", err)
	}
}

func TestKubernetesProviderCleanupEnvironmentReturnsFailureWhenDeleteIsForbidden(t *testing.T) {
	client := fake.NewSimpleClientset(cleanupObjects(cleanupTargetID, false)...)
	client.PrependReactor("delete", "deployments", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, apierrors.NewForbidden(appsv1.Resource("deployments"), "owned", errors.New("denied"))
	})
	provider := NewKubernetesProvider(WithKubernetesTimeout(time.Second))
	if err := provider.RegisterClient(cleanupClusterID, client); err != nil {
		t.Fatal(err)
	}

	err := provider.CleanupEnvironment(context.Background(), cleanupClusterID, cleanupNamespace, cleanupProjectID, cleanupTargetID)
	if !errors.Is(err, ErrEnvironmentCleanupFailed) {
		t.Fatalf("cleanup error = %v, want ErrEnvironmentCleanupFailed", err)
	}
	if _, err := client.AppsV1().Deployments(cleanupNamespace).Get(context.Background(), "owned", metav1.GetOptions{}); err != nil {
		t.Fatalf("failed cleanup removed Deployment: %v", err)
	}
}

func TestKubernetesProviderCleanupEnvironmentSucceedsWhenNoResourcesExist(t *testing.T) {
	provider := NewKubernetesProvider(WithKubernetesTimeout(time.Second))
	if err := provider.RegisterClient(cleanupClusterID, fake.NewSimpleClientset()); err != nil {
		t.Fatal(err)
	}
	if err := provider.CleanupEnvironment(context.Background(), cleanupClusterID, cleanupNamespace, cleanupProjectID, cleanupTargetID); err != nil {
		t.Fatalf("empty environment cleanup: %v", err)
	}
}

func cleanupObjects(targetID string, includeOtherTarget bool) []k8sruntime.Object {
	labelsFor := func(target string) map[string]string {
		return map[string]string{
			ProjectLabelKey:   cleanupProjectID,
			targetLabelKey:    target,
			managedByLabelKey: managedByLabelValue,
		}
	}
	ownedLabels := labelsFor(targetID)
	objects := []k8sruntime.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "owned", Namespace: cleanupNamespace, Labels: ownedLabels}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "owned-pod", Namespace: cleanupNamespace, Labels: ownedLabels}},
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "owned-rs", Namespace: cleanupNamespace, Labels: ownedLabels}},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "owned-hpa", Namespace: cleanupNamespace, Labels: ownedLabels}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "owned-ingress", Namespace: cleanupNamespace, Labels: ownedLabels}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "owned-service", Namespace: cleanupNamespace, Labels: ownedLabels}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "owned-config", Namespace: cleanupNamespace, Labels: ownedLabels}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "owned-secret", Namespace: cleanupNamespace, Labels: ownedLabels}},
	}
	if includeOtherTarget {
		otherLabels := labelsFor(otherTargetID)
		objects = append(objects,
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: cleanupNamespace, Labels: otherLabels}},
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "other-service", Namespace: cleanupNamespace, Labels: otherLabels}},
		)
	}
	return objects
}
