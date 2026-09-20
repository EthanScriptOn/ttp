package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type createABExperimentRequest struct {
	Name        string               `json:"name"`
	TargetID    string               `json:"target_id"`
	AReleaseID  string               `json:"a_release_id"`
	BReleaseID  string               `json:"b_release_id" binding:"required"`
	Assignment  string               `json:"assignment"`
	RoutingRule domain.ABRoutingRule `json:"routing_rule"`
	ATraffic    int                  `json:"a_traffic"`
	BTraffic    int                  `json:"b_traffic"`
}

type updateABTrafficRequest struct {
	ATraffic int `json:"a_traffic"`
	BTraffic int `json:"b_traffic"`
}

type finishABExperimentRequest struct {
	Result string `json:"result" binding:"required"`
}

func (s *Server) listABExperiments(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	items, err := s.deps.Store.ListABExperiments(c.Request.Context(), project.SpaceID, project.ID)
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	for index := range items {
		items[index] = s.withABRuntime(c.Request.Context(), items[index])
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (s *Server) getABExperiment(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Store.GetABExperiment(c.Request.Context(), project.SpaceID, project.ID, c.Param("experimentID"))
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"experiment": s.withABRuntime(c.Request.Context(), item)})
}

func (s *Server) createABExperiment(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	if !s.deps.Runtime.SupportsABExperiment() {
		writeExperimentError(c, runtime.ErrReleaseDeploymentUnsupported)
		return
	}
	var request createABExperimentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "A/B 实验配置格式不正确")
		return
	}
	targetID := strings.TrimSpace(request.TargetID)
	if targetID == "" {
		writeError(c, http.StatusBadRequest, "invalid_request", "请选择实验环境")
		return
	}
	target, ok := s.findProjectTarget(c.Request.Context(), project, targetID)
	if !ok {
		writeError(c, http.StatusNotFound, "not_found", "实验环境不存在")
		return
	}
	if !target.Enabled || target.Status != "active" {
		writeError(c, http.StatusConflict, "conflict", "实验环境当前不可用")
		return
	}
	if err := s.ensureRuntimeCluster(c.Request.Context(), project.SpaceID, target.ClusterID); err != nil {
		writeRuntimeError(c, err)
		return
	}
	if err := s.ensureRuntimeNamespace(c.Request.Context(), project.SpaceID, target.Environment, target.ClusterID, target.Namespace); err != nil {
		writeRuntimeError(c, err)
		return
	}
	aVersion, err := s.releaseVersionForTarget(project, target, request.AReleaseID, true)
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	aVersion.Role = "A"
	bVersion, err := s.releaseVersionForTarget(project, target, request.BReleaseID, false)
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	bVersion.Role = "B"
	if aVersion.ReleaseID != "" && strings.EqualFold(aVersion.ReleaseID, bVersion.ReleaseID) {
		writeError(c, http.StatusBadRequest, "invalid_request", "A、B 不能使用同一个发布单")
		return
	}
	aImage, imageErr := s.imageForABVersion(c.Request.Context(), project, aVersion)
	if imageErr != nil {
		writeExperimentError(c, imageErr)
		return
	}
	bImage, imageErr := s.imageForABVersion(c.Request.Context(), project, bVersion)
	if imageErr != nil {
		writeExperimentError(c, imageErr)
		return
	}
	aTraffic, bTraffic := request.ATraffic, request.BTraffic
	if aTraffic == 0 && bTraffic == 0 {
		aTraffic, bTraffic = 99, 1
	}
	assignment := strings.TrimSpace(request.Assignment)
	if assignment == "" {
		assignment = "percentage"
	}
	if assignment == "json_field" {
		assignment = "user_id"
	}
	created, err := s.deps.Store.CreateABExperiment(c.Request.Context(), project.SpaceID, project.ID, store.CreateABExperimentInput{
		Name: request.Name, TargetID: target.ID, Environment: target.Environment, EnvironmentStage: target.Stage, ClusterID: target.ClusterID, Namespace: target.Namespace,
		Replicas: target.Replicas, Strategy: target.DeployStrategy, Assignment: assignment, RoutingRule: request.RoutingRule, AVersion: aVersion, BVersion: bVersion, ATraffic: aTraffic, BTraffic: bTraffic,
		CreatedBy: currentUserID(c),
	})
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	if err := s.deps.Runtime.DeployABExperiment(c.Request.Context(), runtime.ABExperimentDeployment{
		ClusterID: target.ClusterID, Namespace: target.Namespace, ProjectID: project.ID, ExperimentID: created.ID,
		ABranch: aVersion.Branch, ACommitSHA: aVersion.CommitSHA, AReleaseID: aVersion.ReleaseID,
		BBranch: bVersion.Branch, BCommitSHA: bVersion.CommitSHA, BReleaseID: bVersion.ReleaseID,
		AImage: aImage, BImage: bImage, Replicas: target.Replicas, Strategy: target.DeployStrategy, Assignment: assignment, RoutingRule: created.RoutingRule,
		ATraffic: aTraffic, BTraffic: bTraffic,
	}); err != nil {
		_ = s.deps.Runtime.CleanupABExperiment(c.Request.Context(), created.ClusterID, created.Namespace, project.ID, created.ID)
		_, _ = s.deps.Store.StopABExperiment(c.Request.Context(), project.SpaceID, project.ID, created.ID)
		writeExperimentError(c, err)
		return
	}
	s.recordAudit(c, "创建 A/B 实验", project.Name+" · "+target.Name)
	created = s.withABRuntime(c.Request.Context(), created)
	c.JSON(http.StatusCreated, gin.H{"experiment": created})
}

