package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func (s *Server) listDeploymentTargets(c *gin.Context) {
	project, ok := s.projectForRequest(c)
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
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	var input store.CreateDeploymentTargetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "部署目标配置格式不正确")
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
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	var input store.UpdateDeploymentTargetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "部署目标配置格式不正确")
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
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	if err := s.deps.Store.DeleteDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, c.Param("targetID")); err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "删除环境部署目标", project.Name+" · "+c.Param("targetID"))
	c.JSON(http.StatusOK, gin.H{"deleted": true, "target_id": c.Param("targetID")})
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

func deploymentTargetFromProject(project domain.Project) domain.DeploymentTarget {
	return store.LegacyDeploymentTarget(project)
}
