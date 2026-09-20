package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/deploymentconfig"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type createReleaseRequest struct {
	Branch     string   `json:"branch"`
	CommitSHAs []string `json:"commit_shas"`
	TargetIDs  []string `json:"target_ids"`
	Strategy   string   `json:"strategy"`
	Stable     int      `json:"stable_percent"`
	Candidate  int      `json:"candidate_percent"`
	Blue       int      `json:"blue_percent"`
	Green      int      `json:"green_percent"`
	Name       string   `json:"name"`
	Publish    bool     `json:"publish"`
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
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	items := s.deps.Release.List(project.ID)
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (s *Server) getRelease(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
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

func (s *Server) getReleaseTargetLogs(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
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
	logs, err := s.deps.Release.TargetLogs(item.ID, strings.TrimSpace(c.Param("targetID")))
	if err != nil {
		writeReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": logs, "total": len(logs)})
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
	claims, _ := auth.ClaimsFrom(c)
	createdByName := strings.TrimSpace(claims.Username)
	if user, userErr := s.deps.Store.User(c.Request.Context(), claims.UserID); userErr == nil {
		createdByName = strings.TrimSpace(user.DisplayName)
		if createdByName == "" {
			createdByName = strings.TrimSpace(user.Username)
		}
	}
	created, duplicate, err := s.deps.Release.Create(c.Request.Context(), release.CreateInput{SpaceID: project.SpaceID, ProjectID: project.ID, RepositoryID: project.RepositoryID, CreatedBy: claims.UserID, CreatedByName: createdByName, Branch: branch, Name: strings.TrimSpace(request.Name), CommitSHAs: request.CommitSHAs, Targets: targets, Strategy: release.Strategy(request.Strategy), Traffic: release.TrafficSplit{StablePercent: request.Stable, CandidatePercent: request.Candidate, BluePercent: request.Blue, GreenPercent: request.Green}})
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
	// A release is a version snapshot. Publishing it again must use the
	// commits captured when the release was created; never resolve the source
	// branch here, because the branch may have moved since then.
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
	resolvedConfig, _, targetErr := s.resolveDeploymentConfigForTargetWithContext(c.Request.Context(), project, deploymentTargetFromReleaseTarget(project, target))
	if targetErr != nil {
		writeReleaseError(c, fmt.Errorf("目标 %s 配置校验失败：%w", target.Name, targetErr))
		return
	}
	if !deploymentConfigReady(resolvedConfig) {
		writeReleaseError(c, fmt.Errorf("目标 %s 尚未配置部署配置", target.Name))
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
	go s.runRetriedTarget(project, updated, target)
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
	// A second click while the same release is already queued or running is
	// idempotent. Return before repeating the network-heavy preflight checks.
	if item.Status == release.StatusQueued || item.Status == release.StatusRunning {
		return nil
	}
	if err := git.RequireRepositoryWriteAccess(ctx, s.deps.Git, project.RepositoryID); err != nil {
		return err
	}
	if len(item.Targets) == 0 {
		targets, targetErr := s.resolveReleaseTargetsForContext(ctx, project, nil)
		if targetErr != nil {
			return targetErr
		}
		item, err = s.deps.Release.SetTargets(id, targets)
		if err != nil {
			return err
		}
	}
	if s.deps.ImageBuilder == nil {
		return runtime.ErrImageBuildUnsupported
	}
	if checker, ok := s.deps.ImageBuilder.(imagebuild.PreflightChecker); ok {
		request, requestErr := s.imageBuildRequest(ctx, project, item, firstReleaseCommit(item))
		if requestErr != nil {
			return requestErr
		}
		if err := checker.Preflight(ctx, request); err != nil {
			return err
		}
	}
	// Validate every selected environment before changing the release state. A
	// single project manifest is retargeted to each namespace independently.
	for _, target := range item.Targets {
		// Cluster clients are process-local. Restore the persisted connection
		// before asking Kubernetes for an access review; otherwise a service
		// restart reports a healthy, persisted target as "runtime cluster not
		// found" even though its kubeconfig is available.
		if err := s.ensureRuntimeCluster(ctx, project.SpaceID, target.ClusterID); err != nil {
			return fmt.Errorf("目标 %s 的 Kubernetes 集群连接失败：%w", target.Name, err)
		}
		if err := s.ensureRuntimeNamespace(ctx, project.SpaceID, target.Environment, target.ClusterID, target.Namespace); err != nil {
			return fmt.Errorf("目标 %s 的 Kubernetes namespace 准备失败：%w", target.Name, err)
		}
		if err := s.deps.Runtime.CheckReleaseAccess(ctx, target.ClusterID, target.Namespace); err != nil {
			return fmt.Errorf("目标 %s 的 Kubernetes 发布权限检查失败：%w", target.Name, err)
		}
		resolvedConfig, _, targetErr := s.resolveDeploymentConfigForTargetWithContext(ctx, project, deploymentTargetFromReleaseTarget(project, target))
		if targetErr != nil {
			return fmt.Errorf("目标 %s 配置校验失败：%w", target.Name, targetErr)
		}
		if !deploymentConfigReady(resolvedConfig) {
			return fmt.Errorf("目标 %s 尚未配置部署配置", target.Name)
		}
	}
	switch item.Status {
	case release.StatusDraft, release.StatusFailed, release.StatusSucceeded, release.StatusCancelled:
		if _, err = s.deps.Release.Transition(id, release.StatusQueued); err != nil {
			// Two clicks can race. If another request already queued the same
			// immutable release, both callers should observe the same run.
			latest, getErr := s.deps.Release.Get(id)
			if getErr != nil || (latest.Status != release.StatusQueued && latest.Status != release.StatusRunning) {
				return err
			}
			return nil
		}
	case release.StatusQueued, release.StatusRunning:
		return nil
	default:
		return fmt.Errorf("%w: cannot publish status %s", release.ErrInvalidStatusFlow, item.Status)
	}
	if _, err = s.deps.Release.Transition(id, release.StatusRunning); err != nil {
		latest, getErr := s.deps.Release.Get(id)
		if getErr == nil && latest.Status == release.StatusRunning {
			return nil
		}
		return err
	}
	_, _ = s.deps.Release.UpdateProgress(id, 5, "preparing", "正在准备发布")
	// Refresh after state transitions. A repeat publish deliberately clears its
	// previous artifact, so the execution goroutine must not retain that stale
	// image in the local snapshot captured before the transition.
	if current, getErr := s.deps.Release.Get(id); getErr == nil {
		item = current
	}
	go func(project domain.Project, item release.Release) {
		for _, initialTarget := range item.Targets {
			current, getErr := s.deps.Release.Get(id)
			if getErr != nil {
				return
			}
			target, found := releaseTargetByID(current, initialTarget.ID)
			if !found {
				return
			}
			if targetErr := s.executeReleaseTarget(project, current, target); targetErr != nil {
				_, _ = s.deps.Release.FailTarget(id, target.ID, fmt.Sprintf("部署失败：%v", targetErr))
				_, _ = s.deps.Release.CancelPendingTargets(id, fmt.Sprintf("因环境 %s 发布失败，未继续发布", target.Name))
				_, _ = s.deps.Release.Fail(id, fmt.Sprintf("目标 %s 部署失败：%v", target.Name, targetErr))
				return
			}
		}
		_, _ = s.deps.Release.FinalizeTargets(id)
	}(project, item)
	return nil
}

func (s *Server) runRetriedTarget(project domain.Project, item release.Release, target release.ReleaseTarget) {
	if current, err := s.deps.Release.Get(item.ID); err == nil {
		if refreshed, found := releaseTargetByID(current, target.ID); found {
			item, target = current, refreshed
		}
	}
	if err := s.executeReleaseTarget(project, item, target); err != nil {
		_, _ = s.deps.Release.FailTarget(item.ID, target.ID, fmt.Sprintf("部署失败：%v", err))
		_, _ = s.deps.Release.Fail(item.ID, fmt.Sprintf("目标 %s 重试失败：%v", target.Name, err))
		return
	}
	_, _ = s.deps.Release.FinalizeTargets(item.ID)
}

func (s *Server) executeReleaseTarget(project domain.Project, item release.Release, target release.ReleaseTarget) (err error) {
	if _, err := s.deps.Release.UpdateTargetProgress(item.ID, target.ID, 0, "preparing", "正在准备 "+target.Name); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.appendReleaseLog(item.ID, target.ID, "ttp", "stderr", "ERROR", err.Error())
		}
	}()
	s.appendReleaseLog(item.ID, target.ID, "ttp", "stdout", "INFO", fmt.Sprintf("release target=%s environment=%s cluster=%s namespace=%s", target.ID, target.EnvironmentStage, target.ClusterID, target.Namespace))
	if len(item.Commits) > 0 {
		s.appendReleaseLog(item.ID, target.ID, "git", "stdout", "INFO", fmt.Sprintf("branch=%s commit=%s", item.Branch, strings.TrimSpace(item.Commits[0].SHA)))
	}
	if _, err := s.deps.Release.UpdateTargetProgress(item.ID, target.ID, 15, "preparing", "已读取发布配置"); err != nil {
		return err
	}
	if _, err := s.deps.Release.UpdateTargetProgress(item.ID, target.ID, 25, "building", "正在构建并推送镜像"); err != nil {
		return err
	}
	if err := s.deployRelease(project, item, target); err != nil {
		return err
	}
	if _, err := s.deps.Release.UpdateTargetProgress(item.ID, target.ID, 95, "checking", "Kubernetes 已接受发布，正在确认 rollout"); err != nil {
		return err
	}
	_, err = s.deps.Release.CompleteTarget(item.ID, target.ID)
	if err == nil {
		s.appendReleaseLog(item.ID, target.ID, "ttp", "stdout", "INFO", fmt.Sprintf("release target=%s completed", target.ID))
	}
	return err
}

func (s *Server) appendReleaseLog(id, targetID, source, stream, level, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	_, _ = s.deps.Release.AppendTargetLog(id, targetID, source, stream, level, line)
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
	commitSHA := strings.TrimSpace(item.Commits[0].SHA)
	// Build and rollout are serial operations. Give each provider its own
	// budget instead of cancelling a legitimate BuildKit build when the shorter
	// Kubernetes rollout deadline elapses.
	rolloutTimeout := s.deps.Config.KubeRolloutTimeout
	if rolloutTimeout <= 0 {
		rolloutTimeout = 10 * time.Minute
	}
	buildTimeout := s.deps.Config.ImageBuilderTimeout
	if buildTimeout <= 0 {
		buildTimeout = 15 * time.Minute
	}
	runtimeTimeout := rolloutTimeout + buildTimeout + time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), runtimeTimeout)
	defer cancel()
	config, _, err := s.resolveDeploymentConfigForTargetWithContext(ctx, project, deploymentTargetFromReleaseTarget(project, target))
	if err != nil {
		return err
	}
	if !deploymentConfigReady(config) {
		return fmt.Errorf("目标 %s 尚未配置部署配置", target.Name)
	}
	s.appendReleaseLog(item.ID, target.ID, "k8s", "stdout", "INFO", fmt.Sprintf("manifest format=%s version=%d", config.Format, config.Version))
	if err := s.ensureRuntimeCluster(ctx, project.SpaceID, target.ClusterID); err != nil {
		return err
	}
	if err := s.ensureRuntimeNamespace(ctx, project.SpaceID, target.Environment, target.ClusterID, target.Namespace); err != nil {
		return err
	}
	imagePullCredential, err := s.imagePullCredentialForProject(ctx, project)
	if err != nil {
		return err
	}
	artifact, err := s.artifactForRelease(ctx, project, item, commitSHA)
	s.appendBuildLogs(item.ID, target.ID, buildLogs(artifact, err))
	if err != nil {
		return err
	}
	s.appendReleaseLog(item.ID, target.ID, "build", "stdout", "INFO", fmt.Sprintf("image=%s", artifact.Image))
	return s.deps.Runtime.DeployRelease(ctx, runtime.ReleaseDeployment{
		ClusterID:           target.ClusterID,
		Namespace:           target.Namespace,
		ProjectID:           project.ID,
		TargetID:            target.ID,
		ReleaseID:           item.ID,
		Branch:              item.Branch,
		CommitSHA:           commitSHA,
		Image:               artifact.Image,
		Replicas:            target.Replicas,
		Strategy:            string(item.Plan.Strategy),
		StablePercent:       item.Plan.Traffic.StablePercent,
		CandidatePercent:    item.Plan.Traffic.CandidatePercent,
		BluePercent:         item.Plan.Traffic.BluePercent,
		GreenPercent:        item.Plan.Traffic.GreenPercent,
		Manifest:            config.Manifest,
		ManifestFormat:      config.Format,
		ManifestVersion:     config.Version,
		ResourceFiles:       deploymentResourceContents(config.Files),
		ImagePullCredential: imagePullCredential,
		Log: func(source, stream, level, line string) {
			s.appendReleaseLog(item.ID, target.ID, source, stream, level, line)
		},
	})
}

