package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/deploymentconfig"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type createReleaseRequest struct {
	Branch       string   `json:"branch"`
	CommitSHAs   []string `json:"commit_shas"`
	TargetIDs    []string `json:"target_ids"`
	Strategy     string   `json:"strategy"`
	Stable       int      `json:"stable_percent"`
	Candidate    int      `json:"candidate_percent"`
	Blue         int      `json:"blue_percent"`
	Green        int      `json:"green_percent"`
	Name         string   `json:"name"`
	SourceBranch string   `json:"source_branch"`
	BaseBranch   string   `json:"base_branch"`
	Publish      bool     `json:"publish"`
}

type releaseProgressRequest struct {
	Progress int    `json:"progress"`
	Stage    string `json:"stage"`
	Message  string `json:"message"`
}

type releaseCancelRequest struct {
	Message string `json:"message"`
}

type releaseFailRequest struct {
	Error string `json:"error"`
}

func (s *Server) listReleases(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	items := s.deps.Release.List(project.ID)
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (s *Server) listReleaseBatches(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	items := s.deps.Release.ListBatches(project.ID)
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (s *Server) mergeReleaseToMain(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Release.Get(c.Param("releaseID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if item.ProjectID != project.ID {
		writeError(c, http.StatusNotFound, "not_found", "发布记录不存在")
		return
	}
	if err := git.RequireRepositoryMergeAccess(c.Request.Context(), s.deps.Git, project.RepositoryID); err != nil {
		writeReleaseError(c, err)
		return
	}
	updated, batch, err := s.deps.Release.MergeToMain(c.Request.Context(), item.ID)
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	s.recordAudit(c, "发布项合入 main", project.Name+" · "+updated.ID)
	c.JSON(http.StatusOK, gin.H{"release": updated, "batch": batch})
}

func (s *Server) closeReleaseBatch(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	batch, err := s.deps.Release.CloseBatch(project.ID, c.Param("batchID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	s.recordAudit(c, "关闭发布批次", project.Name+" · "+batch.ID)
	c.JSON(http.StatusOK, gin.H{"batch": batch})
}

func (s *Server) getRelease(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Release.Get(c.Param("releaseID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if item.ProjectID != project.ID {
		writeError(c, http.StatusNotFound, "not_found", "发布记录不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"release": item})
}

func (s *Server) createRelease(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	var request createReleaseRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "发布配置格式不正确")
		return
	}
	branch := strings.TrimSpace(request.Branch)
	if branch == "" {
		branch = project.DefaultBranch
	}
	targets, err := s.resolveReleaseTargets(c, project, request.TargetIDs)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if err := ensureReleaseStartsAtDev(targets); err != nil {
		writeReleaseError(c, err)
		return
	}
	ownerID := uint64(0)
	ownerName := ""
	if claims, claimsOK := auth.ClaimsFrom(c); claimsOK {
		ownerID = claims.UserID
		if user, userErr := s.deps.Store.User(c.Request.Context(), claims.UserID); userErr == nil {
			ownerName = firstNonEmptyAPI(user.DisplayName, user.Username)
		}
	}
	baseBranch := strings.TrimSpace(request.BaseBranch)
	if baseBranch == "" {
		baseBranch = project.DefaultBranch
	}
	created, duplicate, err := s.deps.Release.Create(c.Request.Context(), release.CreateInput{SpaceID: project.SpaceID, ProjectID: project.ID, RepositoryID: project.RepositoryID, Branch: branch, SourceBranch: firstNonEmptyAPI(request.SourceBranch, branch), BaseBranch: baseBranch, Name: strings.TrimSpace(request.Name), OwnerID: ownerID, OwnerName: ownerName, CommitSHAs: request.CommitSHAs, Targets: targets, Strategy: release.Strategy(request.Strategy), Traffic: release.TrafficSplit{StablePercent: request.Stable, CandidatePercent: request.Candidate, BluePercent: request.Blue, GreenPercent: request.Green}})
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if request.Publish {
		if err := s.startPublish(c.Request.Context(), project, created.ID); err != nil {
			writeReleaseError(c, err)
			return
		}
		// startPublish advances the release synchronously to running. Return the
		// current snapshot so the console does not briefly show a published
		// release as a draft until its next refresh.
		if updated, getErr := s.deps.Release.Get(created.ID); getErr == nil {
			created = updated
		}
	}
	action := "创建发布草稿"
	if request.Publish {
		action = "开始发布"
	}
	s.recordAudit(c, action, project.Name+" · "+created.Branch)
	c.JSON(func() int {
		if duplicate {
			return http.StatusOK
		}
		return http.StatusCreated
	}(), gin.H{"release": created, "duplicate": duplicate})
}

func (s *Server) publishRelease(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Release.Get(c.Param("releaseID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if item.ProjectID != project.ID {
		writeError(c, http.StatusNotFound, "not_found", "发布记录不存在")
		return
	}
	if err := s.startPublish(c.Request.Context(), project, item.ID); err != nil {
		writeReleaseError(c, err)
		return
	}
	updated, err := s.deps.Release.Get(item.ID)
	if err == nil {
		item = updated
	}
	s.recordAudit(c, "再次发布", project.Name+" · "+item.Branch)
	c.JSON(http.StatusAccepted, gin.H{"release": item})
}

// publishReleaseTarget advances exactly one environment. The regular publish
// endpoint uses the same single-target worker; this endpoint is the explicit
// button for moving from DEV to UAT to PRE to PROD.
func (s *Server) publishReleaseTarget(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Release.Get(c.Param("releaseID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if item.ProjectID != project.ID {
		writeError(c, http.StatusNotFound, "not_found", "发布记录不存在")
		return
	}

	// Releases created before deployment targets were introduced are completed
	// with the project's default target on their first publish.
	if len(item.Targets) == 0 {
		targets, targetErr := s.resolveReleaseTargetsForContext(c.Request.Context(), project, nil)
		if targetErr != nil {
			writeStoreError(c, targetErr)
			return
		}
		if targetErr := ensureReleaseStartsAtDev(targets); targetErr != nil {
			writeReleaseError(c, targetErr)
			return
		}
		item, err = s.deps.Release.SetTargets(item.ID, targets)
		if err != nil {
			writeReleaseError(c, err)
			return
		}
	}
	if targetErr := ensureReleaseStartsAtDevInputs(item.Targets); targetErr != nil {
		writeReleaseError(c, targetErr)
		return
	}
	targetID := strings.TrimSpace(c.Param("targetID"))
	target, found := releaseTargetByID(item.Targets, targetID)
	if !found {
		writeError(c, http.StatusNotFound, "not_found", "发布环境不存在")
		return
	}
	if err := git.RequireRepositoryWriteAccess(c.Request.Context(), s.deps.Git, project.RepositoryID); err != nil {
		writeReleaseError(c, err)
		return
	}
	if _, _, targetErr := s.resolveDeploymentConfigForTargetWithContext(c.Request.Context(), project, deploymentTargetFromReleaseTarget(project, target)); targetErr != nil {
		writeReleaseError(c, fmt.Errorf("目标 %s 配置校验失败：%w", target.Name, targetErr))
		return
	}
	if item.BatchID == "" {
		item, _, err = s.deps.Release.AttachToBatch(c.Request.Context(), item.ID, firstNonEmptyAPI(item.BaseBranch, project.DefaultBranch))
		if err != nil {
			writeReleaseError(c, err)
			return
		}
		target, found = releaseTargetByID(item.Targets, targetID)
		if !found {
			writeError(c, http.StatusNotFound, "not_found", "发布环境不存在")
			return
		}
	}

	updated, started, err := s.deps.Release.StartTarget(item.ID, targetID)
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if target, found = releaseTargetByID(updated.Targets, targetID); !found {
		writeError(c, http.StatusNotFound, "not_found", "发布环境不存在")
		return
	}
	if started {
		go s.runReleaseTarget(project, updated, target)
	}
	s.recordAudit(c, "开始环境发布", project.Name+" · "+target.Name)
	c.JSON(http.StatusAccepted, gin.H{"release": updated, "target_id": targetID})
}

func (s *Server) retryReleaseTarget(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Release.Get(c.Param("releaseID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if item.ProjectID != project.ID {
		writeError(c, http.StatusNotFound, "not_found", "发布记录不存在")
		return
	}
	targetID := strings.TrimSpace(c.Param("targetID"))
	var target release.ReleaseTarget
	for _, candidate := range item.Targets {
		if candidate.ID == targetID {
			target = candidate
			break
		}
	}
	if target.ID == "" {
		writeError(c, http.StatusNotFound, "not_found", "发布环境不存在")
		return
	}
	if err := git.RequireRepositoryWriteAccess(c.Request.Context(), s.deps.Git, project.RepositoryID); err != nil {
		writeReleaseError(c, err)
		return
	}
	if _, _, targetErr := s.resolveDeploymentConfigForTargetWithContext(c.Request.Context(), project, deploymentTargetFromReleaseTarget(project, target)); targetErr != nil {
		writeReleaseError(c, fmt.Errorf("目标 %s 配置校验失败：%w", target.Name, targetErr))
		return
	}
	updated, err := s.deps.Release.RetryTarget(item.ID, targetID)
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	for _, candidate := range updated.Targets {
		if candidate.ID == targetID {
			target = candidate
			break
		}
	}
	go s.runReleaseTarget(project, updated, target)
	s.recordAudit(c, "重试环境发布", project.Name+" · "+target.Name)
	c.JSON(http.StatusAccepted, gin.H{"release": updated, "target_id": targetID})
}

func (s *Server) updateReleaseProgress(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Release.Get(c.Param("releaseID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if item.ProjectID != project.ID {
		writeError(c, http.StatusNotFound, "not_found", "发布记录不存在")
		return
	}
	var request releaseProgressRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "发布进度格式不正确")
		return
	}
	updated, err := s.deps.Release.UpdateProgress(item.ID, request.Progress, request.Stage, request.Message)
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"release": updated})
}

func (s *Server) cancelRelease(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Release.Get(c.Param("releaseID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if item.ProjectID != project.ID {
		writeError(c, http.StatusNotFound, "not_found", "发布记录不存在")
		return
	}
	var request releaseCancelRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
			writeError(c, http.StatusBadRequest, "invalid_request", "取消原因格式不正确")
			return
		}
	}
	message := strings.TrimSpace(request.Message)
	if message == "" {
		message = "用户取消发布"
	}
	updated, err := s.deps.Release.Cancel(item.ID, message)
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	s.recordAudit(c, "取消发布", project.Name+" · "+updated.Branch)
	c.JSON(http.StatusOK, gin.H{"release": updated})
}

func (s *Server) failRelease(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Release.Get(c.Param("releaseID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if item.ProjectID != project.ID {
		writeError(c, http.StatusNotFound, "not_found", "发布记录不存在")
		return
	}
	var request releaseFailRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "失败原因格式不正确")
		return
	}
	updated, err := s.deps.Release.Fail(item.ID, request.Error)
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	s.recordAudit(c, "发布失败", project.Name+" · "+updated.Branch)
	c.JSON(http.StatusOK, gin.H{"release": updated})
}

func (s *Server) removeReleaseCommit(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Release.Get(c.Param("releaseID"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	if item.ProjectID != project.ID {
		writeError(c, http.StatusNotFound, "not_found", "发布记录不存在")
		return
	}
	updated, err := s.deps.Release.RemoveCommit(item.ID, c.Param("sha"))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	s.recordAudit(c, "从草稿移除 commit", project.Name+" · "+c.Param("sha"))
	c.JSON(http.StatusOK, updated)
}

func (s *Server) startPublish(ctx context.Context, project domain.Project, id string) error {
	item, err := s.deps.Release.Get(id)
	if err != nil {
		return err
	}
	if err := git.RequireRepositoryWriteAccess(ctx, s.deps.Git, project.RepositoryID); err != nil {
		return err
	}
	if len(item.Targets) == 0 {
		targets, targetErr := s.resolveReleaseTargetsForContext(ctx, project, nil)
		if targetErr != nil {
			return targetErr
		}
		if targetErr := ensureReleaseStartsAtDev(targets); targetErr != nil {
			return targetErr
		}
		item, err = s.deps.Release.SetTargets(id, targets)
		if err != nil {
			return err
		}
	}
	if targetErr := ensureReleaseStartsAtDevInputs(item.Targets); targetErr != nil {
		return targetErr
	}
	terminal := item.Status == release.StatusFailed || item.Status == release.StatusSucceeded || item.Status == release.StatusCancelled
	if item.BatchID == "" || terminal {
		item, _, err = s.deps.Release.AttachToBatch(ctx, id, firstNonEmptyAPI(item.BaseBranch, project.DefaultBranch))
		if err != nil {
			return err
		}
	}
	if len(item.Targets) == 0 {
		return fmt.Errorf("%w: 没有可发布的环境部署目标", release.ErrInvalidRelease)
	}

	// A normal publish starts the first unfinished environment only. Later
	// environments stay pending until the operator explicitly advances them.
	// This is deliberately decided on the server so an old client cannot
	// accidentally turn a four-step release into an automatic rollout.
	if item.Status == release.StatusRunning {
		return nil
	}
	var target release.ReleaseTarget
	var found bool
	if terminal {
		// Validate the first environment before changing the terminal record. A
		// repeat publish is a full rerun from DEV; a targeted retry remains the
		// way to rerun only one failed environment.
		target, found = firstReleaseTarget(item.Targets)
	} else {
		target, found = firstUnfinishedReleaseTarget(item.Targets)
	}
	if !found {
		return fmt.Errorf("%w: 所有环境都已完成，请使用再次发布", release.ErrInvalidStatusFlow)
	}
	if _, _, targetErr := s.resolveDeploymentConfigForTargetWithContext(ctx, project, deploymentTargetFromReleaseTarget(project, target)); targetErr != nil {
		return fmt.Errorf("目标 %s 配置校验失败：%w", target.Name, targetErr)
	}
	if terminal {
		// Transition performs the atomic reset for failed, succeeded, and
		// cancelled records, including restoring DEV to pending and clearing
		// the previous run's execution metadata.
		item, err = s.deps.Release.Transition(id, release.StatusQueued)
		if err != nil {
			return err
		}
		target, found = firstUnfinishedReleaseTarget(item.Targets)
		if !found {
			return fmt.Errorf("%w: 重复发布没有可用的环境部署目标", release.ErrInvalidRelease)
		}
	}
	switch item.Status {
	case release.StatusDraft, release.StatusQueued:
		// A caller may have queued the release explicitly. Continue into the
		// same single-target worker instead of leaving it queued forever.
	default:
		return fmt.Errorf("%w: cannot publish status %s", release.ErrInvalidStatusFlow, item.Status)
	}
	startedItem, started, err := s.deps.Release.StartTarget(id, target.ID)
	if err != nil {
		return err
	}
	if !started {
		// A duplicate click may observe the target already owned by another
		// executor. Do not start a second worker.
		return nil
	}
	target, found = releaseTargetByID(startedItem.Targets, target.ID)
	if !found {
		return fmt.Errorf("%w: 环境部署目标在发布过程中消失", release.ErrInvalidRelease)
	}
	go s.runReleaseTarget(project, startedItem, target)
	return nil
}

func firstUnfinishedReleaseTarget(targets []release.ReleaseTarget) (release.ReleaseTarget, bool) {
	var selected release.ReleaseTarget
	selectedRank := 0
	found := false
	for _, target := range targets {
		if target.Status == release.TargetSucceeded {
			continue
		}
		rank := releaseEnvironmentRank(target)
		if !found || rank < selectedRank {
			selected = target
			selectedRank = rank
			found = true
		}
	}
	return selected, found
}

func firstReleaseTarget(targets []release.ReleaseTarget) (release.ReleaseTarget, bool) {
	if len(targets) == 0 {
		return release.ReleaseTarget{}, false
	}
	// Service.Create and SetTargets keep this slice in environment order. A
	// small defensive scan also keeps legacy records safe if they were loaded
	// from an older persistence implementation.
	best := targets[0]
	bestRank := releaseEnvironmentRank(best)
	for _, target := range targets[1:] {
		if rank := releaseEnvironmentRank(target); rank < bestRank {
			best, bestRank = target, rank
		}
	}
	return best, true
}

func ensureReleaseStartsAtDev(targets []release.TargetInput) error {
	return ensureReleaseStartsAtDevInputsFromInputs(targets)
}

func ensureReleaseStartsAtDevInputs(targets []release.ReleaseTarget) error {
	inputs := make([]release.TargetInput, 0, len(targets))
	for _, target := range targets {
		inputs = append(inputs, target.TargetInput)
	}
	return ensureReleaseStartsAtDevInputsFromInputs(inputs)
}

func ensureReleaseStartsAtDevInputsFromInputs(targets []release.TargetInput) error {
	for _, target := range targets {
		if releaseEnvironmentRankInput(target) == 1 {
			return nil
		}
	}
	return fmt.Errorf("%w: 发布必须包含 DEV 环境，所有新版本都从 DEV 开始", release.ErrInvalidRelease)
}

func releaseEnvironmentRank(target release.ReleaseTarget) int {
	return releaseEnvironmentRankInput(target.TargetInput)
}

func releaseEnvironmentRankInput(target release.TargetInput) int {
	if target.SortOrder > 0 {
		return target.SortOrder
	}
	switch strings.ToLower(strings.TrimSpace(target.EnvironmentStage)) {
	case "dev":
		return 1
	case "uat":
		return 2
	case "pre":
		return 3
	case "prod":
		return 4
	}
	environment := strings.ToLower(strings.TrimSpace(target.Environment))
	switch environment {
	case "dev", "develop", "development":
		return 1
	case "uat", "qa", "test", "testing":
		return 2
	case "pre", "preprod", "pre-production", "stage", "staging":
		return 3
	case "prod", "pro", "production":
		return 4
	}
	value := strings.ToLower(strings.Join([]string{target.ID, target.Name}, " "))
	switch {
	case strings.Contains(value, "开发") || strings.Contains(value, "dev"):
		return 1
	case strings.Contains(value, "测试") || strings.Contains(value, "uat") || strings.Contains(value, "qa"):
		return 2
	case strings.Contains(value, "预发布") || strings.Contains(value, "staging") || strings.Contains(value, "stage") || strings.Contains(value, "pre"):
		return 3
	case strings.Contains(value, "生产") || strings.Contains(value, "prod"):
		return 4
	default:
		return 5
	}
}

func firstNonEmptyAPI(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func releaseTargetByID(targets []release.ReleaseTarget, targetID string) (release.ReleaseTarget, bool) {
	targetID = strings.TrimSpace(targetID)
	for _, target := range targets {
		if target.ID == targetID {
			return target, true
		}
	}
	return release.ReleaseTarget{}, false
}

func (s *Server) runReleaseTarget(project domain.Project, item release.Release, target release.ReleaseTarget) {
	if err := s.executeReleaseTarget(project, item, target); err != nil {
		_, _ = s.deps.Release.FailTarget(item.ID, target.ID, fmt.Sprintf("部署失败：%v", err))
		_, _ = s.deps.Release.Fail(item.ID, fmt.Sprintf("目标 %s 重试失败：%v", target.Name, err))
		return
	}
	_, _ = s.deps.Release.FinalizeTargets(item.ID)
}

func (s *Server) executeReleaseTarget(project domain.Project, item release.Release, target release.ReleaseTarget) error {
	steps := []struct {
		progress int
		stage    string
		message  string
	}{
		{20, "building", "正在构建镜像"},
		{40, "testing", "正在确认代码版本"},
		{60, "pushing", "正在推送镜像"},
		{82, "deploying", "正在更新 Kubernetes 部署"},
		{95, "checking", "正在检查 Pod 健康状态"},
	}
	if _, err := s.deps.Release.UpdateTargetProgress(item.ID, target.ID, 0, "preparing", "正在准备 "+target.Name); err != nil {
		return err
	}
	for _, step := range steps {
		time.Sleep(700 * time.Millisecond)
		if _, err := s.deps.Release.UpdateTargetProgress(item.ID, target.ID, step.progress, step.stage, step.message+" · "+target.Name); err != nil {
			return err
		}
		if step.stage == "deploying" {
			if err := s.deployRelease(project, item, target); err != nil {
				return err
			}
		}
	}
	_, err := s.deps.Release.CompleteTarget(item.ID, target.ID)
	return err
}

func deploymentTargetFromReleaseTarget(project domain.Project, target release.ReleaseTarget) domain.DeploymentTarget {
	return domain.DeploymentTarget{
		ID: target.ID, ProjectID: project.ID, SpaceID: project.SpaceID,
		Name: target.Name, Environment: target.Environment, Stage: target.EnvironmentStage, SortOrder: target.SortOrder, ClusterID: target.ClusterID,
		Namespace: target.Namespace, Replicas: target.Replicas, ContainerPort: target.ContainerPort,
		DeployStrategy: target.DeployStrategy,
	}
}

func (s *Server) deployRelease(project domain.Project, item release.Release, target release.ReleaseTarget) error {
	if len(item.Commits) == 0 || strings.TrimSpace(item.Commits[0].SHA) == "" {
		return fmt.Errorf("发布版本没有可用的 commit")
	}
	// Once a release joins a batch, the batch integration returns an immutable
	// snapshot commit for this release item. Build and deploy that snapshot so
	// a later developer joining the shared batch cannot silently change a
	// version that is already being tested.
	commitSHA := strings.TrimSpace(item.BatchSnapshotSHA)
	if commitSHA == "" {
		commitSHA = strings.TrimSpace(item.Commits[0].SHA)
	}
	shortSHA := commitSHA
	if len(shortSHA) > 10 {
		shortSHA = shortSHA[:10]
	}
	image := fmt.Sprintf("example.invalid/%s:%s", strings.TrimSpace(project.ID), shortSHA)
	// A real image pull and Deployment rollout can legitimately take several
	// minutes. Keep the worker deadline just beyond the provider's configured
	// rollout deadline so the provider can return its useful status summary.
	runtimeTimeout := s.deps.Config.KubeRolloutTimeout
	if runtimeTimeout <= 0 {
		runtimeTimeout = 10 * time.Minute
	} else {
		runtimeTimeout += time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeTimeout)
	defer cancel()
	config, _, err := s.resolveDeploymentConfigForTargetWithContext(ctx, project, deploymentTargetFromReleaseTarget(project, target))
	if err != nil {
		return err
	}
	if err := s.ensureRuntimeCluster(ctx, project.SpaceID, target.ClusterID); err != nil {
		return err
	}
	return s.deps.Runtime.DeployRelease(ctx, runtime.ReleaseDeployment{
		ClusterID:        target.ClusterID,
		Namespace:        target.Namespace,
		ProjectID:        project.ID,
		ReleaseID:        item.ID,
		Branch:           firstNonEmptyAPI(item.SourceBranch, item.Branch),
		CommitSHA:        commitSHA,
		Image:            image,
		Replicas:         target.Replicas,
		Strategy:         string(item.Plan.Strategy),
		StablePercent:    item.Plan.Traffic.StablePercent,
		CandidatePercent: item.Plan.Traffic.CandidatePercent,
		BluePercent:      item.Plan.Traffic.BluePercent,
		GreenPercent:     item.Plan.Traffic.GreenPercent,
		Manifest:         config.Manifest,
		ManifestFormat:   config.Format,
		ManifestVersion:  config.Version,
	})
}

func (s *Server) resolveReleaseTargets(c *gin.Context, project domain.Project, requested []string) ([]release.TargetInput, error) {
	return s.resolveReleaseTargetsForContext(c.Request.Context(), project, requested)
}

func (s *Server) resolveReleaseTargetsForContext(ctx context.Context, project domain.Project, requested []string) ([]release.TargetInput, error) {
	items, err := s.deps.Store.ListDeploymentTargets(ctx, project.SpaceID, project.ID)
	if err != nil {
		return nil, err
	}
	selected := make([]domain.DeploymentTarget, 0)
	seen := make(map[string]struct{})
	if len(requested) == 0 {
		if len(items) > 0 {
			selected = append(selected, items[0])
		}
	} else {
		byID := make(map[string]domain.DeploymentTarget, len(items))
		for _, item := range items {
			byID[item.ID] = item
		}
		for _, rawID := range requested {
			id := strings.TrimSpace(rawID)
			if id == "" {
				continue
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			item, exists := byID[id]
			if !exists {
				return nil, store.ErrNotFound
			}
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w: 项目没有可用的部署目标", store.ErrInvalidInput)
	}
	result := make([]release.TargetInput, 0, len(selected))
	for _, item := range selected {
		if !item.Enabled {
			return nil, fmt.Errorf("%w: 部署目标 %s 已停用", store.ErrConflict, item.Name)
		}
		result = append(result, release.TargetInput{ID: item.ID, Name: item.Name, Environment: item.Environment, EnvironmentStage: item.Stage, SortOrder: item.SortOrder, ClusterID: item.ClusterID, Namespace: item.Namespace, Replicas: item.Replicas, ContainerPort: item.ContainerPort, DeployStrategy: item.DeployStrategy})
	}
	return result, nil
}

func writeReleaseError(c *gin.Context, err error) {
	if errors.Is(err, git.ErrGitWriteAccessDenied) {
		writeGitWriteAccessDenied(c, err)
		return
	}
	if errors.Is(err, git.ErrGitAccessCheckFailed) || errors.Is(err, git.ErrGitAccessCheckUnsupported) {
		writeGitAccessError(c, err)
		return
	}
	status, code := http.StatusInternalServerError, "internal_error"
	switch {
	case errors.Is(err, deploymentconfig.ErrInvalidManifest):
		status, code = http.StatusBadRequest, "invalid_manifest"
	case errors.Is(err, git.ErrRepositoryNotFound), errors.Is(err, git.ErrBranchNotFound), errors.Is(err, git.ErrCommitNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusGatewayTimeout, "provider_timeout"
	case errors.Is(err, runtime.ErrDeploymentRolloutTimeout):
		status, code = http.StatusGatewayTimeout, "rollout_timeout"
	case errors.Is(err, runtime.ErrDeploymentRolloutFailed):
		status, code = http.StatusBadGateway, "rollout_failed"
	case errors.Is(err, runtime.ErrReleaseDeploymentUnsupported):
		status, code = http.StatusNotImplemented, "deployment_unsupported"
	case errors.Is(err, git.ErrProviderHTTP), errors.Is(err, git.ErrProviderResponse):
		status, code = http.StatusBadGateway, "provider_error"
	case errors.Is(err, release.ErrInvalidRelease), errors.Is(err, release.ErrInvalidStatus):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, release.ErrInvalidStatusFlow), errors.Is(err, release.ErrReleaseImmutable), errors.Is(err, release.ErrLastCommit), errors.Is(err, release.ErrTargetRetry), errors.Is(err, release.ErrTargetNotReady):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, release.ErrBatchConflict), errors.Is(err, release.ErrBatchClosed), errors.Is(err, release.ErrMainMergeNotReady):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, release.ErrReleaseNotFound), errors.Is(err, release.ErrBatchNotFound):
		status, code = http.StatusNotFound, "not_found"
	}
	writeError(c, status, code, err.Error())
}
