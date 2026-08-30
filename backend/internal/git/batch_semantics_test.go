package git

import (
	"context"
	"strings"
	"testing"
)

func TestDemoProviderBatchAndMainMergesHonorSelectedCommitsAndMainCAS(t *testing.T) {
	commit := func(sha string) Commit { return Commit{SHA: sha, ShortSHA: sha} }
	provider := NewDemoProviderWithRepositories([]DemoRepositoryInput{{
		Repository: Repository{ID: "provider-batch-test", Name: "Provider batch test", DefaultBranch: "main"},
		Branches: map[string][]Commit{
			"main":    {commit("main-base")},
			"release": {commit("release-a"), commit("release-b"), commit("main-base")},
		},
	}})

	batch, err := provider.IntegrateReleaseToBatch(context.Background(), BatchBranchRequest{
		RepositoryID: "provider-batch-test", BatchBranch: "batch-1", BaseBranch: "main", BaseSHA: "main-base",
		SourceBranch: "release", SelectedSHAs: []string{"release-a"},
	})
	if err != nil || batch.Status != "ready" {
		t.Fatalf("integrate selected commit into batch: result=%#v err=%v", batch, err)
	}
	batchCommits, err := provider.ListCommits(context.Background(), "provider-batch-test", "batch-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCommit(batchCommits, "release-a") || hasCommit(batchCommits, "release-b") {
		t.Fatalf("batch branch did not contain only selected commit: %#v", batchCommits)
	}

	merged, err := provider.MergeReleaseToMain(context.Background(), MainMergeRequest{
		RepositoryID: "provider-batch-test", SourceBranch: "batch-1", BaseBranch: "main",
		BatchBranch: "batch-1", CurrentMainSHA: "main-base", SelectedSHAs: []string{"release-a"},
	})
	if err != nil || merged.Status != "merged" {
		t.Fatalf("merge selected release into main: result=%#v err=%v", merged, err)
	}

	mainCommits, err := provider.ListCommits(context.Background(), "provider-batch-test", "main", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCommit(mainCommits, "release-a") || hasCommit(mainCommits, "release-b") {
		t.Fatalf("main received more than the selected release: %#v", mainCommits)
	}

	cas, err := provider.MergeReleaseToMain(context.Background(), MainMergeRequest{
		RepositoryID: "provider-batch-test", SourceBranch: "release", BaseBranch: "main",
		CurrentMainSHA: "main-base", SelectedSHAs: []string{"release-b"},
	})
	if err != nil || !strings.EqualFold(cas.Status, "conflict") {
		t.Fatalf("stale main CAS was not rejected: result=%#v err=%v", cas, err)
	}
}

func hasCommit(commits []Commit, sha string) bool {
	for _, commit := range commits {
		if strings.EqualFold(commit.SHA, sha) {
			return true
		}
	}
	return false
}
