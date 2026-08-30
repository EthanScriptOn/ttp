package release

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
)

func TestCreateIsIdempotentAndCommitCanBeRemoved(t *testing.T) {
	service := NewService(git.NewDemoProvider())
	service.SetClock(func() time.Time { return time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC) })
	input := CreateInput{ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main", CommitSHAs: []string{"a1b2c3d4e5f6", "f6e5d4c3b2a1"}, Strategy: StrategyCanary}
	first, duplicate, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate || first.Status != StatusDraft || first.Plan.Traffic.CandidatePercent != 10 {
		t.Fatalf("unexpected first release: duplicate=%v release=%#v", duplicate, first)
	}
	second, duplicate, err := service.Create(context.Background(), CreateInput{ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main", CommitSHAs: []string{"f6e5d4c3b2a1", "a1b2c3d4e5f6"}, Strategy: StrategyCanary})
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate || second.ID != first.ID {
		t.Fatalf("expected duplicate of %s, got duplicate=%v %s", first.ID, duplicate, second.ID)
	}

	updated, err := service.RemoveCommit(first.ID, "f6e5d4c3b2a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Commits) != 1 || updated.Commits[0].SHA != "a1b2c3d4e5f6" {
		t.Fatalf("unexpected remaining commits: %#v", updated.Commits)
	}
	if _, duplicate, err = service.Create(context.Background(), input); err != nil || duplicate {
		t.Fatalf("edited release should no longer occupy old fingerprint: duplicate=%v err=%v", duplicate, err)
	}
}

func TestReleaseStatusFlow(t *testing.T) {
	service := NewService(nil)
	release, _, err := service.Create(context.Background(), CreateInput{ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []Status{StatusQueued, StatusRunning, StatusSucceeded} {
		release, err = service.Transition(release.ID, status)
		if err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
	}
	if _, err = service.Transition(release.ID, StatusFailed); !errors.Is(err, ErrInvalidStatusFlow) {
		t.Fatalf("expected terminal flow error, got %v", err)
	}
	if _, err = service.RemoveCommit(release.ID, release.Commits[0].SHA); !errors.Is(err, ErrReleaseImmutable) {
		t.Fatalf("expected immutable error, got %v", err)
	}
	if _, err = service.Transition(release.ID, StatusQueued); err != nil {
		t.Fatalf("completed release should support repeat publish: %v", err)
	}
}

func TestReleaseExecutionMetadataAndTerminalImmutability(t *testing.T) {
	service := NewService(nil)
	clock := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return clock })
	release, _, err := service.Create(context.Background(), CreateInput{ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}

	// Optional execution fields stay absent from a newly-created release so
	// clients that only understand the original response remain compatible.
	encoded, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"progress"`) || strings.Contains(string(encoded), `"started_at"`) {
		t.Fatalf("draft unexpectedly contains execution metadata: %s", encoded)
	}

	if _, err = service.Transition(release.ID, StatusQueued); err != nil {
		t.Fatal(err)
	}
	progress, err := service.UpdateProgress(release.ID, 35, "building", "正在构建镜像")
	if err != nil {
		t.Fatal(err)
	}
	if progress.Progress != 35 || progress.Stage != "building" || progress.Message != "正在构建镜像" {
		t.Fatalf("unexpected progress update: %#v", progress)
	}
	if progress.StartedAt != nil || progress.FinishedAt != nil {
		t.Fatalf("queued release should not have execution timestamps: %#v", progress)
	}

	clock = clock.Add(time.Minute)
	running, err := service.Transition(release.ID, StatusRunning)
	if err != nil {
		t.Fatal(err)
	}
	if running.StartedAt == nil || running.StartedAt.Equal(clock) == false {
		t.Fatalf("running release should record start time: %#v", running)
	}
	*running.StartedAt = running.StartedAt.Add(time.Hour)
	stored, err := service.Get(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.StartedAt == nil || !stored.StartedAt.Equal(clock) {
		t.Fatalf("release timestamp was mutated through a returned copy: %#v", stored)
	}

	clock = clock.Add(time.Minute)
	finished, err := service.Transition(release.ID, StatusSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Progress != 100 || finished.Stage != "succeeded" || finished.FinishedAt == nil || !finished.FinishedAt.Equal(clock) {
		t.Fatalf("unexpected completed metadata: %#v", finished)
	}
	if _, err = service.UpdateProgress(release.ID, 80, "rollback", ""); !errors.Is(err, ErrReleaseImmutable) {
		t.Fatalf("expected completed progress to be immutable, got %v", err)
	}
	if _, err = service.Cancel(release.ID, "operator requested cancellation"); !errors.Is(err, ErrReleaseImmutable) {
		t.Fatalf("expected completed release cancellation to be immutable, got %v", err)
	}

	// Re-queue remains the existing repeat-publish operation, but starts with
	// clean execution metadata for the new run.
	requeued, err := service.Transition(release.ID, StatusQueued)
	if err != nil {
		t.Fatalf("repeat publish should still be supported: %v", err)
	}
	if requeued.Progress != 0 || requeued.Stage != "" || requeued.Message != "" || requeued.Error != "" || requeued.StartedAt != nil || requeued.FinishedAt != nil {
		t.Fatalf("repeat publish did not reset execution metadata: %#v", requeued)
	}
}

func TestReleaseFailAndCancel(t *testing.T) {
	service := NewService(nil)
	release, _, err := service.Create(context.Background(), CreateInput{ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Transition(release.ID, StatusQueued); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Transition(release.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	failed, err := service.Fail(release.ID, "镜像构建失败")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.Error != "镜像构建失败" || failed.FinishedAt == nil {
		t.Fatalf("unexpected failed release: %#v", failed)
	}
	if _, err = service.Fail(release.ID, "再次失败"); !errors.Is(err, ErrReleaseImmutable) {
		t.Fatalf("expected failed release to be immutable: %v", err)
	}

	other, _, err := service.Create(context.Background(), CreateInput{ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "release/2026.01"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.Cancel(other.ID, "用户取消发布")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusCancelled || cancelled.Stage != "cancelled" || cancelled.Message != "用户取消发布" || cancelled.FinishedAt == nil {
		t.Fatalf("unexpected cancelled release: %#v", cancelled)
	}
	if _, err = service.UpdateProgress(other.ID, 10, "deploying", ""); !errors.Is(err, ErrReleaseImmutable) {
		t.Fatalf("expected cancelled release progress to be immutable: %v", err)
	}

	if _, err = service.UpdateProgress(other.ID, 101, "deploying", ""); !errors.Is(err, ErrInvalidProgress) {
		t.Fatalf("expected invalid progress error, got %v", err)
	}
	if _, err = service.Fail(other.ID, ""); !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("expected missing error message error, got %v", err)
	}
}

func TestCancelPendingTargetsAfterTargetFailure(t *testing.T) {
	service := NewService(nil)
	release, _, err := service.Create(context.Background(), CreateInput{
		ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main",
		Targets: []TargetInput{{ID: "dev", Name: "开发环境"}, {ID: "uat", Name: "测试环境"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.FailTarget(release.ID, "dev", "集群不可用"); err != nil {
		t.Fatal(err)
	}
	updated, err := service.CancelPendingTargets(release.ID, "因开发环境失败，未继续发布")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Targets[0].Status != TargetFailed || updated.Targets[1].Status != TargetCancelled {
		t.Fatalf("unexpected target statuses: %#v", updated.Targets)
	}
	if updated.Targets[1].Message == "" || updated.Targets[1].FinishedAt == nil {
		t.Fatalf("pending target was not closed: %#v", updated.Targets[1])
	}
}

func TestRetryTargetPreservesSuccessfulTargets(t *testing.T) {
	service := NewService(nil)
	release, _, err := service.Create(context.Background(), CreateInput{
		ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main",
		Targets: []TargetInput{{ID: "dev", Name: "开发环境"}, {ID: "uat", Name: "测试环境"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Transition(release.ID, StatusQueued); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Transition(release.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateTargetProgress(release.ID, "dev", 100, "checking", "开发环境已完成"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CompleteTarget(release.ID, "dev"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.FailTarget(release.ID, "uat", "测试集群暂时不可用"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Fail(release.ID, "测试环境发布失败"); err != nil {
		t.Fatal(err)
	}

	retried, err := service.RetryTarget(release.ID, "uat")
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != StatusRunning || retried.Targets[0].Status != TargetSucceeded || retried.Targets[1].Status != TargetPending {
		t.Fatalf("retry should preserve the successful target: %#v", retried)
	}
	if retried.Targets[0].Progress != 100 || retried.Targets[0].FinishedAt == nil {
		t.Fatalf("successful target was reset during retry: %#v", retried.Targets[0])
	}

	if _, err = service.CompleteTarget(release.ID, "uat"); err != nil {
		t.Fatal(err)
	}
	finished, err := service.FinalizeTargets(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != StatusSucceeded || finished.Progress != 100 {
		t.Fatalf("release did not finish after the failed target was retried: %#v", finished)
	}
}

func TestFinalizeTargetsUpdatesReleaseStatus(t *testing.T) {
	service := NewService(nil)
	release, _, err := service.Create(context.Background(), CreateInput{
		ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main",
		Targets: []TargetInput{{ID: "dev", Name: "开发环境"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Transition(release.ID, StatusQueued); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Transition(release.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CompleteTarget(release.ID, "dev"); err != nil {
		t.Fatal(err)
	}
	finished, err := service.FinalizeTargets(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != StatusSucceeded {
		t.Fatalf("finalize left release in %s", finished.Status)
	}
}

func TestReleaseTargetsAdvanceInEnvironmentOrder(t *testing.T) {
	service := NewService(nil)
	release, _, err := service.Create(context.Background(), CreateInput{
		ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main",
		Targets: []TargetInput{
			{ID: "prod", Name: "生产环境", Environment: "prod"},
			{ID: "pre", Name: "预发布环境", Environment: "pre"},
			{ID: "uat", Name: "测试环境", Environment: "uat"},
			{ID: "dev", Name: "开发环境", Environment: "dev"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []TargetStatus{release.Targets[0].Status, release.Targets[1].Status, release.Targets[2].Status, release.Targets[3].Status}; !reflect.DeepEqual(got, []TargetStatus{TargetPending, TargetWaiting, TargetWaiting, TargetWaiting}) {
		t.Fatalf("targets were not initialized in rollout order: %#v", got)
	}
	if got := []string{release.Targets[0].Environment, release.Targets[1].Environment, release.Targets[2].Environment, release.Targets[3].Environment}; !reflect.DeepEqual(got, []string{"dev", "uat", "pre", "prod"}) {
		t.Fatalf("targets were not sorted by environment: %#v", got)
	}

	if _, err = service.CompleteTarget(release.ID, "uat"); !errors.Is(err, ErrTargetNotReady) {
		t.Fatalf("expected UAT completion to be blocked before DEV, got %v", err)
	}
	if _, _, err = service.StartTarget(release.ID, "uat"); !errors.Is(err, ErrTargetNotReady) {
		t.Fatalf("expected UAT start to be blocked before DEV, got %v", err)
	}

	started, claimed, err := service.StartTarget(release.ID, "dev")
	if err != nil || !claimed || started.Targets[0].Status != TargetRunning {
		t.Fatalf("DEV was not started: claimed=%v release=%#v err=%v", claimed, started, err)
	}
	if _, err = service.CompleteTarget(release.ID, "dev"); err != nil {
		t.Fatal(err)
	}
	progressed, err := service.Get(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progressed.Targets[1].Status != TargetPending || progressed.Targets[2].Status != TargetWaiting || progressed.Targets[3].Status != TargetWaiting {
		t.Fatalf("completing DEV did not unlock only UAT: %#v", progressed.Targets)
	}

	for _, targetID := range []string{"uat", "pre", "prod"} {
		if _, claimed, err = service.StartTarget(release.ID, targetID); err != nil || !claimed {
			t.Fatalf("failed to start %s: claimed=%v err=%v", targetID, claimed, err)
		}
		if _, err = service.CompleteTarget(release.ID, targetID); err != nil {
			t.Fatalf("failed to complete %s: %v", targetID, err)
		}
	}
	finished, err := service.FinalizeTargets(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != StatusSucceeded || !productionSucceeded(finished.Targets) {
		t.Fatalf("ordered release did not finish with an explicit production target: %#v", finished)
	}
}

func TestFailedTargetLeavesLaterEnvironmentsWaiting(t *testing.T) {
	service := NewService(nil)
	release, _, err := service.Create(context.Background(), CreateInput{
		ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main",
		Targets: []TargetInput{
			{ID: "dev", Environment: "dev"},
			{ID: "uat", Environment: "uat"},
			{ID: "pre", Environment: "pre"},
			{ID: "prod", Environment: "prod"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.StartTarget(release.ID, "dev"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.FailTarget(release.ID, "dev", "开发集群不可用"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Fail(release.ID, "DEV 发布失败"); err != nil {
		t.Fatal(err)
	}

	failed, err := service.Get(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range failed.Targets[1:] {
		if target.Status != TargetWaiting {
			t.Fatalf("later target was changed after DEV failure: %#v", failed.Targets)
		}
	}
	if _, err = service.RetryTarget(release.ID, "dev"); err != nil {
		t.Fatal(err)
	}
	retried, err := service.Get(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Targets[0].Status != TargetPending || retried.Targets[1].Status != TargetWaiting {
		t.Fatalf("retry changed later environment readiness: %#v", retried.Targets)
	}
}

func TestProductionGateRequiresExplicitProdEnvironment(t *testing.T) {
	completed := []ReleaseTarget{{TargetInput: TargetInput{ID: "production", Name: "生产环境", Environment: "production"}, Status: TargetSucceeded}}
	if productionSucceeded(completed) {
		t.Fatal("production alias unexpectedly satisfied the explicit prod gate")
	}
	completed[0].Environment = "prod"
	if !productionSucceeded(completed) {
		t.Fatal("explicit prod target did not satisfy the production gate")
	}
	if productionSucceeded([]ReleaseTarget{{TargetInput: TargetInput{ID: "dev", Environment: "dev"}, Status: TargetSucceeded}}) {
		t.Fatal("DEV-only release unexpectedly satisfied the production gate")
	}
}

func TestReleaseProgressUpdatesAreConcurrentSafe(t *testing.T) {
	service := NewService(nil)
	release, _, err := service.Create(context.Background(), CreateInput{ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Transition(release.ID, StatusQueued); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Transition(release.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}

	var waitGroup sync.WaitGroup
	for i := 0; i < 32; i++ {
		waitGroup.Add(1)
		go func(progress int) {
			defer waitGroup.Done()
			if _, updateErr := service.UpdateProgress(release.ID, progress, "deploying", "执行中"); updateErr != nil {
				t.Errorf("concurrent progress update failed: %v", updateErr)
			}
		}(i*3 + 1)
	}
	waitGroup.Wait()

	updated, err := service.Get(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Progress < 1 || updated.Progress > 94 || updated.Stage != "deploying" {
		t.Fatalf("unexpected final concurrent update: %#v", updated)
	}
}

func TestBlueGreenTrafficUsesBlueAndGreenFields(t *testing.T) {
	service := NewService(nil)
	item, _, err := service.Create(context.Background(), CreateInput{
		ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main",
		Strategy: StrategyBlueGreen,
		Traffic:  TrafficSplit{BluePercent: 75, GreenPercent: 25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Plan.Traffic.BluePercent != 75 || item.Plan.Traffic.GreenPercent != 25 || item.Plan.Traffic.StablePercent != 0 || item.Plan.Traffic.CandidatePercent != 0 {
		t.Fatalf("unexpected blue/green traffic: %#v", item.Plan.Traffic)
	}

	// The compatibility path accepts the old stable/candidate field names for
	// blue/green clients that have not upgraded their request payload yet.
	legacy, _, err := service.Create(context.Background(), CreateInput{
		ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main",
		CommitSHAs: []string{"a1b2c3d4e5f6"}, Strategy: StrategyBlueGreen,
		Traffic: TrafficSplit{StablePercent: 60, CandidatePercent: 40},
	})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Plan.Traffic.BluePercent != 60 || legacy.Plan.Traffic.GreenPercent != 40 {
		t.Fatalf("legacy blue/green traffic was not translated: %#v", legacy.Plan.Traffic)
	}
}

func TestReleaseHandlerCreatesAndTransitions(t *testing.T) {
	handler := NewHandler(NewService(nil))
	body := `{"project_id":"reverse-lab","repository_id":"demo-repo","branch":"main","commit_shas":["a1b2c3d4e5f6"]}`
	create := httptest.NewRequest("POST", "/releases", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, create)
	if recorder.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Release Release `json:"release"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	status := httptest.NewRequest("POST", "/releases/"+response.Release.ID+"/status", strings.NewReader(`{"status":"queued"}`))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, status)
	if recorder.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
}
