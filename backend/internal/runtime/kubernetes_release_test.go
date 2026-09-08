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

func TestKubernetesProviderDeployReleaseAppliesManifestAndUpdatesWorkload(t *testing.T) {
	client := fake.NewSimpleClientset()
	markDeploymentsReadyOnGet(client)
	provider := NewKubernetesProvider()
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	var _ ReleaseDeployer = provider

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: checkout-settings
data:
  APP_ENV: dev
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
spec:
  selector:
    matchLabels:
      app: checkout
  template:
    metadata:
      labels:
        app: checkout
    spec:
      containers:
        - name: api
          image: old.invalid/checkout:old
---
apiVersion: v1
kind: Service
metadata:
  name: checkout
spec:
  selector:
    app: checkout
  ports:
    - port: 8080
      targetPort: 8080
`
	deployment := ReleaseDeployment{
		ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", ReleaseID: "release-1",
		Branch: "feature/test", CommitSHA: "abcdef123456", Image: "registry.invalid/checkout:abcdef1234",
		Replicas: 2, Strategy: "rolling", StablePercent: 100, Manifest: manifest, ManifestFormat: "yaml",
	}
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}

	created, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if created.Spec.Replicas == nil || *created.Spec.Replicas != 2 {
		t.Fatalf("replicas = %v, want 2", created.Spec.Replicas)
	}
	container := created.Spec.Template.Spec.Containers[0]
	if container.Image != deployment.Image || envValue(container.Env, "CICD_COMMIT_SHA") != deployment.CommitSHA {
		t.Fatalf("release image/environment not applied: %#v", container)
	}
	if created.Spec.Template.Labels[ProjectLabelKey] != deployment.ProjectID || created.Spec.Template.Labels[releaseLabelKey] != deployment.ReleaseID {
		t.Fatalf("release labels not applied: %#v", created.Spec.Template.Labels)
	}
	if created.Spec.Template.Annotations[releaseBranchAnnotKey] != deployment.Branch {
		t.Fatalf("branch annotation = %q", created.Spec.Template.Annotations[releaseBranchAnnotKey])
	}
	if _, err := client.CoreV1().Services("lab").Get(context.Background(), "checkout", metav1.GetOptions{}); err != nil {
		t.Fatalf("service was not applied: %v", err)
	}
	if _, err := client.CoreV1().ConfigMaps("lab").Get(context.Background(), "checkout-settings", metav1.GetOptions{}); err != nil {
		t.Fatalf("config map was not applied: %v", err)
	}

	deployment.ReleaseID = "release-2"
	deployment.CommitSHA = "fedcba654321"
	deployment.Image = "registry.invalid/checkout:fedcba6543"
	deployment.Replicas = 1
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	updated, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Replicas == nil || *updated.Spec.Replicas != 1 || updated.Spec.Template.Spec.Containers[0].Image != deployment.Image {
		t.Fatalf("existing deployment was not updated: %#v", updated.Spec)
	}
}

func TestKubernetesProviderDeployReleaseAppliesCommonApplicationResources(t *testing.T) {
	client := fake.NewSimpleClientset()
	markDeploymentsReadyOnGet(client)
	provider := NewKubernetesProvider()
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}

	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
spec:
  selector:
    matchLabels:
      app: checkout
  template:
    metadata:
      labels:
        app: checkout
    spec:
      containers:
        - name: api
          image: old.invalid/checkout:old
---
apiVersion: v1
kind: Secret
metadata:
  name: checkout-secrets
stringData:
  DATABASE_URL: mysql://example
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: checkout
spec:
  rules:
    - host: checkout.example.test
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: checkout
                port:
                  number: 8080
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: checkout
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: checkout
  minReplicas: 1
  maxReplicas: 4
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
`
	deployment := ReleaseDeployment{
		ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", ReleaseID: "release-common",
		Branch: "main", CommitSHA: "abcdef123456", Image: "registry.invalid/checkout:abcdef1234", Replicas: 2,
		Strategy: "rolling", StablePercent: 100, Manifest: manifest, ManifestFormat: "yaml",
	}
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}

	secret, err := client.CoreV1().Secrets("lab").Get(context.Background(), "checkout-secrets", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("secret was not applied: %v", err)
	}
	if secret.StringData["DATABASE_URL"] != "mysql://example" && string(secret.Data["DATABASE_URL"]) != "mysql://example" {
		t.Fatalf("secret value was not preserved: %#v", secret)
	}
	ingress, err := client.NetworkingV1().Ingresses("lab").Get(context.Background(), "checkout", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ingress was not applied: %v", err)
	}
	if len(ingress.Spec.Rules) != 1 || ingress.Spec.Rules[0].Host != "checkout.example.test" {
		t.Fatalf("unexpected ingress: %#v", ingress.Spec.Rules)
	}
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers("lab").Get(context.Background(), "checkout", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("HPA was not applied: %v", err)
	}
	if hpa.Spec.MaxReplicas != 4 || hpa.Spec.ScaleTargetRef.Name != "checkout" {
		t.Fatalf("unexpected HPA: %#v", hpa.Spec)
	}

	deployment.ReleaseID = "release-common-2"
	deployment.CommitSHA = "fedcba654321"
	deployment.Image = "registry.invalid/checkout:fedcba6543"
	deployment.Replicas = 1
	deployment.Manifest = strings.Replace(deployment.Manifest, "mysql://example", "mysql://updated", 1)
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	updatedSecret, err := client.CoreV1().Secrets("lab").Get(context.Background(), "checkout-secrets", metav1.GetOptions{})
	if err != nil || updatedSecret.StringData["DATABASE_URL"] != "mysql://updated" {
		t.Fatalf("secret was not updated: %v, %#v", err, updatedSecret)
	}
}