func (s *Server) imagePullCredentialForProject(ctx context.Context, project domain.Project) (*runtime.ImagePullCredential, error) {
	if strings.TrimSpace(project.RegistryConnectionID) == "" {
		return nil, nil
	}
	connection, err := s.deps.Store.GetImageRegistryConnection(ctx, project.SpaceID, project.RegistryConnectionID)
	if err != nil {
		return nil, fmt.Errorf("项目镜像仓库连接不可用")
	}
	credential, err := s.openImageRegistryCredential(connection)
	if err != nil {
		return nil, fmt.Errorf("项目镜像仓库凭证不可用")
	}
	return &runtime.ImagePullCredential{ConnectionID: connection.ID, Registry: connection.Registry, AuthType: connection.AuthType, Username: credential.Username, Secret: credential.Secret, SecretName: connection.PullSecretName}, nil
}

// artifactForRelease obtains one immutable artifact for a release execution.
// The first environment builds it; later environments reuse the persisted
// digest, never a mutable tag or a second independent build.
func (s *Server) artifactForRelease(ctx context.Context, project domain.Project, item release.Release, commitSHA string) (imagebuild.Result, error) {
	commitSHA = strings.TrimSpace(commitSHA)
	if item.Artifact != nil && strings.EqualFold(strings.TrimSpace(item.Artifact.CommitSHA), commitSHA) && imagebuild.IsDigest(item.Artifact.Digest) && strings.Contains(item.Artifact.Image, "@"+item.Artifact.Digest) {
		return imagebuild.Result{Image: item.Artifact.Image, Digest: item.Artifact.Digest, StartedAt: item.Artifact.BuiltAt, FinishedAt: item.Artifact.BuiltAt}, nil
	}
	result, err := s.buildReleaseImage(ctx, project, item.ID, commitSHA)
	if err != nil {
		return result, err
	}
	if _, err := s.deps.Release.SetArtifact(item.ID, release.Artifact{Image: result.Image, Digest: result.Digest, CommitSHA: commitSHA, BuiltAt: result.FinishedAt}); err != nil {
		return result, err
	}
	return result, nil
}

