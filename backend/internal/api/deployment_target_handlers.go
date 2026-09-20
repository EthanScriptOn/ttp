package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func (s *Server) listDeploymentTargets(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	items, err := s.deps.Store.ListDeploymentTargets(c.Request.Context(), project.SpaceID, project.ID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	for index := range items {
		items[index] = s.enrichDeploymentTarget(c, project, items[index])
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (s *Server) createDeploymentTarget(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	var input store.CreateDeploymentTargetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "部署目标配置格式不正确")
		return
	}
	environment, err := store.NormalizeDeploymentEnvironment(input.Environment)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	namespace, err := s.generatedDeploymentNamespace(c, project, environment)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	clusterID := strings.TrimSpace(input.ClusterID)
	if clusterID == "" {
		clusterID = strings.TrimSpace(project.ClusterID)
	}
	input.ClusterID = clusterID
	input.Environment = environment
	input.Namespace = namespace
	quota, err := s.resolveNamespaceQuota(c.Request.Context(), project.SpaceID, clusterID, environment, input.ResourceQuota, false)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	input.ResourceQuota = &quota
	if err := s.ensureRuntimeCluster(c.Request.Context(), project.SpaceID, clusterID); err != nil {
		writeRuntimeError(c, err)
		return
	}
	if err := s.ensureRuntimeNamespaceWithQuota(c.Request.Context(), project.SpaceID, environment, clusterID, namespace, quota); err != nil {
		writeRuntimeError(c, err)
		return
	}
	if _, err := s.deps.Store.UpsertNamespaceQuota(c.Request.Context(), project.SpaceID, clusterID, environment, namespace, quota); err != nil {
		writeStoreError(c, err)
		return
	}
	target, err := s.deps.Store.CreateDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	target = s.enrichDeploymentTarget(c, project, target)
	s.recordAudit(c, "添加环境部署目标", project.Name+" · "+target.Name)
	c.JSON(http.StatusCreated, gin.H{"target": target})
}

func (s *Server) updateDeploymentTarget(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	var input store.UpdateDeploymentTargetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "部署目标配置格式不正确")
		return
	}
	if s.deploymentTargetHasActiveRelease(project.ID, c.Param("targetID")) {
		writeError(c, http.StatusConflict, "deployment_target_locked", "环境正在发布中，发布完成后才能修改环境配置")
		return
	}
	current, err := s.deps.Store.GetDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, c.Param("targetID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	environment, err := store.NormalizeDeploymentEnvironment(current.Environment)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if input.Environment != nil {
		environment, err = store.NormalizeDeploymentEnvironment(*input.Environment)
		if err != nil {
			writeStoreError(c, err)
			return
		}
	}
	input.Environment = &environment
	clusterID := current.ClusterID
	if input.ClusterID != nil {
		clusterID = strings.TrimSpace(*input.ClusterID)
	}
	namespace, err := s.generatedDeploymentNamespace(c, project, environment)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	input.Namespace = &namespace
	quota, err := s.resolveNamespaceQuota(c.Request.Context(), project.SpaceID, clusterID, environment, input.ResourceQuota, true)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	input.ResourceQuota = &quota
	if err := s.ensureRuntimeCluster(c.Request.Context(), project.SpaceID, clusterID); err != nil {
		writeRuntimeError(c, err)
		return
	}
	if err := s.ensureRuntimeNamespaceWithQuota(c.Request.Context(), project.SpaceID, environment, clusterID, namespace, quota); err != nil {
		writeRuntimeError(c, err)
		return
	}
	if _, err := s.deps.Store.UpsertNamespaceQuota(c.Request.Context(), project.SpaceID, clusterID, environment, namespace, quota); err != nil {
		writeStoreError(c, err)
		return
	}
	target, err := s.deps.Store.UpdateDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, c.Param("targetID"), input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	target = s.enrichDeploymentTarget(c, project, target)
	s.recordAudit(c, "更新环境部署目标", project.Name+" · "+target.Name)
	c.JSON(http.StatusOK, gin.H{"target": target})
}

func (s *Server) deleteDeploymentTarget(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	target, err := s.deps.Store.GetDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, c.Param("targetID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if s.deploymentTargetHasActiveRelease(project.ID, c.Param("targetID")) {
		writeError(c, http.StatusConflict, "deployment_target_locked", "环境正在发布中，发布完成后才能删除环境配置")
		return
	}
	if !s.ensureRuntimeTarget(c, project, target) {
		return
	}
	if err := s.deps.Runtime.CleanupEnvironment(c.Request.Context(), target.ClusterID, target.Namespace, project.ID, target.ID); err != nil {
		writeRuntimeError(c, err)
		return
	}
	if err := s.deps.Store.DeleteDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, target.ID); err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "删除环境部署目标", project.Name+" · "+c.Param("targetID"))
	c.JSON(http.StatusOK, gin.H{"deleted": true, "target_id": c.Param("targetID")})
}