func TestKubernetesProviderDeployReleaseTimesOutWhenDeploymentDoesNotBecomeReady(t *testing.T) {
	client := fake.NewSimpleClientset()
	provider := NewKubernetesProvider(
		WithKubernetesRolloutTimeout(25*time.Millisecond),
		WithKubernetesRolloutPollInterval(time.Millisecond),
	)
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
spec:
  selector:
    matchLabels:
      app: checkout
  template:
    metadata:
      labels:
        app: checkout
    spec:
      containers:
        - name: api
          image: old.invalid/checkout:old
`
	err := provider.DeployRelease(context.Background(), ReleaseDeployment{
		ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", ReleaseID: "release-timeout",
		CommitSHA: "abcdef123456", Image: "registry.invalid/checkout:abcdef1234", Replicas: 1,
		Strategy: "rolling", Manifest: manifest, ManifestFormat: "yaml",
	})
	if !errors.Is(err, ErrDeploymentRolloutTimeout) {
		t.Fatalf("error = %v, want rollout timeout", err)
	}
	if !strings.Contains(err.Error(), "checkout") || !strings.Contains(err.Error(), "副本") {
		t.Fatalf("timeout error omitted rollout details: %v", err)
	}
}

func TestKubernetesProviderDeployReleaseStopsOnDeploymentFailure(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		getAction, ok := action.(k8stesting.GetAction)
		if !ok {
			return false, nil, nil
		}
		object, err := client.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), action.GetNamespace(), getAction.GetName())
		if err != nil {
			return false, nil, nil
		}
		deployment := object.(*appsv1.Deployment).DeepCopy()
		deployment.Status.Conditions = []appsv1.DeploymentCondition{{
			Type:    appsv1.DeploymentProgressing,
			Status:  corev1.ConditionFalse,
			Reason:  "ProgressDeadlineExceeded",
			Message: "新版本 Pod 一直没有准备好",
		}}
		return true, deployment, nil
	})
	provider := NewKubernetesProvider(
		WithKubernetesRolloutTimeout(time.Second),
		WithKubernetesRolloutPollInterval(time.Millisecond),
	)
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	err := provider.DeployRelease(context.Background(), ReleaseDeployment{
		ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", ReleaseID: "release-failed",
		CommitSHA: "abcdef123456", Image: "registry.invalid/checkout:abcdef1234", Replicas: 1,
		Strategy: "rolling", Manifest: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
spec:
  selector:
    matchLabels:
      app: checkout
  template:
    metadata:
      labels:
        app: checkout
    spec:
      containers:
        - name: api
          image: old.invalid/checkout:old
`, ManifestFormat: "yaml",
	})
	if !errors.Is(err, ErrDeploymentRolloutFailed) {
		t.Fatalf("error = %v, want rollout failure", err)
	}
	if !strings.Contains(err.Error(), "ProgressDeadlineExceeded") || !strings.Contains(err.Error(), "新版本 Pod") {
		t.Fatalf("failure condition was not preserved: %v", err)
	}
}

