package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestDeployReleaseReconcilesPreviousResourceSet(t *testing.T) {
	oldLabels := releaseLabels("checkout", "target-dev", "release-old")
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "checkout-old", Namespace: "lab", Labels: oldLabels}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "checkout-old", Namespace: "lab", Labels: oldLabels}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "checkout-old", Namespace: "lab", Labels: oldLabels}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "checkout-old", Namespace: "lab", Labels: oldLabels}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "checkout-data", Namespace: "lab", Labels: oldLabels}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "checkout-cicd-config", Namespace: "lab", Labels: map[string]string{
			ProjectLabelKey: "checkout", targetLabelKey: "target-dev", managedByLabelKey: managedByLabelValue,
		}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "checkout-old-pod", Namespace: "lab", Labels: oldLabels}},
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "checkout-old-rs", Namespace: "lab", Labels: oldLabels}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "other-target", Namespace: "lab", Labels: releaseLabels("checkout", "target-test", "release-old")}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "other-project", Namespace: "lab", Labels: releaseLabels("payments", "target-dev", "release-old")}},
	)
	provider := NewKubernetesProvider(WithKubernetesRolloutWait(false))
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	var logs []string
	deployment := releaseDeploymentForReconcile("checkout-current")
	deployment.Log = func(_, _, _, line string) { logs = append(logs, line) }
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}

	assertKubernetesObjectMissing(t, func() error {
		_, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout-old", metav1.GetOptions{})
		return err
	})
	assertKubernetesObjectMissing(t, func() error {
		_, err := client.CoreV1().Services("lab").Get(context.Background(), "checkout-old", metav1.GetOptions{})
		return err
	})
	assertKubernetesObjectMissing(t, func() error {
		_, err := client.CoreV1().ConfigMaps("lab").Get(context.Background(), "checkout-old", metav1.GetOptions{})
		return err
	})
	assertKubernetesObjectMissing(t, func() error {
		_, err := client.CoreV1().Secrets("lab").Get(context.Background(), "checkout-old", metav1.GetOptions{})
		return err
	})

	for name, get := range map[string]func() error{
		"PVC": func() error {
			_, err := client.CoreV1().PersistentVolumeClaims("lab").Get(context.Background(), "checkout-data", metav1.GetOptions{})
			return err
		},
		"runtime ConfigMap": func() error {
			_, err := client.CoreV1().ConfigMaps("lab").Get(context.Background(), "checkout-cicd-config", metav1.GetOptions{})
			return err
		},
		"controller Pod": func() error {
			_, err := client.CoreV1().Pods("lab").Get(context.Background(), "checkout-old-pod", metav1.GetOptions{})
			return err
		},
		"controller ReplicaSet": func() error {
			_, err := client.AppsV1().ReplicaSets("lab").Get(context.Background(), "checkout-old-rs", metav1.GetOptions{})
			return err
		},
		"other target": func() error {
			_, err := client.AppsV1().Deployments("lab").Get(context.Background(), "other-target", metav1.GetOptions{})
			return err
		},
		"other project": func() error {
			_, err := client.AppsV1().Deployments("lab").Get(context.Background(), "other-project", metav1.GetOptions{})
			return err
		},
	} {
		if err := get(); err != nil {
			t.Fatalf("%s should be preserved: %v", name, err)
		}
	}
	if _, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout-current", metav1.GetOptions{}); err != nil {
		t.Fatalf("current Deployment should remain: %v", err)
	}
	joinedLogs := strings.Join(logs, "\n")
	if !strings.Contains(joinedLogs, "delete stale Deployment/checkout-old") || !strings.Contains(joinedLogs, "stale resources removed=4") {
		t.Fatalf("reconciliation logs missing stale resource details:\n%s", joinedLogs)
	}
}

func TestDeployReleaseKeepsSameResourceIdentityDuringIteration(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "checkout", Namespace: "lab", Labels: releaseLabels("checkout", "target-dev", "release-old"),
	}})
	provider := NewKubernetesProvider(WithKubernetesRolloutWait(false))
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	deployment := releaseDeploymentForReconcile("checkout")
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	list, err := client.AppsV1().Deployments("lab").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "checkout" {
		t.Fatalf("same resource iteration created another Deployment: %#v", list.Items)
	}
	if list.Items[0].Labels[releaseLabelKey] != deployment.ReleaseID {
		t.Fatalf("Deployment release label = %q, want %q", list.Items[0].Labels[releaseLabelKey], deployment.ReleaseID)
	}
}

func TestDeployReleaseFailurePreservesPreviousResourceSet(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "checkout-old", Namespace: "lab", Labels: releaseLabels("checkout", "target-dev", "release-old"),
	}})
	provider := NewKubernetesProvider(
		WithKubernetesRolloutTimeout(25*time.Millisecond),
		WithKubernetesRolloutPollInterval(time.Millisecond),
	)
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	err := provider.DeployRelease(context.Background(), releaseDeploymentForReconcile("checkout-current"))
	if !errors.Is(err, ErrDeploymentRolloutTimeout) {
		t.Fatalf("error = %v, want rollout timeout", err)
	}
	if _, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout-old", metav1.GetOptions{}); err != nil {
		t.Fatalf("previous Deployment was removed after failed rollout: %v", err)
	}
}

func TestDeployReleaseReturnsReconciliationDeleteFailure(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "checkout-old", Namespace: "lab", Labels: releaseLabels("checkout", "target-dev", "release-old"),
	}})
	client.PrependReactor("delete", "deployments", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		deleteAction, ok := action.(k8stesting.DeleteAction)
		if !ok || deleteAction.GetName() != "checkout-old" {
			return false, nil, nil
		}
		return true, nil, errors.New("delete denied")
	})
	provider := NewKubernetesProvider(WithKubernetesRolloutWait(false))
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	err := provider.DeployRelease(context.Background(), releaseDeploymentForReconcile("checkout-current"))
	if !errors.Is(err, ErrEnvironmentCleanupFailed) || !strings.Contains(err.Error(), "checkout-old") {
		t.Fatalf("error = %v, want reconciliation delete failure", err)
	}
}

func releaseDeploymentForReconcile(name string) ReleaseDeployment {
	return ReleaseDeployment{
		ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", TargetID: "target-dev", ReleaseID: "release-current",
		Branch: "feature/current", CommitSHA: "abcdef123456", Image: "registry.invalid/checkout:abcdef1234", Strategy: "rolling",
		ResourceFiles: []string{
			"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + name + "\nspec:\n  selector:\n    matchLabels:\n      app: " + name + "\n  template:\n    metadata:\n      labels:\n        app: " + name + "\n    spec:\n      containers:\n        - name: api\n          image: old.invalid/checkout:old\n",
			"apiVersion: v1\nkind: Service\nmetadata:\n  name: " + name + "\nspec:\n  selector:\n    app: " + name + "\n  ports:\n    - port: 8080\n      targetPort: 8080\n",
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name + "\ndata:\n  APP_ENV: dev\n",
		},
	}
}

func releaseLabels(projectID, targetID, releaseID string) map[string]string {
	return map[string]string{
		ProjectLabelKey:   projectID,
		targetLabelKey:    targetID,
		managedByLabelKey: managedByLabelValue,
		releaseLabelKey:   releaseID,
	}
}

func assertKubernetesObjectMissing(t *testing.T, get func() error) {
	t.Helper()
	if err := get(); !apierrors.IsNotFound(err) {
		t.Fatalf("object still exists or lookup failed: %v", err)
	}
}