func (s *Server) deploymentTargetHasActiveRelease(projectID, targetID string) bool {
	if s.deps.Release == nil {
		return false
	}
	for _, item := range s.deps.Release.List(projectID) {
		if item.Status != release.StatusQueued && item.Status != release.StatusRunning {
			continue
		}
		for _, target := range item.Targets {
			if target.ID == targetID && (target.Status == release.TargetPending || target.Status == release.TargetRunning) {
				return true
			}
		}
	}
	return false
}

func (s *Server) generatedDeploymentNamespace(c *gin.Context, project domain.Project, environment string) (string, error) {
	space, err := s.deps.Store.Space(c.Request.Context(), currentUserID(c), project.SpaceID)
	if err != nil {
		return "", err
	}
	source := strings.TrimSpace(space.Slug)
	if source == "" {
		source = strings.TrimSpace(space.Name)
	}
	if source == "" {
		source = strings.TrimSpace(space.ID)
	}
	return store.BuildDeploymentNamespace(source, environment)
}

func (s *Server) resolveNamespaceQuota(ctx context.Context, spaceID, clusterID, environment string, requested *domain.NamespaceQuota, preferRequested bool) (domain.NamespaceQuota, error) {
	if !preferRequested && s.deps.Store != nil {
		persisted, err := s.deps.Store.GetNamespaceQuota(ctx, spaceID, clusterID, environment)
		switch {
		case err == nil:
			return store.NormalizeNamespaceQuota(&persisted)
		case !errors.Is(err, store.ErrNotFound):
			return domain.NamespaceQuota{}, err
		}
	}
	if requested != nil {
		return store.NormalizeNamespaceQuota(requested)
	}
	if s.deps.Store != nil {
		persisted, err := s.deps.Store.GetNamespaceQuota(ctx, spaceID, clusterID, environment)
		switch {
		case err == nil:
			return store.NormalizeNamespaceQuota(&persisted)
		case !errors.Is(err, store.ErrNotFound):
			return domain.NamespaceQuota{}, err
		}
	}
	return store.NormalizeNamespaceQuota(nil)
}

// deploymentTargetForProject resolves the target selected by the console. An
// omitted target_id means the first target in the explicit release order.
func (s *Server) deploymentTargetForProject(c *gin.Context, project domain.Project) (domain.DeploymentTarget, bool) {
	targetID := strings.TrimSpace(c.Query("target_id"))
	target, err := s.deps.Store.GetDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, targetID)
	if err != nil {
		writeStoreError(c, err)
		return domain.DeploymentTarget{}, false
	}
	return s.enrichDeploymentTarget(c, project, target), true
}

func (s *Server) enrichDeploymentTarget(c *gin.Context, project domain.Project, target domain.DeploymentTarget) domain.DeploymentTarget {
	if s.deps.Store != nil {
		if quota, err := s.deps.Store.GetNamespaceQuota(c.Request.Context(), project.SpaceID, target.ClusterID, target.Environment); err == nil {
			target.ResourceQuota = quota
		} else if errors.Is(err, store.ErrNotFound) {
			target.ResourceQuota = store.DefaultNamespaceQuota()
		}
	}
	target.Health = "unknown"
	target.PodCount = 0
	target.HealthyPodCount = 0
	if s.deps.Runtime != nil {
		if err := s.ensureRuntimeCluster(c.Request.Context(), project.SpaceID, target.ClusterID); err == nil {
			pods, err := s.deps.Runtime.ListPodsInNamespace(c.Request.Context(), target.ClusterID, target.Namespace, project.ID)
			if err == nil {
				target.PodCount = len(pods)
				for _, pod := range pods {
					if pod.Ready {
						target.HealthyPodCount++
					}
				}
				switch {
				case target.PodCount == 0:
					target.Health = "unknown"
				case target.PodCount == target.HealthyPodCount:
					target.Health = "healthy"
				default:
					target.Health = "degraded"
				}
			}
		}
	}
	if s.deps.Release != nil {
		matchedTarget := false
		for _, item := range s.deps.Release.List(project.ID) {
			for _, releaseTarget := range item.Targets {
				if releaseTarget.ID != target.ID {
					continue
				}
				matchedTarget = true
				target.LastRelease = item.CreatedAt.UTC().Format("2006-01-02 15:04")
				if len(item.Commits) > 0 {
					target.LastCommit = item.Commits[0].ShortSHA
				}
				return target
			}
			// Releases created before environment targets were introduced have no
			// target snapshots. Keep those records visible on DEV.
			if !matchedTarget && len(item.Targets) == 0 && target.Stage == store.DeploymentStageDev {
				target.LastRelease = item.CreatedAt.UTC().Format("2006-01-02 15:04")
				if len(item.Commits) > 0 {
					target.LastCommit = item.Commits[0].ShortSHA
				}
				return target
			}
		}
	}
	return target
}