func TestKubernetesProviderDeployReleaseRejectsUnsupportedBoundaryBeforeWrites(t *testing.T) {
	client := fake.NewSimpleClientset()
	provider := NewKubernetesProvider()
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	base := ReleaseDeployment{
		ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", ReleaseID: "release-1",
		CommitSHA: "abcdef123456", Image: "registry.invalid/checkout:abcdef1234", Replicas: 1,
		Strategy: "rolling", Manifest: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
spec:
  selector:
    matchLabels:
      app: checkout
  template:
    metadata:
      labels:
        app: checkout
    spec:
      containers:
        - name: api
          image: old.invalid/checkout:old
`, ManifestFormat: "yaml",
	}

	for name, mutate := range map[string]func(*ReleaseDeployment){
		"canary strategy": func(item *ReleaseDeployment) { item.Strategy = "canary" },
		"cluster scoped kind": func(item *ReleaseDeployment) {
			item.Manifest = `apiVersion: v1
kind: Namespace
metadata:
  name: checkout
`
		},
		"wrong namespace": func(item *ReleaseDeployment) { item.Manifest = stringsReplaceNamespace(item.Manifest, "other") },
	} {
		t.Run(name, func(t *testing.T) {
			item := base
			mutate(&item)
			if err := provider.DeployRelease(context.Background(), item); err == nil {
				t.Fatal("expected release boundary error")
			}
			if _, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("boundary failure wrote a deployment: %v", err)
			}
		})
	}
}

func TestKubernetesProviderRejectsZeroReplicas(t *testing.T) {
	provider := NewKubernetesProvider()
	if err := provider.RegisterClient("cluster-a", fake.NewSimpleClientset()); err != nil {
		t.Fatal(err)
	}
	err := provider.DeployRelease(context.Background(), ReleaseDeployment{
		ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", ReleaseID: "release-zero",
		Branch: "main", CommitSHA: "abcdef123456", Image: "registry.example/checkout:abcdef1234",
		Replicas: 0, Strategy: "rolling", StablePercent: 100,
		Manifest: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
spec:
  selector:
    matchLabels:
      app: checkout
  template:
    metadata:
      labels:
        app: checkout
    spec:
      containers:
        - name: api
          image: old.invalid/checkout:old
`, ManifestFormat: "yaml",
	})
	if !errors.Is(err, ErrInvalidKubernetesInput) {
		t.Fatalf("zero replicas error = %v, want ErrInvalidKubernetesInput", err)
	}
}

func stringsReplaceNamespace(manifest, namespace string) string {
	return manifest + "\n---\napiVersion: v1\nkind: Service\nmetadata:\n  name: wrong\n  namespace: " + namespace + "\n"
}

func markDeploymentsReadyOnGet(client *fake.Clientset) {
	client.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		getAction, ok := action.(k8stesting.GetAction)
		if !ok {
			return false, nil, nil
		}
		object, err := client.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), action.GetNamespace(), getAction.GetName())
		if err != nil {
			return false, nil, nil
		}
		deployment := object.(*appsv1.Deployment).DeepCopy()
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		if deployment.Generation == 0 {
			deployment.Generation = 1
		}
		deployment.Status.ObservedGeneration = deployment.Generation
		deployment.Status.Replicas = desired
		deployment.Status.UpdatedReplicas = desired
		deployment.Status.ReadyReplicas = desired
		deployment.Status.AvailableReplicas = desired
		return true, deployment, nil
	})
}