// buildReleaseImage only forwards the saved source revision, the optional
// project-owned image repository and short-lived source credentials to the
// platform builder. Dockerfile and platform settings remain entirely inside
// that internal service.
func (s *Server) buildReleaseImage(ctx context.Context, project domain.Project, releaseID, commitSHA string) (imagebuild.Result, error) {
	if s == nil || s.deps.ImageBuilder == nil {
		return imagebuild.Result{}, runtime.ErrImageBuildUnsupported
	}
	request, err := s.imageBuildRequest(ctx, project, release.Release{ID: releaseID}, commitSHA)
	if err != nil {
		return imagebuild.Result{}, err
	}
	return s.deps.ImageBuilder.Build(ctx, request)
}

func firstReleaseCommit(item release.Release) string {
	if len(item.Commits) == 0 {
		return ""
	}
	return strings.TrimSpace(item.Commits[0].SHA)
}

func (s *Server) imageBuildRequest(ctx context.Context, project domain.Project, item release.Release, commitSHA string) (imagebuild.Request, error) {
	credential := imagebuild.SourceCredential{}
	gitCredential, credentialErr := s.deps.Store.GetProjectGitCredential(ctx, project.SpaceID, project.ID)
	if credentialErr == nil {
		token, openErr := s.credentialCipher.open(gitCredential.TokenCiphertext)
		if openErr != nil {
			return imagebuild.Request{}, fmt.Errorf("项目仓库机器人凭证不可用")
		}
		credential = imagebuild.SourceCredential{Username: gitCredential.Username, Token: token}
	} else if !errors.Is(credentialErr, store.ErrNotFound) {
		return imagebuild.Request{}, credentialErr
	}
	request := imagebuild.Request{
		ProjectID: project.ID, ReleaseID: item.ID, RepositoryURL: project.RepositoryURL, CommitSHA: commitSHA,
		SourceCredential: credential, ImageRepository: strings.TrimSpace(project.ImageRepository),
	}
	if strings.TrimSpace(project.RegistryConnectionID) != "" {
		connection, connectionErr := s.deps.Store.GetImageRegistryConnection(ctx, project.SpaceID, project.RegistryConnectionID)
		if connectionErr != nil {
			return imagebuild.Request{}, fmt.Errorf("项目镜像仓库连接不可用")
		}
		registryCredential, openErr := s.openImageRegistryCredential(connection)
		if openErr != nil {
			return imagebuild.Request{}, fmt.Errorf("项目镜像仓库凭证不可用")
		}
		request.RegistryCredential = imagebuild.PushCredential{
			ConnectionID: connection.ID,
			Registry:     connection.Registry,
			AuthType:     connection.AuthType,
			Username:     registryCredential.Username,
			Secret:       registryCredential.Secret,
		}
	}
	return request, nil
}

