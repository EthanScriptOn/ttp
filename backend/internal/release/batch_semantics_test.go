package release

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
)

func TestBatchFirstReleaseOpensBatchAndLaterReleaseJoinsIt(t *testing.T) {
	service, _ := newBatchSemanticsService(t)
	first := createBatchRelease(t, service, "release-a", "a1")
	second := createBatchRelease(t, service, "release-b", "b1")

	first, batch, err := service.AttachToBatch(context.Background(), first.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != BatchOpen || batch.BaseBranch != "main" || batch.BaseSHA != "main-base" {
		t.Fatalf("first release did not create the expected open batch: %#v", batch)
	}
	if first.BatchID != batch.ID || first.BatchBranch != batch.Branch || first.BatchBaseSHA != batch.BaseSHA {
		t.Fatalf("first release was not attached to its batch: %#v", first)
	}
	if first.BatchSnapshotSHA == "" || first.BatchSnapshotSHA != batch.HeadSHA {
		t.Fatalf("first release did not retain its batch snapshot: release=%#v batch=%#v", first, batch)
	}

	second, laterBatch, err := service.AttachToBatch(context.Background(), second.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if laterBatch.ID != batch.ID || laterBatch.Branch != batch.Branch || laterBatch.BaseSHA != batch.BaseSHA {
		t.Fatalf("later release did not join the original batch: first=%#v later=%#v", batch, laterBatch)
	}
	if second.BatchID != first.BatchID || len(laterBatch.Items) != 2 {
		t.Fatalf("batch membership is not shared by both releases: release=%#v batch=%#v", second, laterBatch)
	}
	if second.BatchSnapshotSHA == "" || second.BatchSnapshotSHA != laterBatch.HeadSHA || second.BatchSnapshotSHA == first.BatchSnapshotSHA {
		t.Fatalf("later release did not receive its own batch snapshot: first=%#v second=%#v batch=%#v", first, second, laterBatch)
	}
	if laterBatch.Items[0].BatchSnapshotSHA != first.BatchSnapshotSHA || laterBatch.Items[1].BatchSnapshotSHA != second.BatchSnapshotSHA {
		t.Fatalf("batch API did not expose each release item's immutable snapshot: %#v", laterBatch.Items)
	}
	if laterBatch.Items[0].BatchSnapshotRevision != first.BatchSnapshotRevision || laterBatch.Items[1].BatchSnapshotRevision != second.BatchSnapshotRevision {
		t.Fatalf("batch API did not expose each release item's snapshot revision: %#v", laterBatch.Items)
	}
	refreshedFirst, err := service.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshedFirst.BatchSnapshotSHA != first.BatchSnapshotSHA {
		t.Fatalf("later batch membership changed the first release snapshot: first=%#v refreshed=%#v", first, refreshedFirst)
	}
	if refreshedFirst.BatchSnapshotRevision != first.BatchSnapshotRevision {
		t.Fatalf("later batch membership changed the first release snapshot revision: first=%#v refreshed=%#v", first, refreshedFirst)
	}
	if laterBatch.Items[0].ReleaseID != first.ID || laterBatch.Items[1].ReleaseID != second.ID {
		t.Fatalf("batch items are not ordered by release creation: %#v", laterBatch.Items)
	}
}

func TestBatchReleasesKeepDifferentSnapshotsWhenSelectedCommitsAlreadyExistInMain(t *testing.T) {
	base := func(sha string) git.Commit { return git.Commit{SHA: sha, ShortSHA: sha} }
	provider := git.NewDemoProviderWithRepositories([]git.DemoRepositoryInput{{
		Repository: git.Repository{ID: "batch-existing-commits-repo", Name: "Existing commits", DefaultBranch: "main"},
		Branches: map[string][]git.Commit{
			"main":      {base("a1"), base("b1"), base("main-base")},
			"release-a": {base("a1"), base("b1"), base("main-base")},
			"release-b": {base("b1"), base("a1"), base("main-base")},
		},
	}})
	service := NewService(provider)
	first := createBatchReleaseWithRepository(t, service, "batch-existing-commits-repo", "release-a", "a1")
	second := createBatchReleaseWithRepository(t, service, "batch-existing-commits-repo", "release-b", "b1")

	first, batch, err := service.AttachToBatch(context.Background(), first.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	second, laterBatch, err := service.AttachToBatch(context.Background(), second.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if laterBatch.ID != batch.ID || laterBatch.HeadSHA != batch.HeadSHA {
		t.Fatalf("adding commits already present in main unexpectedly moved the shared batch: first=%#v later=%#v", batch, laterBatch)
	}
	if first.BatchSnapshotSHA != "a1" || second.BatchSnapshotSHA != "b1" {
		t.Fatalf("release snapshots did not retain their selected commits: first=%#v second=%#v", first, second)
	}
	if first.BatchSnapshotSHA == second.BatchSnapshotSHA {
		t.Fatalf("different release items reused one snapshot: first=%#v second=%#v", first, second)
	}
	if laterBatch.Items[0].BatchSnapshotSHA != first.BatchSnapshotSHA || laterBatch.Items[1].BatchSnapshotSHA != second.BatchSnapshotSHA {
		t.Fatalf("batch items did not expose per-release snapshots: %#v", laterBatch.Items)
	}
}

func TestReleasesAdvanceIndependentlyAndOnlyOneItemMergesIntoMain(t *testing.T) {
	service, provider := newBatchSemanticsService(t)
	first := createBatchRelease(t, service, "release-a", "a1")
	second := createBatchRelease(t, service, "release-b", "b1")
	attachBatchRelease(t, service, first.ID)
	attachBatchRelease(t, service, second.ID)

	if _, _, err := service.MergeToMain(context.Background(), second.ID); !errors.Is(err, ErrMainMergeNotReady) {
		t.Fatalf("unfinished release was allowed into main: %v", err)
	}

	completeBatchRelease(t, service, first.ID)
	mergedFirst, batch, err := service.MergeToMain(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mergedFirst.MainMergeStatus != MainMergeMerged || batch.Items[0].MainMergeStatus != MainMergeMerged {
		t.Fatalf("first release was not marked as individually merged: release=%#v batch=%#v", mergedFirst, batch)
	}
	assertMainContainsOnlySelectedRelease(t, provider, "a1", "b1")

	second, err = service.Get(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != StatusDraft || second.Targets[0].Status != TargetPending || second.Targets[1].Status != TargetWaiting {
		t.Fatalf("first release rollout changed the second release: %#v", second)
	}

	completeBatchRelease(t, service, second.ID)
	mergedSecond, _, err := service.MergeToMain(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mergedSecond.MainMergeStatus != MainMergeMerged {
		t.Fatalf("second release was not independently merged: %#v", mergedSecond)
	}
	assertMainContainsReleases(t, provider, "a1", "b1")
}

func TestMainMergeRejectsWhenMainChangesAfterBatchAttachment(t *testing.T) {
	service, provider := newBatchSemanticsService(t)
	release := createBatchRelease(t, service, "release-a", "a1")
	batch := attachBatchRelease(t, service, release.ID)
	completeBatchRelease(t, service, release.ID)

	if _, err := provider.MergeReleaseToMain(context.Background(), git.MainMergeRequest{
		RepositoryID:   "batch-semantics-repo",
		SourceBranch:   "release-external",
		BaseBranch:     "main",
		CurrentMainSHA: batch.BaseSHA,
		SelectedSHAs:   []string{"external-1"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := service.MergeToMain(context.Background(), release.ID); !errors.Is(err, ErrBatchConflict) {
		t.Fatalf("main change was not rejected: %v", err)
	}
	unchanged, err := service.Get(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.MainMergeStatus != MainMergePending || unchanged.MainMergeSHA != "" {
		t.Fatalf("rejected merge changed release state: %#v", unchanged)
	}
}

func TestMainMergeRequiresCompletedProduction(t *testing.T) {
	service, _ := newBatchSemanticsService(t)
	release := createBatchRelease(t, service, "release-a", "a1")
	attachBatchRelease(t, service, release.ID)

	for _, targetID := range []string{"dev", "uat", "pre"} {
		if _, _, err := service.StartTarget(release.ID, targetID); err != nil {
			t.Fatalf("start %s: %v", targetID, err)
		}
		if _, err := service.CompleteTarget(release.ID, targetID); err != nil {
			t.Fatalf("complete %s: %v", targetID, err)
		}
	}

	if _, _, err := service.MergeToMain(context.Background(), release.ID); !errors.Is(err, ErrMainMergeNotReady) {
		t.Fatalf("release without completed PROD was allowed into main: %v", err)
	}
}

func TestBatchAttachmentFreezesSelectedCommits(t *testing.T) {
	service, _ := newBatchSemanticsService(t)
	release, duplicate, err := service.Create(context.Background(), CreateInput{
		ProjectID: "batch-semantics-project", RepositoryID: "batch-semantics-repo", Branch: "release-a",
		CommitSHAs: []string{"a1", "main-base"}, Targets: batchEnvironmentInputs(),
	})
	if err != nil || duplicate {
		t.Fatalf("create release: duplicate=%v err=%v", duplicate, err)
	}
	_, batch, err := service.AttachToBatch(context.Background(), release.ID, "main")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.RemoveCommit(release.ID, "a1"); !errors.Is(err, ErrReleaseImmutable) {
		t.Fatalf("attached release allowed its selected commits to change: %v", err)
	}
	stored, err := service.Get(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Commits) != 2 || len(batch.Items) != 1 || len(batch.Items[0].Commits) != 2 {
		t.Fatalf("attached release or batch lost its fixed commit snapshot: release=%#v batch=%#v", stored, batch)
	}
}

func TestConcurrentMainMergesAreSerializedAndEachItemRemainsIndependent(t *testing.T) {
	service, provider := newBatchSemanticsService(t)
	first := createBatchRelease(t, service, "release-a", "a1")
	second := createBatchRelease(t, service, "release-b", "b1")
	attachBatchRelease(t, service, first.ID)
	attachBatchRelease(t, service, second.ID)
	completeBatchRelease(t, service, first.ID)
	completeBatchRelease(t, service, second.ID)

	type mergeResult struct {
		releaseID string
		err       error
	}
	started := make(chan struct{})
	results := make(chan mergeResult, 2)
	var wait sync.WaitGroup
	for _, releaseID := range []string{first.ID, second.ID} {
		wait.Add(1)
		go func(releaseID string) {
			defer wait.Done()
			<-started
			_, _, err := service.MergeToMain(context.Background(), releaseID)
			results <- mergeResult{releaseID: releaseID, err: err}
		}(releaseID)
	}
	close(started)
	wait.Wait()
	close(results)

	merged := 0
	for result := range results {
		if result.err == nil {
			merged++
			continue
		}
		t.Fatalf("unexpected concurrent merge error for %s: %v", result.releaseID, result.err)
	}
	if merged != 2 {
		t.Fatalf("expected both independently completed releases to merge in sequence: merged=%d", merged)
	}

	commits, err := provider.ListCommits(context.Background(), "batch-semantics-repo", "main", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !containsBatchCommit(commits, "a1") || !containsBatchCommit(commits, "b1") {
		t.Fatalf("main did not contain both independently merged releases: %#v", commits)
	}
}

func TestMainMergeRejectsExternalMainChangeDuringProviderCall(t *testing.T) {
	_, baseProvider := newBatchSemanticsService(t)
	provider := &mainChangeBeforeMergeProvider{DemoProvider: baseProvider}
	service := NewService(provider)
	release := createBatchRelease(t, service, "release-a", "a1")
	attachBatchRelease(t, service, release.ID)
	completeBatchRelease(t, service, release.ID)

	if _, _, err := service.MergeToMain(context.Background(), release.ID); !errors.Is(err, ErrBatchConflict) {
		t.Fatalf("provider-side main CAS did not reject the race: %v", err)
	}
	unchanged, err := service.Get(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.MainMergeStatus != MainMergeConflict || unchanged.MainMergeSHA != "" {
		t.Fatalf("provider-side CAS rejection changed release state: %#v", unchanged)
	}
}

type mainChangeBeforeMergeProvider struct {
	*git.DemoProvider
	once sync.Once
}

func (p *mainChangeBeforeMergeProvider) MergeReleaseToMain(ctx context.Context, request git.MainMergeRequest) (git.MainMergeResult, error) {
	var changeErr error
	p.once.Do(func() {
		_, changeErr = p.DemoProvider.MergeReleaseToMain(ctx, git.MainMergeRequest{
			RepositoryID: "batch-semantics-repo", SourceBranch: "release-external", BaseBranch: "main",
			CurrentMainSHA: "main-base", SelectedSHAs: []string{"external-1"},
		})
	})
	if changeErr != nil {
		return git.MainMergeResult{}, changeErr
	}
	return p.DemoProvider.MergeReleaseToMain(ctx, request)
}

func newBatchSemanticsService(t *testing.T) (*Service, *git.DemoProvider) {
	t.Helper()
	base := func(sha string) git.Commit { return git.Commit{SHA: sha, ShortSHA: sha} }
	provider := git.NewDemoProviderWithRepositories([]git.DemoRepositoryInput{{
		Repository: git.Repository{ID: "batch-semantics-repo", Name: "Batch semantics test", DefaultBranch: "main"},
		Branches: map[string][]git.Commit{
			"main":             {base("main-base")},
			"release-a":        {base("a1"), base("main-base")},
			"release-b":        {base("b1"), base("main-base")},
			"release-external": {base("external-1"), base("main-base")},
		},
	}})
	return NewService(provider), provider
}

func createBatchRelease(t *testing.T, service *Service, branch, sha string) Release {
	return createBatchReleaseWithRepository(t, service, "batch-semantics-repo", branch, sha)
}

func createBatchReleaseWithRepository(t *testing.T, service *Service, repositoryID, branch, sha string) Release {
	t.Helper()
	release, duplicate, err := service.Create(context.Background(), CreateInput{
		ProjectID: "batch-semantics-project", RepositoryID: repositoryID, Branch: branch,
		CommitSHAs: []string{sha}, Targets: batchEnvironmentInputs(),
	})
	if err != nil {
		t.Fatalf("create %s: %v", branch, err)
	}
	if duplicate {
		t.Fatalf("create %s unexpectedly returned a duplicate", branch)
	}
	return release
}

func attachBatchRelease(t *testing.T, service *Service, releaseID string) Batch {
	t.Helper()
	_, batch, err := service.AttachToBatch(context.Background(), releaseID, "main")
	if err != nil {
		t.Fatalf("attach %s: %v", releaseID, err)
	}
	return batch
}

func completeBatchRelease(t *testing.T, service *Service, releaseID string) {
	t.Helper()
	for _, targetID := range []string{"dev", "uat", "pre", "prod"} {
		if _, claimed, err := service.StartTarget(releaseID, targetID); err != nil || !claimed {
			t.Fatalf("start %s: claimed=%v err=%v", targetID, claimed, err)
		}
		if _, err := service.CompleteTarget(releaseID, targetID); err != nil {
			t.Fatalf("complete %s: %v", targetID, err)
		}
	}
	finished, err := service.FinalizeTargets(releaseID)
	if err != nil {
		t.Fatalf("finalize %s: %v", releaseID, err)
	}
	if finished.Status != StatusSucceeded || !productionSucceeded(finished.Targets) {
		t.Fatalf("release %s did not finish through PROD: %#v", releaseID, finished)
	}
}

func batchEnvironmentInputs() []TargetInput {
	return []TargetInput{
		{ID: "dev", Environment: "dev"},
		{ID: "uat", Environment: "uat"},
		{ID: "pre", Environment: "pre"},
		{ID: "prod", Environment: "prod"},
	}
}

func assertMainContainsOnlySelectedRelease(t *testing.T, provider *git.DemoProvider, selected, excluded string) {
	t.Helper()
	commits, err := provider.ListCommits(context.Background(), "batch-semantics-repo", "main", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !containsBatchCommit(commits, selected) {
		t.Fatalf("main does not contain selected release commit %s: %#v", selected, commits)
	}
	if containsBatchCommit(commits, excluded) {
		t.Fatalf("main unexpectedly contains another batch release commit %s: %#v", excluded, commits)
	}
}

func assertMainContainsReleases(t *testing.T, provider *git.DemoProvider, selected ...string) {
	t.Helper()
	commits, err := provider.ListCommits(context.Background(), "batch-semantics-repo", "main", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, sha := range selected {
		if !containsBatchCommit(commits, sha) {
			t.Fatalf("main does not contain release commit %s: %#v", sha, commits)
		}
	}
}

func containsBatchCommit(commits []git.Commit, sha string) bool {
	for _, commit := range commits {
		if commit.SHA == sha {
			return true
		}
	}
	return false
}
