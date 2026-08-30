package release

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
)

func TestPrepareSameBranchIsReady(t *testing.T) {
	service := newPreparationService(t, map[string][]git.Commit{
		"main": {
			preparationCommit("main-2"),
			preparationCommit("main-1"),
		},
	})

	result, err := service.Prepare(context.Background(), preparationInput("main"), "main", "main-2")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != PreparationReady || !result.CanPublish || result.RequiresMerge {
		t.Fatalf("same branch should be ready: %#v", result)
	}
	if result.SourceAhead != 0 || result.BaseAhead != 0 || !result.BaseIncluded {
		t.Fatalf("unexpected same-branch counts: %#v", result)
	}
	if result.TemporaryBranch != "" || len(result.ConflictFiles) != 0 || result.SupportsConflictResolution {
		t.Fatalf("same branch should not suggest a merge branch or conflicts: %#v", result)
	}
	if !result.Capabilities.ReadOnly || result.Capabilities.CanWriteGit || result.Capabilities.CanAutoMerge {
		t.Fatalf("unexpected capabilities: %#v", result.Capabilities)
	}
}

func TestPrepareWhenBaseHistoryIsIncludedIsReady(t *testing.T) {
	base := preparationCommit("base-2")
	baseOld := preparationCommit("base-1")
	service := newPreparationService(t, map[string][]git.Commit{
		"main":    {base, baseOld},
		"release": {preparationCommit("release-1"), base, baseOld},
	})

	result, err := service.Prepare(context.Background(), preparationInput("release"), "main", "release-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != PreparationReady || !result.CanPublish || result.TemporaryBranch != "" {
		t.Fatalf("included base should be ready: %#v", result)
	}
	if result.SourceAhead != 1 || result.BaseAhead != 0 || !result.BaseIncluded {
		t.Fatalf("unexpected included-base counts: %#v", result)
	}
	if result.SourceBranch != "release" || result.BaseBranch != "main" || result.SelectedSHA != "release-1" {
		t.Fatalf("unexpected branch selection: %#v", result)
	}
}

func TestPrepareDivergedBranchesCanPublishWithoutMergingMain(t *testing.T) {
	service := newPreparationService(t, map[string][]git.Commit{
		"main":    {preparationCommit("base-2"), preparationCommit("common")},
		"release": {preparationCommit("release-2"), preparationCommit("common")},
	})
	service.SetClock(func() time.Time {
		return time.Date(2026, time.August, 26, 14, 5, 6, 0, time.FixedZone("CST", 8*60*60))
	})

	result, err := service.Prepare(context.Background(), preparationInput("release"), "main", "release-2")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != PreparationReady || !result.CanPublish || result.RequiresMerge {
		t.Fatalf("diverged branches should still be publishable: %#v", result)
	}
	if result.SourceAhead != 1 || result.BaseAhead != 1 || result.BaseIncluded {
		t.Fatalf("unexpected diverged counts: %#v", result)
	}
	if result.TemporaryBranch != "" || result.ReleaseBranch != "release" || result.ReleaseSHA != "release-2" {
		t.Fatalf("release should use the selected source version directly: %#v", result)
	}
	if len(result.ConflictFiles) != 0 || result.SupportsConflictResolution || result.Capabilities.CanWriteGit || result.Capabilities.CanCreateTemporaryBranch || result.Capabilities.CanDetectConflicts {
		t.Fatalf("preparation claimed unsupported Git operations: %#v", result)
	}
	if !strings.Contains(result.Message, "加入当前开放批次") || !strings.Contains(result.Message, "代码冲突") {
		t.Fatalf("message does not explain batch behavior: %q", result.Message)
	}
}