func (s *Server) updateABExperimentTraffic(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Store.GetABExperiment(c.Request.Context(), project.SpaceID, project.ID, c.Param("experimentID"))
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	if item.Status != domain.ABExperimentRunning {
		writeExperimentError(c, fmt.Errorf("%w: 实验当前不可调整流量", store.ErrConflict))
		return
	}
	var request updateABTrafficRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "流量配置格式不正确")
		return
	}
	if request.ATraffic < 0 || request.BTraffic < 0 || request.ATraffic > 100 || request.BTraffic > 100 || request.ATraffic+request.BTraffic != 100 {
		writeExperimentError(c, fmt.Errorf("%w: A/B 流量必须在 0 到 100 之间且总和为 100", store.ErrInvalidInput))
		return
	}
	if err := s.deps.Runtime.UpdateABExperimentTraffic(c.Request.Context(), item.ClusterID, item.Namespace, project.ID, item.ID, request.ATraffic, request.BTraffic); err != nil {
		writeExperimentError(c, err)
		return
	}
	updated, err := s.deps.Store.UpdateABExperimentTraffic(c.Request.Context(), project.SpaceID, project.ID, item.ID, store.UpdateABTrafficInput{ATraffic: request.ATraffic, BTraffic: request.BTraffic})
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	s.recordAudit(c, "调整 A/B 实验流量", project.Name+" · "+item.Name)
	c.JSON(http.StatusOK, gin.H{"experiment": s.withABRuntime(c.Request.Context(), updated)})
}

func (s *Server) stopABExperiment(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Store.GetABExperiment(c.Request.Context(), project.SpaceID, project.ID, c.Param("experimentID"))
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	if item.Status != domain.ABExperimentRunning {
		writeExperimentError(c, fmt.Errorf("%w: 实验当前不可停止", store.ErrConflict))
		return
	}
	if err := s.deps.Runtime.StopABExperiment(c.Request.Context(), item.ClusterID, item.Namespace, project.ID, item.ID, "a"); err != nil {
		writeExperimentError(c, err)
		return
	}
	updated, err := s.deps.Store.StopABExperiment(c.Request.Context(), project.SpaceID, project.ID, item.ID)
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	s.recordAudit(c, "停止 A/B 实验", project.Name+" · "+item.Name)
	c.JSON(http.StatusOK, gin.H{"experiment": s.withABRuntime(c.Request.Context(), updated)})
}

func (s *Server) finishABExperiment(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	item, err := s.deps.Store.GetABExperiment(c.Request.Context(), project.SpaceID, project.ID, c.Param("experimentID"))
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	if item.Status != domain.ABExperimentRunning {
		writeExperimentError(c, fmt.Errorf("%w: 实验当前不可结束", store.ErrConflict))
		return
	}
	var request finishABExperimentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "结束结果格式不正确")
		return
	}
	result := strings.TrimSpace(strings.ToLower(request.Result))
	if result != "keep_a" && result != "promote_b" {
		writeExperimentError(c, fmt.Errorf("%w: 结束结果必须是 keep_a 或 promote_b", store.ErrInvalidInput))
		return
	}
	keepVariant := "a"
	if result == "promote_b" {
		keepVariant = "b"
	}
	if err := s.deps.Runtime.StopABExperiment(c.Request.Context(), item.ClusterID, item.Namespace, project.ID, item.ID, keepVariant); err != nil {
		writeExperimentError(c, err)
		return
	}
	updated, err := s.deps.Store.FinishABExperiment(c.Request.Context(), project.SpaceID, project.ID, item.ID, result)
	if err != nil {
		writeExperimentError(c, err)
		return
	}
	s.recordAudit(c, "结束 A/B 实验", project.Name+" · "+item.Name)
	c.JSON(http.StatusOK, gin.H{"experiment": s.withABRuntime(c.Request.Context(), updated)})
}

