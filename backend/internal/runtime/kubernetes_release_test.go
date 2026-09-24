package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
  replicas: 2
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
	deployment.Manifest = strings.Replace(deployment.Manifest, "replicas: 2", "replicas: 3", 1)
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	updated, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Replicas == nil || *updated.Spec.Replicas != 3 || updated.Spec.Template.Spec.Containers[0].Image != deployment.Image {
		t.Fatalf("existing deployment was not updated: %#v", updated.Spec)
	}
}

func TestKubernetesProviderDeployReleaseAppliesIndependentResourceFiles(t *testing.T) {
	client := fake.NewSimpleClientset()
	markDeploymentsReadyOnGet(client)
	provider := NewKubernetesProvider()
	if err := provider.RegisterClient("cluster-files", client); err != nil {
		t.Fatal(err)
	}
	deployment := ReleaseDeployment{
		ClusterID: "cluster-files", Namespace: "lab", ProjectID: "checkout", ReleaseID: "release-files",
		CommitSHA: "abcdef123456", Image: "registry.invalid/checkout:abcdef1234", Replicas: 1, Strategy: "rolling",
		ResourceFiles: []string{
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: checkout-settings\ndata:\n  APP_ENV: dev\n",
			"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: checkout\nspec:\n  selector:\n    matchLabels:\n      app: checkout\n  template:\n    metadata:\n      labels:\n        app: checkout\n    spec:\n      containers:\n        - name: api\n          image: old.invalid/checkout:old\n",
		},
	}
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().ConfigMaps("lab").Get(context.Background(), "checkout-settings", metav1.GetOptions{}); err != nil {
		t.Fatalf("config map from resource file was not applied: %v", err)
	}
	if _, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout", metav1.GetOptions{}); err != nil {
		t.Fatalf("deployment from resource file was not applied: %v", err)
	}
}

func TestKubernetesProviderDeployReleaseManagesPersistentVolumeClaim(t *testing.T) {
	client := fake.NewSimpleClientset()
	provider := NewKubernetesProvider(WithKubernetesRolloutWait(false))
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: checkout-data
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 2Gi
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
          volumeMounts:
            - name: data
              mountPath: /app/data
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: checkout-data
`
	deployment := ReleaseDeployment{
		ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", TargetID: "target-dev", ReleaseID: "release-pvc-1",
		CommitSHA: "abcdef123456", Image: "registry.invalid/checkout:abcdef1234", Replicas: 1,
		Strategy: "rolling", Manifest: manifest, ManifestFormat: "yaml",
	}
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	claim, err := client.CoreV1().PersistentVolumeClaims("lab").Get(context.Background(), "checkout-data", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("PVC was not created: %v", err)
	}
	storage := claim.Spec.Resources.Requests[corev1.ResourceStorage]
	if got := storage.String(); got != "2Gi" {
		t.Fatalf("PVC storage = %q, want 2Gi", got)
	}
	if claim.Labels[ProjectLabelKey] != deployment.ProjectID || claim.Labels[targetLabelKey] != deployment.TargetID || claim.Labels[managedByLabelKey] != managedByLabelValue {
		t.Fatalf("PVC ownership labels = %#v", claim.Labels)
	}

	deployment.ReleaseID = "release-pvc-2"
	deployment.CommitSHA = "fedcba654321"
	deployment.Image = "registry.invalid/checkout:fedcba6543"
	deployment.Manifest = strings.Replace(manifest, "storage: 2Gi", "storage: 4Gi", 1)
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	resized, err := client.CoreV1().PersistentVolumeClaims("lab").Get(context.Background(), "checkout-data", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("resized PVC was not readable: %v", err)
	}
	resizedStorage := resized.Spec.Resources.Requests[corev1.ResourceStorage]
	if got := resizedStorage.String(); got != "4Gi" {
		t.Fatalf("resized PVC storage = %q, want 4Gi", got)
	}

	deployment.ReleaseID = "release-pvc-3"
	deployment.Manifest = strings.Replace(manifest, "storage: 2Gi", "storage: 1Gi", 1)
	if err := provider.DeployRelease(context.Background(), deployment); !errors.Is(err, ErrInvalidKubernetesInput) || !strings.Contains(err.Error(), "cannot be reduced") {
		t.Fatalf("PVC shrink error = %v, want a guarded invalid input error", err)
	}
}

func TestKubernetesProviderDeployReleaseAppliesAdditionalWorkloads(t *testing.T) {
	client := fake.NewSimpleClientset()
	provider := NewKubernetesProvider(WithKubernetesRolloutWait(false))
	if err := provider.RegisterClient("cluster-a", client); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: checkout-stateful
spec:
  serviceName: checkout
  replicas: 3
  selector:
    matchLabels:
      app: checkout-stateful
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 2Gi
  template:
    metadata:
      labels:
        app: checkout-stateful
    spec:
      containers:
        - name: api
          image: old.invalid/checkout:old
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: checkout-agent
spec:
  selector:
    matchLabels:
      app: checkout-agent
  template:
    metadata:
      labels:
        app: checkout-agent
    spec:
      containers:
        - name: agent
          image: old.invalid/checkout:old
---
apiVersion: batch/v1
kind: Job
metadata:
  name: checkout-migrate
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: migrate
          image: old.invalid/checkout:old
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: checkout-cleanup
spec:
  schedule: "*/5 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: cleanup
              image: old.invalid/checkout:old
`
	deployment := ReleaseDeployment{
		ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", TargetID: "target-dev", ReleaseID: "release-workloads",
		CommitSHA: "abcdef123456", Image: "registry.invalid/checkout:abcdef1234", Replicas: 2,
		Strategy: "rolling", Manifest: manifest, ManifestFormat: "yaml",
	}
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	statefulSet, err := client.AppsV1().StatefulSets("lab").Get(context.Background(), "checkout-stateful", metav1.GetOptions{})
	if err != nil || statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 3 {
		t.Fatalf("stateful set did not keep manifest replicas: %v, %#v", err, statefulSet)
	}
	if statefulSet.Spec.VolumeClaimTemplates[0].Labels[targetLabelKey] != deployment.TargetID {
		t.Fatalf("stateful set PVC template target label = %q", statefulSet.Spec.VolumeClaimTemplates[0].Labels[targetLabelKey])
	}
	daemonSet, err := client.AppsV1().DaemonSets("lab").Get(context.Background(), "checkout-agent", metav1.GetOptions{})
	if err != nil || daemonSet.Spec.Template.Spec.Containers[0].Image != deployment.Image {
		t.Fatalf("daemon set image = %q, err=%v", daemonSet.Spec.Template.Spec.Containers[0].Image, err)
	}
	job, err := client.BatchV1().Jobs("lab").Get(context.Background(), "checkout-migrate", metav1.GetOptions{})
	if err != nil || job.Spec.Template.Spec.Containers[0].Image != deployment.Image {
		t.Fatalf("job image = %q, err=%v", job.Spec.Template.Spec.Containers[0].Image, err)
	}
	cronJob, err := client.BatchV1().CronJobs("lab").Get(context.Background(), "checkout-cleanup", metav1.GetOptions{})
	if err != nil || cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image != deployment.Image {
		t.Fatalf("cron job image = %q, err=%v", cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image, err)
	}
	if cronJob.Spec.JobTemplate.Labels[targetLabelKey] != deployment.TargetID {
		t.Fatalf("cron job template target label = %q", cronJob.Spec.JobTemplate.Labels[targetLabelKey])
	}
}

