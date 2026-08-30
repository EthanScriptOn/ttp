package store

import (
	"context"
	"testing"
)

func TestMemoryCreateSpaceSeedsDemoCluster(t *testing.T) {
	data := NewMemory()
	space, err := data.CreateSpace(context.Background(), 1, CreateSpaceInput{Name: "新测试空间", Slug: "new-test-space"})
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	clusters, err := data.ListClusters(context.Background(), space.ID)
	if err != nil {
		t.Fatalf("list clusters: %v", err)
	}
	if len(clusters) != 1 || clusters[0].Name != "演示集群" || clusters[0].ID == defaultDemoClusterID {
		t.Fatalf("unexpected new-space clusters: %#v", clusters)
	}

	project, err := data.CreateProject(context.Background(), space.ID, CreateProjectInput{
		Name:          "新空间项目",
		RepositoryURL: "https://example.invalid/new-space-project",
		ClusterID:     defaultDemoClusterID,
	})
	if err != nil {
		t.Fatalf("create project with demo cluster alias: %v", err)
	}
	if project.ClusterID != clusters[0].ID {
		t.Fatalf("project cluster = %q, want %q", project.ClusterID, clusters[0].ID)
	}

	updated, err := data.UpdateProject(context.Background(), space.ID, project.ID, UpdateProjectInput{ClusterID: stringPointer(defaultDemoClusterID)})
	if err != nil {
		t.Fatalf("update project with demo cluster alias: %v", err)
	}
	if updated.ClusterID != clusters[0].ID {
		t.Fatalf("updated project cluster = %q, want %q", updated.ClusterID, clusters[0].ID)
	}

	labClusters, err := data.ListClusters(context.Background(), "space-lab")
	if err != nil {
		t.Fatalf("list lab clusters: %v", err)
	}
	if len(labClusters) != 2 {
		t.Fatalf("lab demo clusters = %d, want 2: %#v", len(labClusters), labClusters)
	}
	seen := make(map[string]bool, len(labClusters))
	for _, cluster := range labClusters {
		seen[cluster.ID] = true
	}
	if !seen[defaultDemoClusterID] || !seen["demo-cluster-uat"] {
		t.Fatalf("lab demo clusters changed: %#v", labClusters)
	}
}

func TestMemorySeedsFourReleaseEnvironments(t *testing.T) {
	data := NewMemory()
	targets, err := data.ListDeploymentTargets(context.Background(), "space-lab", "reverse-lab")
	if err != nil {
		t.Fatalf("list seeded deployment targets: %v", err)
	}
	if len(targets) != 4 {
		t.Fatalf("seeded deployment targets = %d, want 4: %#v", len(targets), targets)
	}

	want := map[string]struct {
		clusterID string
		namespace string
	}{
		"dev":  {clusterID: "demo-cluster", namespace: "lab"},
		"uat":  {clusterID: "demo-cluster-uat", namespace: "uat"},
		"pre":  {clusterID: "demo-cluster-uat", namespace: "pre"},
		"prod": {clusterID: "demo-cluster", namespace: "prod"},
	}
	for _, target := range targets {
		expected, ok := want[target.Environment]
		if !ok {
			t.Fatalf("unexpected seeded environment %q", target.Environment)
		}
		if target.ClusterID != expected.clusterID || target.Namespace != expected.namespace {
			t.Fatalf("%s target = %s/%s, want %s/%s", target.Environment, target.ClusterID, target.Namespace, expected.clusterID, expected.namespace)
		}
	}
}

func TestMemoryDeploymentTargetsUseExplicitStageAndOrder(t *testing.T) {
	data := NewMemory()
	project, err := data.CreateProject(context.Background(), "space-lab", CreateProjectInput{
		Name:          "阶段顺序项目",
		RepositoryURL: "https://example.invalid/stage-order",
		ClusterID:     defaultDemoClusterID,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	initial, err := data.ListDeploymentTargets(context.Background(), "space-lab", project.ID)
	if err != nil || len(initial) != 1 || initial[0].Stage != DeploymentStageDev || initial[0].SortOrder != 1 {
		t.Fatalf("new project did not start with DEV #1: targets=%#v err=%v", initial, err)
	}

	if _, err := data.CreateDeploymentTarget(context.Background(), "space-lab", project.ID, CreateDeploymentTargetInput{
		Name: "发布候选环境", Environment: "release", Stage: DeploymentStagePre, SortOrder: 3,
		ClusterID: defaultDemoClusterID, Namespace: "release",
	}); err != nil {
		t.Fatalf("create release-named PRE target: %v", err)
	}
	if _, err := data.CreateDeploymentTarget(context.Background(), "space-lab", project.ID, CreateDeploymentTargetInput{
		Name: "验收环境", Environment: "uat", Stage: DeploymentStageUAT, SortOrder: 2,
		ClusterID: defaultDemoClusterID, Namespace: "uat",
	}); err != nil {
		t.Fatalf("create UAT target: %v", err)
	}

	targets, err := data.ListDeploymentTargets(context.Background(), "space-lab", project.ID)
	if err != nil {
		t.Fatalf("list deployment targets: %v", err)
	}
	if len(targets) != 3 || targets[0].Stage != DeploymentStageDev || targets[0].SortOrder != 1 || targets[1].Stage != DeploymentStageUAT || targets[1].SortOrder != 2 || targets[2].Environment != "release" || targets[2].Stage != DeploymentStagePre || targets[2].SortOrder != 3 {
		t.Fatalf("targets were not ordered by explicit stage/order: %#v", targets)
	}

	if _, err := data.CreateDeploymentTarget(context.Background(), "space-lab", project.ID, CreateDeploymentTargetInput{
		Name: "错误顺序", Environment: "uat-2", Stage: DeploymentStageUAT, SortOrder: 1,
		ClusterID: defaultDemoClusterID, Namespace: "uat-2",
	}); err == nil {
		t.Fatal("non-DEV target was allowed to use sort order 1")
	}
	if err := data.DeleteDeploymentTarget(context.Background(), "space-lab", project.ID, targets[0].ID); err == nil {
		t.Fatal("the only DEV target was allowed to be deleted")
	}

	first, err := data.GetDeploymentTarget(context.Background(), "space-lab", project.ID, "")
	if err != nil || first.ID != targets[0].ID {
		t.Fatalf("implicit target lookup did not choose DEV #1: target=%#v err=%v", first, err)
	}
}

func stringPointer(value string) *string { return &value }