func (s *Server) findProjectTarget(ctx context.Context, project domain.Project, targetID string) (domain.DeploymentTarget, bool) {
	targets, err := s.deps.Store.ListDeploymentTargets(ctx, project.SpaceID, project.ID)
	if err != nil {
		return domain.DeploymentTarget{}, false
	}
	for _, target := range targets {
		if target.ID == targetID {
			return target, true
		}
	}
	return domain.DeploymentTarget{}, false
}

func (s *Server) releaseVersionForTarget(project domain.Project, target domain.DeploymentTarget, releaseID string, allowCurrent bool) (domain.ABExperimentVersion, error) {
	releaseID = strings.TrimSpace(releaseID)
	items := s.deps.Release.List(project.ID)
	if releaseID != "" {
		for _, item := range items {
			if item.ID != releaseID {
				continue
			}
			if allowCurrent {
				version, ok := successfulReleaseVersion(item, target.ID)
				if !ok {
					return domain.ABExperimentVersion{}, fmt.Errorf("%w: 发布单 %s 尚未完成环境 %s 发布", store.ErrConflict, releaseID, target.Name)
				}
				return version, nil
			}
			version, ok := candidateReleaseVersion(item)
			if !ok {
				return domain.ABExperimentVersion{}, fmt.Errorf("%w: 发布单 %s 没有可用的候选 commit", store.ErrConflict, releaseID)
			}
			return version, nil
		}
		return domain.ABExperimentVersion{}, fmt.Errorf("%w: 发布单不存在", store.ErrNotFound)
	}
	if !allowCurrent {
		return domain.ABExperimentVersion{}, fmt.Errorf("%w: B 版本必须选择有 commit 的候选发布单", store.ErrInvalidInput)
	}
	if target.LastCommit != "" {
		for _, item := range items {
			if version, ok := successfulReleaseVersion(item, target.ID); ok && strings.EqualFold(shortABSHA(version.CommitSHA), target.LastCommit) {
				version.Role = "A"
				version.Message = "环境当前稳定版本"
				return version, nil
			}
		}
		return domain.ABExperimentVersion{Role: "A", ReleaseID: target.LastRelease, Branch: project.DefaultBranch, CommitSHA: target.LastCommit, ShortSHA: shortABSHA(target.LastCommit), Message: "环境当前稳定版本"}, nil
	}
	for _, item := range items {
		if version, ok := successfulReleaseVersion(item, target.ID); ok {
			version.Role = "A"
			version.Message = "环境当前稳定版本"
			return version, nil
		}
	}
	return domain.ABExperimentVersion{}, fmt.Errorf("%w: 环境还没有可用的 A 稳定版本", store.ErrConflict)
}

func successfulReleaseVersion(item release.Release, targetID string) (domain.ABExperimentVersion, bool) {
	if item.Status == release.StatusFailed || item.Status == release.StatusCancelled {
		return domain.ABExperimentVersion{}, false
	}
	for _, target := range item.Targets {
		if target.ID != targetID || target.Status != release.TargetSucceeded || len(item.Commits) == 0 {
			continue
		}
		commit := item.Commits[0]
		return domain.ABExperimentVersion{Role: "B", ReleaseID: item.ID, Branch: item.Branch, CommitSHA: commit.SHA, ShortSHA: commit.ShortSHA, Message: commit.Message}, true
	}
	return domain.ABExperimentVersion{}, false
}

func candidateReleaseVersion(item release.Release) (domain.ABExperimentVersion, bool) {
	if item.Status == release.StatusFailed || item.Status == release.StatusCancelled || len(item.Commits) == 0 {
		return domain.ABExperimentVersion{}, false
	}
	commit := item.Commits[0]
	return domain.ABExperimentVersion{Role: "B", ReleaseID: item.ID, Branch: item.Branch, CommitSHA: commit.SHA, ShortSHA: commit.ShortSHA, Message: commit.Message}, true
}