func TestKubernetesProviderDeployReleaseInjectsImagePullSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	provider := NewKubernetesProvider(WithKubernetesRolloutWait(false))
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
      imagePullSecrets:
        - name: existing-secret
      containers:
        - name: api
          image: old.invalid/checkout:old
`
	credential := &ImagePullCredential{ConnectionID: "acr-main", Registry: "registry.example.com", AuthType: "basic", Username: "robot", Secret: "password", SecretName: "ttp-registry-acr-main"}
	if err := provider.DeployRelease(context.Background(), ReleaseDeployment{ClusterID: "cluster-a", Namespace: "lab", ProjectID: "checkout", TargetID: "target-dev", ReleaseID: "release-1", CommitSHA: "abcdef123456", Image: "registry.example.com/team/checkout@sha256:" + strings.Repeat("a", 64), Replicas: 1, Strategy: "rolling", Manifest: manifest, ManifestFormat: "yaml", ImagePullCredential: credential}); err != nil {
		t.Fatal(err)
	}
	secret, err := client.CoreV1().Secrets("lab").Get(context.Background(), credential.SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]map[string]map[string]string
	if err := json.Unmarshal(secret.Data[corev1.DockerConfigJsonKey], &config); err != nil {
		t.Fatal(err)
	}
	if config["auths"][credential.Registry]["auth"] != base64.StdEncoding.EncodeToString([]byte("robot:password")) {
		t.Fatalf("unexpected docker auth config: %#v", config)
	}
	if secret.Labels[targetLabelKey] != "target-dev" {
		t.Fatalf("image pull Secret target label = %q, want target-dev", secret.Labels[targetLabelKey])
	}
	deployed, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, reference := range deployed.Spec.Template.Spec.ImagePullSecrets {
		seen[reference.Name] = true
	}
	if !seen[credential.SecretName] || !seen["existing-secret"] {
		t.Fatalf("imagePullSecrets = %#v", deployed.Spec.Template.Spec.ImagePullSecrets)
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

func TestKubernetesProviderAllowsManifestOwnedReplicas(t *testing.T) {
	client := fake.NewSimpleClientset()
	provider := NewKubernetesProvider(WithKubernetesRolloutWait(false))
	if err := provider.RegisterClient("cluster-a", client); err != nil {
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
	if err != nil {
		t.Fatalf("release should not require target replicas: %v", err)
	}
	deployment, err := client.AppsV1().Deployments("lab").Get(context.Background(), "checkout", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Replicas != nil {
		t.Fatalf("replicas should be left to the manifest/defaulting, got %d", *deployment.Spec.Replicas)
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
