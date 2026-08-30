package runtime

import (
	"context"
	"errors"
	"testing"
)

var demoAPI = PodRef{ClusterID: "demo-cluster", Namespace: "lab", Name: "reverse-lab-api-7d9f8c6d4b-x2k9m"}

func TestDemoProviderRuntimeOperations(t *testing.T) {
	provider := NewDemoProvider()
	pods, err := provider.ListPods(context.Background(), "demo-cluster", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 2 || !pods[0].Ready || pods[0].RestartCount != 0 || pods[1].RestartCount != 1 {
		t.Fatalf("unexpected pod list: %#v", pods)
	}
	detail, err := provider.GetPod(context.Background(), demoAPI)
	if err != nil {
		t.Fatal(err)
	}
	wrongProject := demoAPI
	wrongProject.ProjectID = "another-project"
	if _, err := provider.GetPod(context.Background(), wrongProject); !errors.Is(err, ErrPodNotFound) {
		t.Fatalf("cross-project demo pod lookup error = %v", err)
	}
	container, ok := detail.Containers["api"]
	if !ok || container.Image != "example.invalid/reverse-lab:v1" || !container.Ready {
		t.Fatalf("unexpected container detail: %#v", detail.Containers)
	}

	logs, err := provider.GetPodLogs(context.Background(), PodLogRequest{PodRef: demoAPI, Container: "api", TailLines: 1})
	if err != nil {
		t.Fatal(err)
	}
	if logs != "2026-01-12T08:30:04Z runtime provider ready\n" {
		t.Fatalf("unexpected tail logs: %q", logs)
	}

	updated, err := provider.UpdatePodConfig(context.Background(), demoAPI, PodConfigUpdate{Config: map[string]string{"LOG_LEVEL": "debug"}, Environment: map[string]string{"TRACE": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Config["LOG_LEVEL"] != "debug" || updated.Environment["TRACE"] != "1" {
		t.Fatalf("update was not applied: %#v", updated)
	}
	updated.Config["LOG_LEVEL"] = "caller-mutated"
	fresh, err := provider.GetPod(context.Background(), demoAPI)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Config["LOG_LEVEL"] != "debug" {
		t.Fatal("provider leaked its internal config map")
	}

	cluster, err := provider.GetClusterMetrics(context.Background(), "demo-cluster")
	if err != nil || cluster.PodCount != 2 || cluster.HealthyPodCount != 2 {
		t.Fatalf("unexpected cluster metrics: %#v, %v", cluster, err)
	}
	if len(cluster.Nodes) != 3 || cluster.Nodes[0].SwapUsedPercent <= 0 {
		t.Fatalf("unexpected cluster node metrics: %#v", cluster.Nodes)
	}
	if len(cluster.Series) != 289 || cluster.Series[0].Timestamp.IsZero() || cluster.Series[len(cluster.Series)-1].SwapUsedPercent <= 0 {
		t.Fatalf("unexpected cluster metric series: points=%d, last=%#v", len(cluster.Series), cluster.Series[len(cluster.Series)-1])
	}
	project, err := provider.GetProjectMetrics(context.Background(), "demo-cluster", "reverse-lab")
	if err != nil || project.PodCount != 2 {
		t.Fatalf("unexpected project metrics: %#v, %v", project, err)
	}
	if len(project.Nodes) != 2 || project.Nodes[0].SwapUsedPercent <= 0 {
		t.Fatalf("unexpected project node metrics: %#v", project.Nodes)
	}
}

func TestDemoProviderNotFound(t *testing.T) {
	_, err := NewDemoProvider().GetPod(context.Background(), PodRef{ClusterID: "missing", Namespace: "lab", Name: "api"})
	if !errors.Is(err, ErrClusterNotFound) {
		t.Fatalf("expected cluster not found, got %v", err)
	}
	_, err = NewDemoProvider().GetProjectMetrics(context.Background(), "demo-cluster", "missing")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected project not found, got %v", err)
	}
}

func TestDemoProviderDeployReleaseUpdatesPods(t *testing.T) {
	provider := NewDemoProvider()
	deployment := ReleaseDeployment{
		ClusterID:     "demo-cluster",
		Namespace:     "lab",
		ProjectID:     "reverse-lab",
		ReleaseID:     "rel-demo-001",
		Branch:        "main",
		CommitSHA:     "a1b2c3d4e5f6",
		Image:         "example.invalid/reverse-lab:a1b2c3d4e5",
		Replicas:      3,
		Strategy:      "rolling",
		StablePercent: 100,
	}
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}

	pods, err := provider.ListPodsInNamespace(context.Background(), "demo-cluster", "lab", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 3 {
		t.Fatalf("pod count after deployment = %d, want 3: %#v", len(pods), pods)
	}
	for _, pod := range pods {
		if pod.Labels["release"] != deployment.ReleaseID || pod.Labels["version"] != "a1b2c3d4e5" || pod.Labels["branch"] != deployment.Branch {
			t.Fatalf("release labels were not applied to %s: %#v", pod.Name, pod.Labels)
		}
		detail, getErr := provider.GetPod(context.Background(), pod.PodRef)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if detail.Environment["CICD_COMMIT_SHA"] != deployment.CommitSHA || detail.Environment["CICD_RELEASE_ID"] != deployment.ReleaseID {
			t.Fatalf("release environment was not applied to %s: %#v", pod.Name, detail.Environment)
		}
		for _, container := range detail.Containers {
			if container.Image != deployment.Image || !container.Ready {
				t.Fatalf("release image was not applied to %s: %#v", pod.Name, detail.Containers)
			}
		}
	}

	deployment.Replicas = 1
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	pods, err = provider.ListPodsInNamespace(context.Background(), "demo-cluster", "lab", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 {
		t.Fatalf("pod count after scale down = %d, want 1", len(pods))
	}
}

func TestDemoProviderDeployReleaseValidatesInput(t *testing.T) {
	provider := NewDemoProvider()
	err := provider.DeployRelease(context.Background(), ReleaseDeployment{ClusterID: "demo-cluster"})
	if !errors.Is(err, ErrInvalidRuntimeInput) {
		t.Fatalf("expected invalid runtime input, got %v", err)
	}
}

func TestDemoProviderUATTargetHasIndependentRuntimeState(t *testing.T) {
	provider := NewDemoProvider()

	pods, err := provider.ListPodsInNamespace(context.Background(), "demo-cluster-uat", "uat", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 2 {
		t.Fatalf("uat pod count = %d, want 2: %#v", len(pods), pods)
	}
	for _, pod := range pods {
		if pod.ClusterID != "demo-cluster-uat" || pod.Namespace != "uat" || pod.NodeName == "" {
			t.Fatalf("uat pod has wrong target context: %#v", pod)
		}
	}

	logs, err := provider.GetPodLogs(context.Background(), PodLogRequest{PodRef: PodRef{
		ClusterID: "demo-cluster-uat", Namespace: "uat", Name: "reverse-lab-api-uat-6f8d7c5b4a-p3m7n",
	}, Container: "api", TailLines: 1})
	if err != nil {
		t.Fatal(err)
	}
	if logs != "2026-01-11T08:30:04Z uat runtime provider ready\n" {
		t.Fatalf("unexpected uat logs: %q", logs)
	}

	metrics, err := provider.GetProjectMetrics(context.Background(), "demo-cluster-uat", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	if metrics.PodCount != 2 || metrics.HealthyPodCount != 2 || metrics.CPUUsedPercent == 18.7 {
		t.Fatalf("uat metrics did not stay independent: %#v", metrics)
	}
	if len(metrics.Nodes) != 2 || metrics.Nodes[0].Name[:4] != "uat-" {
		t.Fatalf("unexpected uat nodes: %#v", metrics.Nodes)
	}
}

func TestDemoProviderCanDeploySameCommitToUATWithoutChangingDevelopment(t *testing.T) {
	provider := NewDemoProvider()
	deployment := ReleaseDeployment{
		ClusterID:     "demo-cluster-uat",
		Namespace:     "uat",
		ProjectID:     "reverse-lab",
		ReleaseID:     "rel-uat-001",
		Branch:        "main",
		CommitSHA:     "a1b2c3d4e5f6",
		Image:         "example.invalid/reverse-lab:a1b2c3d4e5",
		Replicas:      1,
		Strategy:      "rolling",
		StablePercent: 100,
	}
	if err := provider.DeployRelease(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}

	devPods, err := provider.ListPodsInNamespace(context.Background(), "demo-cluster", "lab", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(devPods) != 2 {
		t.Fatalf("development pods changed after uat deployment: %#v", devPods)
	}
	uatPods, err := provider.ListPodsInNamespace(context.Background(), "demo-cluster-uat", "uat", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(uatPods) != 1 || uatPods[0].Labels["release"] != deployment.ReleaseID || uatPods[0].Labels["version"] != "a1b2c3d4e5" {
		t.Fatalf("uat deployment was not isolated: %#v", uatPods)
	}
}