// imageForABVersion reuses a release artifact when one exists. A candidate
// release that has not yet been deployed can still participate in an A/B
// experiment: the detached builder produces an immutable image for its saved
// commit without changing that release's deployment lifecycle.
func (s *Server) imageForABVersion(ctx context.Context, project domain.Project, version domain.ABExperimentVersion) (string, error) {
	releaseID := strings.TrimSpace(version.ReleaseID)
	commitSHA := strings.TrimSpace(version.CommitSHA)
	if releaseID == "" || commitSHA == "" {
		return "", fmt.Errorf("%w: A/B 版本必须来自有保存提交的发布单", store.ErrConflict)
	}
	item, err := s.deps.Release.Get(releaseID)
	if err != nil {
		return "", err
	}
	if item.ProjectID != project.ID {
		return "", fmt.Errorf("%w: 发布单不属于当前项目", store.ErrNotFound)
	}
	if item.Artifact != nil && strings.EqualFold(strings.TrimSpace(item.Artifact.CommitSHA), commitSHA) && imagebuild.IsDigest(item.Artifact.Digest) && strings.Contains(item.Artifact.Image, "@"+item.Artifact.Digest) {
		return item.Artifact.Image, nil
	}
	result, err := s.buildReleaseImage(ctx, project, item.ID, commitSHA)
	if err != nil {
		return "", err
	}
	if !imagebuild.IsDigest(result.Digest) || !strings.Contains(result.Image, "@"+result.Digest) {
		return "", fmt.Errorf("A/B 候选版本没有可用的不可变镜像")
	}
	return result.Image, nil
}

func (s *Server) withABRuntime(ctx context.Context, item domain.ABExperiment) domain.ABExperiment {
	pods, err := s.deps.Runtime.ListABExperimentPods(ctx, item.ClusterID, item.Namespace, item.ProjectID, item.ID)
	if err != nil {
		return item
	}
	item.APods = item.APods[:0]
	item.BPods = item.BPods[:0]
	for _, pod := range pods {
		variant := strings.ToLower(strings.TrimSpace(pod.Labels["cicd.yuebuy.com/ab-variant"]))
		view := domain.ABExperimentPod{Name: pod.Name, Variant: variant, Version: pod.Labels["version"], NodeName: pod.NodeName, PodIP: pod.PodIP, Phase: string(pod.Phase), Ready: pod.Ready, RestartCount: pod.RestartCount}
		if variant == "b" {
			item.BPods = append(item.BPods, view)
		} else {
			item.APods = append(item.APods, view)
		}
	}
	if metrics, metricsErr := s.deps.Runtime.GetABExperimentMetrics(ctx, item.ClusterID, item.Namespace, item.ProjectID, item.ID); metricsErr == nil {
		item.AStats = domain.ABExperimentStats{MetricsAvailable: metrics.A.MetricsAvailable, MetricsMessage: metrics.A.MetricsMessage, RequestRateRPS: metrics.A.RequestRateRPS, ErrorRatePercent: metrics.A.ErrorRatePercent, LatencyP95MS: metrics.A.LatencyP95MS}
		item.BStats = domain.ABExperimentStats{MetricsAvailable: metrics.B.MetricsAvailable, MetricsMessage: metrics.B.MetricsMessage, RequestRateRPS: metrics.B.RequestRateRPS, ErrorRatePercent: metrics.B.ErrorRatePercent, LatencyP95MS: metrics.B.LatencyP95MS}
	}
	return item
}

func shortABSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 10 {
		return value[:10]
	}
	return value
}

func writeExperimentError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "internal_error"
	message := err.Error()
	switch {
	case errors.Is(err, store.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, store.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, store.ErrInvalidInput):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, runtime.ErrInvalidRuntimeInput):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, runtime.ErrClusterNotFound), errors.Is(err, runtime.ErrProjectNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, runtime.ErrReleaseDeploymentUnsupported):
		status, code, message = http.StatusNotImplemented, "ab_experiment_unsupported", "当前运行时未接入 A/B 流量路由"
	case errors.Is(err, runtime.ErrImageBuildUnsupported):
		status, code = http.StatusNotImplemented, "image_build_unsupported"
	case errors.Is(err, imagebuild.ErrUnauthorized):
		status, code = http.StatusBadGateway, "image_builder_unauthorized"
	case errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusGatewayTimeout, "provider_timeout"
	}
	writeError(c, status, code, message)
}

func currentUserID(c *gin.Context) uint64 {
	claims, ok := auth.ClaimsFrom(c)
	if !ok {
		return 0
	}
	return claims.UserID
}