func TestPrepareMergeUsesIsolatedDemoWorkspaceAndResolveProducesReleaseVersion(t *testing.T) {
	service := NewService(git.NewDemoProvider())
	service.SetClock(func() time.Time {
		return time.Date(2026, time.August, 26, 14, 5, 6, 0, time.UTC)
	})
	input := CreateInput{ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "release/2026.01"}

	conflict, err := service.PrepareMerge(context.Background(), input, "main", "112233445566", "release-prep-test")
	if err != nil {
		t.Fatal(err)
	}
	if conflict.Status != PreparationConflict || conflict.CanPublish || len(conflict.ConflictFiles) != 1 || conflict.TemporaryBranch != "release-prep-test" {
		t.Fatalf("unexpected isolated merge result: %#v", conflict)
	}
	if !conflict.SupportsConflictResolution || !conflict.Capabilities.CanWriteGit {
		t.Fatalf("demo provider did not expose merge capabilities: %#v", conflict)
	}

	resolvedContent := "replicas: 2\nimageTag: release\n"
	ready, err := service.ResolveMerge(context.Background(), input, "main", "112233445566", "release-prep-test", []git.MergeResolution{{Path: "deploy/application.yaml", Content: resolvedContent}})
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != PreparationReady || !ready.CanPublish || ready.ReleaseBranch != "release-prep-test" || ready.ReleaseSHA == "" {
		t.Fatalf("unexpected resolved merge result: %#v", ready)
	}
	if ready.ConflictFiles[0].Status != "resolved" || ready.ConflictFiles[0].ResolvedContent != resolvedContent {
		t.Fatalf("resolution was not retained: %#v", ready.ConflictFiles)
	}

	commits, err := service.provider.ListCommits(context.Background(), "demo-repo", "release-prep-test", 10)
	if err != nil || len(commits) != 1 || commits[0].SHA != ready.ReleaseSHA {
		t.Fatalf("prepared branch was not readable: commits=%#v err=%v", commits, err)
	}
}

func TestPrepareRejectsUnknownBranches(t *testing.T) {
	service := newPreparationService(t, map[string][]git.Commit{
		"main":    {preparationCommit("main-1")},
		"release": {preparationCommit("release-1")},
	})

	_, err := service.Prepare(context.Background(), preparationInput("missing"), "main", "main-1")
	if !errors.Is(err, git.ErrBranchNotFound) {
		t.Fatalf("unknown source branch error = %v", err)
	}
	_, err = service.Prepare(context.Background(), preparationInput("release"), "missing", "release-1")
	if !errors.Is(err, git.ErrBranchNotFound) {
		t.Fatalf("unknown base branch error = %v", err)
	}
}

func TestPrepareRejectsSelectedCommitThatDoesNotExist(t *testing.T) {
	service := newPreparationService(t, map[string][]git.Commit{
		"main":    {preparationCommit("main-1")},
		"release": {preparationCommit("release-1")},
	})

	_, err := service.Prepare(context.Background(), preparationInput("release"), "main", "missing")
	if !errors.Is(err, git.ErrCommitNotFound) {
		t.Fatalf("unknown selected commit error = %v", err)
	}
}

func TestPrepareRejectsSelectedCommitFromAnotherBranch(t *testing.T) {
	service := newPreparationService(t, map[string][]git.Commit{
		"main":    {preparationCommit("main-1")},
		"release": {preparationCommit("release-1")},
	})

	_, err := service.Prepare(context.Background(), preparationInput("release"), "main", "main-1")
	if !errors.Is(err, git.ErrCommitNotFound) {
		t.Fatalf("commit from another branch error = %v", err)
	}
}

func TestPreparationJSONUsesSnakeCaseAndExplainsCapabilities(t *testing.T) {
	service := newPreparationService(t, map[string][]git.Commit{
		"main":    {preparationCommit("base-1")},
		"release": {preparationCommit("release-1"), preparationCommit("base-1")},
	})
	result, err := service.Prepare(context.Background(), preparationInput("release"), "main", "release-1")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	for _, field := range []string{`"source_branch"`, `"base_branch"`, `"selected_sha"`, `"source_ahead"`, `"base_ahead"`, `"can_publish"`, `"can_write_git"`, `"can_auto_merge"`, `"supports_conflict_resolution"`, `"conflict_files"`} {
		if !strings.Contains(encoded, field) {
			t.Fatalf("JSON is missing %s: %s", field, encoded)
		}
	}
	if strings.Contains(encoded, `"sourceBranch"`) || strings.Contains(encoded, `"baseBranch"`) {
		t.Fatalf("JSON contains camelCase fields: %s", encoded)
	}
}

func newPreparationService(t *testing.T, histories map[string][]git.Commit) *Service {
	t.Helper()
	branches := make(map[string][]git.Commit, len(histories))
	for name, commits := range histories {
		branches[name] = append([]git.Commit(nil), commits...)
	}
	provider := git.NewDemoProviderWithRepositories([]git.DemoRepositoryInput{
		{
			Repository: git.Repository{ID: "preparation-repo", Name: "Preparation test", DefaultBranch: "main"},
			Branches:   branches,
		},
	})
	return NewService(provider)
}

func preparationInput(branch string) CreateInput {
	return CreateInput{ProjectID: "preparation-project", RepositoryID: "preparation-repo", Branch: branch}
}

func preparationCommit(sha string) git.Commit {
	return git.Commit{SHA: sha, ShortSHA: sha}
}
