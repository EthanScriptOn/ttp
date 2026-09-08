package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

func TestMemoryCreateSpaceDoesNotCreateCluster(t *testing.T) {
	data := NewMemoryWithFixtures()
	space, err := data.CreateSpace(context.Background(), 1, CreateSpaceInput{Name: "新测试空间", Slug: "new-test-space"})
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	clusters, err := data.ListClusters(context.Background(), space.ID)
	if err != nil {
		t.Fatalf("list clusters: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("unexpected new-space clusters: %#v", clusters)
	}

	_, err = data.CreateProject(context.Background(), space.ID, CreateProjectInput{
		Name:          "新空间项目",
		RepositoryURL: "https://example.invalid/new-space-project",
		ClusterID:     "",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("create project without an explicit cluster: %v", err)
	}

	labClusters, err := data.ListClusters(context.Background(), "space-lab")
	if err != nil {
		t.Fatalf("list lab clusters: %v", err)
	}
	if len(labClusters) != 2 {
		t.Fatalf("fixture clusters = %d, want 2: %#v", len(labClusters), labClusters)
	}
	seen := make(map[string]bool, len(labClusters))
	for _, cluster := range labClusters {
		seen[cluster.ID] = true
	}
	if !seen["demo-cluster"] || !seen["demo-cluster-uat"] {
		t.Fatalf("fixture clusters changed: %#v", labClusters)
	}
}

func TestNewMemoryStartsEmpty(t *testing.T) {
	data := NewMemory()
	if _, err := data.ListSpaces(context.Background(), 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty memory store ListSpaces error = %v, want ErrNotFound", err)
	}
}

func TestMemoryCreateProjectRequiresAnExplicitCluster(t *testing.T) {
	data := NewMemoryWithFixtures()
	_, err := data.CreateProject(context.Background(), "space-lab", CreateProjectInput{
		Name:          "自动选择集群的项目",
		RepositoryURL: "https://example.invalid/auto-cluster",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("create project without an explicit cluster: %v", err)
	}
}

func TestMemoryProjectSettingsRejectInvalidValues(t *testing.T) {
	base := CreateProjectInput{
		Name:          "边界项目",
		RepositoryURL: "https://example.invalid/boundary",
		ClusterID:     "demo-cluster",
	}
	cases := []struct {
		name  string
		apply func(*CreateProjectInput)
	}{
		{name: "invalid strategy", apply: func(input *CreateProjectInput) { input.DeployStrategy = "bogus" }},
		{name: "invalid namespace", apply: func(input *CreateProjectInput) { input.Namespace = "Bad_Ns" }},
		{name: "port too large", apply: func(input *CreateProjectInput) { input.ContainerPort = 70000 }},
		{name: "port negative", apply: func(input *CreateProjectInput) { input.ContainerPort = -1 }},
		{name: "replicas too large", apply: func(input *CreateProjectInput) { input.Replicas = 101 }},
		{name: "replicas negative", apply: func(input *CreateProjectInput) { input.Replicas = -1 }},
		{name: "invalid branch", apply: func(input *CreateProjectInput) { input.DefaultBranch = "release..broken" }},
		{name: "invalid repository scheme", apply: func(input *CreateProjectInput) { input.RepositoryURL = "ftp://example.invalid/repository" }},
		{name: "image repository with tag", apply: func(input *CreateProjectInput) { input.ImageRepository = "registry.example.com/team/app:v1" }},
		{name: "image repository without host", apply: func(input *CreateProjectInput) { input.ImageRepository = "team/app" }},
		{name: "image repository uppercase path", apply: func(input *CreateProjectInput) { input.ImageRepository = "registry.example.com/Team/App" }},
		{name: "image repository with scheme", apply: func(input *CreateProjectInput) { input.ImageRepository = "https://registry.example.com/team/app" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := base
			input.Name += " " + testCase.name
			testCase.apply(&input)
			_, err := NewMemoryWithFixtures().CreateProject(context.Background(), "space-lab", input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("create error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestMemoryProjectSettingsNormalizeDefaultsAndRejectInvalidUpdates(t *testing.T) {
	data := NewMemoryWithFixtures()
	created, err := data.CreateProject(context.Background(), "space-lab", CreateProjectInput{
		Name:          "默认值项目",
		RepositoryURL: "https://example.invalid/defaults",
		ClusterID:     "demo-cluster",
	})
	if err != nil {
		t.Fatalf("create project with defaults: %v", err)
	}
	if created.DefaultBranch != "main" || created.Namespace != "lab" || created.DeployStrategy != "rolling" || created.Replicas != 1 || created.ContainerPort != 8080 {
		t.Fatalf("normalized project = %#v", created)
	}

	badBranch := ""
	badNamespace := "Bad_Ns"
	zeroReplicas := 0
	zeroPort := 0
	updates := []struct {
		name  string
		input UpdateProjectInput
	}{
		{name: "empty branch", input: UpdateProjectInput{DefaultBranch: &badBranch}},
		{name: "invalid namespace", input: UpdateProjectInput{Namespace: &badNamespace}},
		{name: "zero replicas", input: UpdateProjectInput{Replicas: &zeroReplicas}},
		{name: "zero port", input: UpdateProjectInput{ContainerPort: &zeroPort}},
	}
	for _, testCase := range updates {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := data.UpdateProject(context.Background(), "space-lab", created.ID, testCase.input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("update error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func TestMemoryProjectImageRepositoryRoundTrip(t *testing.T) {
	data := NewMemoryWithFixtures()
	created, err := data.CreateProject(context.Background(), "space-lab", CreateProjectInput{
		Name:            "镜像仓库项目",
		RepositoryURL:   "https://example.invalid/image-repository",
		ClusterID:       "demo-cluster",
		ImageRepository: "registry.example.com/team/app",
	})
	if err != nil {
		t.Fatalf("create project with image_repository: %v", err)
	}
	if created.ImageRepository != "registry.example.com/team/app" {
		t.Fatalf("created.ImageRepository = %q", created.ImageRepository)
	}

	loaded, err := data.GetProject(context.Background(), "space-lab", created.ID)
	if err != nil || loaded.ImageRepository != "registry.example.com/team/app" {
		t.Fatalf("read back project: %v %#v", err, loaded.ImageRepository)
	}

	replacement := "registry.example.com/other/team-service"
	updated, err := data.UpdateProject(context.Background(), "space-lab", created.ID, UpdateProjectInput{ImageRepository: &replacement})
	if err != nil || updated.ImageRepository != replacement {
		t.Fatalf("update image_repository: %v %#v", err, updated.ImageRepository)
	}

	cleared := ""
	updated, err = data.UpdateProject(context.Background(), "space-lab", created.ID, UpdateProjectInput{ImageRepository: &cleared})
	if err != nil || updated.ImageRepository != "" {
		t.Fatalf("clear image_repository: %v %#v", err, updated.ImageRepository)
	}
}

func TestMemoryDeploymentTargetsUseExplicitReleaseOrder(t *testing.T) {
	data := NewMemoryWithFixtures()
	target, err := data.CreateDeploymentTarget(context.Background(), "space-lab", "reverse-lab", CreateDeploymentTargetInput{
		Name: "随便起的名字", Environment: "release", Stage: DeploymentStagePre, SortOrder: 3,
		ClusterID: "demo-cluster", Namespace: "release", Replicas: 2, ContainerPort: 8080, DeployStrategy: "rolling",
	})
	if err != nil {
		t.Fatalf("create explicitly ordered target: %v", err)
	}
	if target.Stage != DeploymentStagePre || target.SortOrder != 3 {
		t.Fatalf("target metadata = (%q, %d), want (pre, 3)", target.Stage, target.SortOrder)
	}

	targets, err := data.ListDeploymentTargets(context.Background(), "space-lab", "reverse-lab")
	if err != nil {
		t.Fatalf("list deployment targets: %v", err)
	}
	if len(targets) != 3 || targets[0].Stage != DeploymentStageDev || targets[1].Stage != DeploymentStageUAT || targets[2].ID != target.ID {
		t.Fatalf("targets are not ordered by sort_order: %#v", targets)
	}

	_, err = data.CreateDeploymentTarget(context.Background(), "space-lab", "reverse-lab", CreateDeploymentTargetInput{
		Name: "错误顺序", Environment: "before-dev", Stage: DeploymentStagePre, SortOrder: 1,
		ClusterID: "demo-cluster", Namespace: "before-dev", Replicas: 1, ContainerPort: 8080, DeployStrategy: "rolling",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid first stage error = %v, want ErrInvalidInput", err)
	}
}

func TestMemoryDeploymentTargetDeletionKeepsAValidDevTarget(t *testing.T) {
	data := NewMemoryWithFixtures()
	pre, err := data.CreateDeploymentTarget(context.Background(), "space-lab", "reverse-lab", CreateDeploymentTargetInput{
		Name: "预发布", Environment: "pre", Stage: DeploymentStagePre, SortOrder: 3,
		ClusterID: "demo-cluster", Namespace: "pre", Replicas: 1, ContainerPort: 8080, DeployStrategy: "rolling",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := data.DeleteDeploymentTarget(context.Background(), "space-lab", "reverse-lab", pre.ID); err != nil {
		t.Fatalf("delete non-dev target: %v", err)
	}
	if _, err := data.GetDeploymentTarget(context.Background(), "space-lab", "reverse-lab", pre.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted target lookup error = %v, want ErrNotFound", err)
	}

	targets, err := data.ListDeploymentTargets(context.Background(), "space-lab", "reverse-lab")
	if err != nil || len(targets) != 2 || targets[0].Stage != DeploymentStageDev {
		t.Fatalf("remaining targets = %#v, err = %v", targets, err)
	}
	if err := data.DeleteDeploymentTarget(context.Background(), "space-lab", "reverse-lab", targets[0].ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete last dev target error = %v, want ErrConflict", err)
	}
}

func TestMemoryDeploymentConfigIsAbsentUntilSaved(t *testing.T) {
	data := NewMemoryWithFixtures()
	if _, err := data.GetDeploymentConfig(context.Background(), "space-lab", "reverse-lab"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty deployment config error = %v, want ErrNotFound", err)
	}
	if _, err := data.SaveDeploymentConfig(context.Background(), "space-lab", "reverse-lab", SaveDeploymentConfigInput{Manifest: "   ", Format: "yaml"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty deployment config save error = %v, want ErrInvalidInput", err)
	}
}

func TestMemoryABExperimentLifecycleAndEnvironmentScope(t *testing.T) {
	data := NewMemoryWithFixtures()
	input := CreateABExperimentInput{
		Name: "结算流程 A/B", TargetID: "target-reverse-lab-dev", Environment: "dev", EnvironmentStage: DeploymentStageDev,
		ClusterID: "demo-cluster", Namespace: "lab", Replicas: 2, Strategy: "rolling", Assignment: "percentage",
		AVersion: domain.ABExperimentVersion{Role: "A", ReleaseID: "rel-a", Branch: "main", CommitSHA: "aaaaaaaaaaaa"},
		BVersion: domain.ABExperimentVersion{Role: "B", ReleaseID: "rel-b", Branch: "feature/checkout", CommitSHA: "bbbbbbbbbbbb"},
		ATraffic: 99, BTraffic: 1, CreatedBy: 1,
	}
	created, err := data.CreateABExperiment(context.Background(), "space-lab", "reverse-lab", input)
	if err != nil {
		t.Fatalf("create A/B experiment: %v", err)
	}
	if created.Status != domain.ABExperimentRunning || created.ATraffic != 99 || created.BTraffic != 1 {
		t.Fatalf("unexpected created experiment: %#v", created)
	}

	if _, err := data.CreateABExperiment(context.Background(), "space-lab", "reverse-lab", input); !errors.Is(err, ErrConflict) {
		t.Fatalf("second running experiment error = %v, want conflict", err)
	}
	if _, err := data.UpdateABExperimentTraffic(context.Background(), "space-lab", "reverse-lab", created.ID, UpdateABTrafficInput{ATraffic: 60, BTraffic: 30}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid traffic error = %v, want invalid input", err)
	}
	updated, err := data.UpdateABExperimentTraffic(context.Background(), "space-lab", "reverse-lab", created.ID, UpdateABTrafficInput{ATraffic: 80, BTraffic: 20})
	if err != nil || updated.ATraffic != 80 || updated.BTraffic != 20 || len(updated.Events) != 2 {
		t.Fatalf("traffic update = %#v, %v", updated, err)
	}

	stopped, err := data.StopABExperiment(context.Background(), "space-lab", "reverse-lab", created.ID)
	if err != nil || stopped.Status != domain.ABExperimentStopped || stopped.ATraffic != 100 || stopped.BTraffic != 0 || stopped.FinishedAt == nil {
		t.Fatalf("stop result = %#v, %v", stopped, err)
	}
	if _, err := data.StopABExperiment(context.Background(), "space-lab", "reverse-lab", created.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stopping terminal experiment error = %v, want conflict", err)
	}

	otherInput := input
	otherInput.Name = "另一个实验"
	otherInput.TargetID = "target-reverse-lab-uat"
	otherInput.Environment = "uat"
	otherInput.EnvironmentStage = DeploymentStageUAT
	otherInput.ClusterID = "demo-cluster-uat"
	otherInput.Namespace = "uat"
	otherInput.AVersion.ReleaseID = "rel-uat-a"
	otherInput.BVersion.ReleaseID = "rel-uat-b"
	other, err := data.CreateABExperiment(context.Background(), "space-lab", "reverse-lab", otherInput)
	if err != nil {
		t.Fatalf("create experiment in another environment: %v", err)
	}
	if other.TargetID != "target-reverse-lab-uat" {
		t.Fatalf("other experiment target = %q", other.TargetID)
	}
	if _, err := data.GetABExperiment(context.Background(), "other-space", "reverse-lab", other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-space experiment lookup error = %v, want not found", err)
	}
	if time.Since(other.StartedAt) < 0 {
		t.Fatal("experiment started in the future")
	}
}