func (s *Server) appendBuildLogs(releaseID, targetID string, logs []imagebuild.LogEntry) {
	for _, entry := range logs {
		stream := strings.ToLower(strings.TrimSpace(entry.Stream))
		if stream != "stderr" {
			stream = "stdout"
		}
		level := strings.ToUpper(strings.TrimSpace(entry.Level))
		if level == "" {
			level = "INFO"
		}
		s.appendReleaseLog(releaseID, targetID, "build", stream, level, entry.Line)
	}
}

// buildLogs keeps the build worker's sanitized output when a build fails. A
// remote worker returns it on imagebuild.Failure while a successful result
// carries it directly.
func buildLogs(result imagebuild.Result, err error) []imagebuild.LogEntry {
	if len(result.Logs) > 0 {
		return result.Logs
	}
	var failure *imagebuild.Failure
	if errors.As(err, &failure) {
		return failure.Logs
	}
	return nil
}

func releaseTargetByID(item release.Release, targetID string) (release.ReleaseTarget, bool) {
	for _, target := range item.Targets {
		if target.ID == targetID {
			return target, true
		}
	}
	return release.ReleaseTarget{}, false
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
		// Keep older API clients working by using the first enabled target in
		// explicit order. The console always sends its selected target IDs.
		for _, item := range items {
			if item.Enabled {
				selected = append(selected, item)
				break
			}
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
		return nil, fmt.Errorf("%w: 项目没有可用的发布环境；部署配置只定义资源文件，请先在“发布环境”中添加并启用 DEV 环境", store.ErrInvalidInput)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].SortOrder < selected[j].SortOrder
	})
	hasDev := false
	result := make([]release.TargetInput, 0, len(selected))
	for _, item := range selected {
		if !item.Enabled {
			return nil, fmt.Errorf("%w: 部署目标 %s 已停用", store.ErrConflict, item.Name)
		}
		if item.Stage == store.DeploymentStageDev {
			hasDev = true
		}
		result = append(result, release.TargetInput{ID: item.ID, Name: item.Name, Environment: item.Environment, EnvironmentStage: item.Stage, SortOrder: item.SortOrder, ClusterID: item.ClusterID, Namespace: item.Namespace, Replicas: item.Replicas, ContainerPort: item.ContainerPort, DeployStrategy: item.DeployStrategy})
	}
	if !hasDev || result[0].EnvironmentStage != store.DeploymentStageDev {
		return nil, fmt.Errorf("%w: 发布必须从 DEV 环境开始", store.ErrInvalidInput)
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
	case errors.Is(err, runtime.ErrImageBuildUnsupported):
		status, code = http.StatusNotImplemented, "image_build_unsupported"
	case errors.Is(err, runtime.ErrReleaseAccessDenied):
		status, code = http.StatusForbidden, "kubernetes_release_access_denied"
	case errors.Is(err, imagebuild.ErrUnauthorized):
		status, code = http.StatusBadGateway, "image_builder_unauthorized"
	case errors.Is(err, imagebuild.ErrRegistryPreflightFailed):
		status, code = http.StatusBadGateway, "image_registry_preflight_failed"
	case errors.Is(err, runtime.ErrProviderNotConfigured):
		status, code = http.StatusServiceUnavailable, "runtime_provider_not_configured"
	case strings.Contains(err.Error(), "尚未配置部署配置"):
		status, code = http.StatusPreconditionRequired, "deployment_config_not_configured"
	case errors.Is(err, git.ErrProviderHTTP), errors.Is(err, git.ErrProviderResponse):
		status, code = http.StatusBadGateway, "provider_error"
	case errors.Is(err, release.ErrInvalidRelease), errors.Is(err, release.ErrInvalidStatus):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, release.ErrInvalidStatusFlow), errors.Is(err, release.ErrReleaseImmutable), errors.Is(err, release.ErrLastCommit), errors.Is(err, release.ErrTargetRetry):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, release.ErrReleaseNotFound):
		status, code = http.StatusNotFound, "not_found"
	}
	writeError(c, status, code, err.Error())
}
