package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/config"
	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type Dependencies struct {
	Config        config.Config
	Store         store.Store
	Auth          *auth.Manager
	Git           git.Provider
	Release       *release.Service
	Runtime       *runtime.Service
	ImageBuilder  imagebuild.Builder
	CredentialKey string
}

type Server struct {
	deps             Dependencies
	router           *gin.Engine
	credentialCipher *credentialCipher
}

func New(deps Dependencies) *Server {
	if deps.Auth == nil {
		deps.Auth = auth.NewManager(deps.Config.JWTSecret, deps.Config.JWTMinutes)
	}
	if deps.Release == nil {
		deps.Release = release.NewService(deps.Git)
	}
	if deps.Runtime == nil {
		deps.Runtime = runtime.NewService(nil)
	}
	credentialKey := strings.TrimSpace(deps.CredentialKey)
	if credentialKey == "" {
		credentialKey = strings.TrimSpace(deps.Config.GitCredentialKey)
	}
	router := gin.New()
	router.Use(gin.Recovery(), cors(deps.Config.AllowedOrigin))
	server := &Server{deps: deps, router: router, credentialCipher: newCredentialCipher(credentialKey, deps.Config.JWTSecret)}
	server.routes()
	return server
}

func (s *Server) Router() http.Handler { return s.router }

func (s *Server) routes() {
	api := s.router.Group("/api")
	api.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	api.POST("/auth/login", s.login)

	protected := api.Group("")
	protected.Use(auth.Middleware(s.deps.Auth))
	protected.GET("/auth/me", s.me)
	protected.POST("/auth/select-space", s.selectSpace)
	protected.GET("/spaces", s.listSpaces)
	protected.POST("/spaces", s.createSpace)
	protected.GET("/space/settings", requirePermission(s.deps.Store, permissions.SpaceRead), s.getSpaceSettings)
	protected.PATCH("/space/settings", requirePermission(s.deps.Store, permissions.SpaceUpdate), s.updateSpaceSettings)
	protected.GET("/space/members", requirePermission(s.deps.Store, permissions.MemberRead), s.getSpaceMembers)
	protected.POST("/space/members", requirePermission(s.deps.Store, permissions.MemberManage), s.createSpaceMember)
	protected.PATCH("/space/members/:userID", requirePermission(s.deps.Store, permissions.MemberManage), s.updateSpaceMember)
	protected.DELETE("/space/members/:userID", requirePermission(s.deps.Store, permissions.MemberManage), s.removeSpaceMember)
	protected.GET("/space/permissions", requirePermission(s.deps.Store, permissions.MemberRead), s.getSpacePermissions)
	protected.GET("/space/project-roles", requirePermission(s.deps.Store, permissions.MemberRead), s.listProjectRoles)
	protected.POST("/space/project-roles", requirePermission(s.deps.Store, permissions.ProjectRolesManage), s.createProjectRole)
	protected.PATCH("/space/project-roles/:roleID", requirePermission(s.deps.Store, permissions.ProjectRolesManage), s.updateProjectRole)
	protected.DELETE("/space/project-roles/:roleID", requirePermission(s.deps.Store, permissions.ProjectRolesManage), s.deleteProjectRole)

	protected.GET("/clusters", requirePermission(s.deps.Store, permissions.ClusterRead), s.listClusters)
	protected.GET("/clusters/:clusterID", requirePermission(s.deps.Store, permissions.ClusterRead), s.getCluster)
	protected.POST("/clusters", requirePermission(s.deps.Store, permissions.ClusterManage), s.createCluster)
	protected.PATCH("/clusters/:clusterID", requirePermission(s.deps.Store, permissions.ClusterManage), s.updateCluster)
	protected.POST("/clusters/:clusterID/test", requirePermission(s.deps.Store, permissions.ClusterManage), s.testCluster)
	protected.POST("/clusters/:clusterID/monitoring/install", requirePermission(s.deps.Store, permissions.ClusterManage), s.installMonitoring)
	protected.GET("/image-registry-connections", requirePermission(s.deps.Store, permissions.RegistryRead), s.listImageRegistryConnections)
	protected.POST("/image-registry-connections", requirePermission(s.deps.Store, permissions.RegistryManage), s.createImageRegistryConnection)
	protected.PATCH("/image-registry-connections/:connectionID", requirePermission(s.deps.Store, permissions.RegistryManage), s.updateImageRegistryConnection)
	protected.POST("/image-registry-connections/:connectionID/test", requirePermission(s.deps.Store, permissions.RegistryManage), s.testImageRegistryConnection)
	protected.DELETE("/image-registry-connections/:connectionID", requirePermission(s.deps.Store, permissions.RegistryManage), s.deleteImageRegistryConnection)
	protected.GET("/audit-logs", requirePermission(s.deps.Store, permissions.AuditRead), s.listAuditLogs)

	protected.GET("/projects", requirePermission(s.deps.Store, permissions.ProjectRead), s.listProjects)
	protected.POST("/projects", requirePermission(s.deps.Store, permissions.ProjectCreate), s.createProject)
	protected.GET("/projects/:projectID", requireProjectPermission(s.deps.Store, permissions.ProjectView), s.getProject)
	protected.GET("/projects/:projectID/access", requireProjectPermission(s.deps.Store, permissions.ProjectView), s.getProjectAccess)
	protected.PATCH("/projects/:projectID", requireProjectPermission(s.deps.Store, permissions.ProjectSettings), s.updateProject)
	protected.GET("/projects/:projectID/members", requireProjectPermission(s.deps.Store, permissions.ProjectMembersRead), s.listProjectMembers)
	protected.POST("/projects/:projectID/members", requireProjectPermission(s.deps.Store, permissions.ProjectMembersManage), s.createProjectMember)
	protected.PATCH("/projects/:projectID/members/:userID", requireProjectPermission(s.deps.Store, permissions.ProjectMembersManage), s.updateProjectMember)
	protected.DELETE("/projects/:projectID/members/:userID", requireProjectPermission(s.deps.Store, permissions.ProjectMembersManage), s.removeProjectMember)
	protected.GET("/projects/:projectID/deployment-targets", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentRead), s.listDeploymentTargets)
	protected.POST("/projects/:projectID/deployment-targets", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentManage), s.createDeploymentTarget)
	protected.PATCH("/projects/:projectID/deployment-targets/:targetID", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentManage), s.updateDeploymentTarget)
	protected.DELETE("/projects/:projectID/deployment-targets/:targetID", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentManage), s.deleteDeploymentTarget)
	protected.GET("/projects/:projectID/git/branches", requireProjectPermission(s.deps.Store, permissions.ProjectGitRead), s.listBranches)
	protected.GET("/projects/:projectID/git/commits", requireProjectPermission(s.deps.Store, permissions.ProjectGitRead), s.listCommits)
	protected.GET("/projects/:projectID/git/tags", requireProjectPermission(s.deps.Store, permissions.ProjectGitRead), s.listTags)
	protected.GET("/projects/:projectID/git/access", requireProjectPermission(s.deps.Store, permissions.ProjectGitRead), s.gitRepositoryAccess)
	protected.GET("/projects/:projectID/git/credential", requireProjectPermission(s.deps.Store, permissions.ProjectGitRead), s.getProjectGitCredential)
	protected.PUT("/projects/:projectID/git/credential", requireProjectPermission(s.deps.Store, permissions.ProjectGitManage), s.saveProjectGitCredential)
	protected.DELETE("/projects/:projectID/git/credential", requireProjectPermission(s.deps.Store, permissions.ProjectGitManage), s.deleteProjectGitCredential)
	protected.POST("/projects/:projectID/release-preparation", requireProjectPermission(s.deps.Store, permissions.ProjectReleaseCreate), s.prepareRelease)
	protected.GET("/projects/:projectID/deployment-config", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentRead), s.getDeploymentConfig)
	protected.PUT("/projects/:projectID/deployment-config", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentManage), s.saveDeploymentConfig)
	protected.POST("/projects/:projectID/deployment-config/validate", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentRead), s.validateDeploymentConfig)
	protected.POST("/projects/:projectID/deployment-config/convert", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentRead), s.convertDeploymentConfig)
	protected.GET("/projects/:projectID/deployment-resources", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentRead), s.listDeploymentResourceFiles)
	protected.POST("/projects/:projectID/deployment-resources", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentManage), s.createDeploymentResourceFile)
	protected.POST("/projects/:projectID/deployment-resources/validate", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentRead), s.validateDeploymentResourceFile)
	protected.PUT("/projects/:projectID/deployment-resources/:resourceID", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentManage), s.updateDeploymentResourceFile)
	protected.DELETE("/projects/:projectID/deployment-resources/:resourceID", requireProjectPermission(s.deps.Store, permissions.ProjectDeploymentManage), s.deleteDeploymentResourceFile)

	protected.GET("/projects/:projectID/releases", requireProjectPermission(s.deps.Store, permissions.ProjectReleaseRead), s.listReleases)
	protected.POST("/projects/:projectID/releases", requireProjectPermission(s.deps.Store, permissions.ProjectReleaseCreate), s.createRelease)
	protected.GET("/projects/:projectID/releases/:releaseID", requireProjectPermission(s.deps.Store, permissions.ProjectReleaseRead), s.getRelease)
	protected.GET("/projects/:projectID/releases/:releaseID/targets/:targetID/logs", requireProjectPermission(s.deps.Store, permissions.ProjectReleaseRead), s.getReleaseTargetLogs)
	protected.POST("/projects/:projectID/releases/:releaseID/publish", requireProjectPermission(s.deps.Store, permissions.ProjectReleasePublish), s.publishRelease)
	protected.POST("/projects/:projectID/releases/:releaseID/targets/:targetID/retry", requireProjectPermission(s.deps.Store, permissions.ProjectReleasePublish), s.retryReleaseTarget)
	protected.PATCH("/projects/:projectID/releases/:releaseID/progress", requireProjectPermission(s.deps.Store, permissions.ProjectReleasePublish), s.updateReleaseProgress)
	protected.POST("/projects/:projectID/releases/:releaseID/cancel", requireProjectPermission(s.deps.Store, permissions.ProjectReleasePublish), s.cancelRelease)
	protected.POST("/projects/:projectID/releases/:releaseID/fail", requireProjectPermission(s.deps.Store, permissions.ProjectReleasePublish), s.failRelease)
	protected.DELETE("/projects/:projectID/releases/:releaseID/commits/:sha", requireProjectPermission(s.deps.Store, permissions.ProjectReleaseUpdate), s.removeReleaseCommit)
	protected.GET("/projects/:projectID/ab-experiments", requireProjectPermission(s.deps.Store, permissions.ProjectReleaseRead), s.listABExperiments)
	protected.POST("/projects/:projectID/ab-experiments", requireProjectPermission(s.deps.Store, permissions.ProjectReleaseCreate), s.createABExperiment)
	protected.GET("/projects/:projectID/ab-experiments/:experimentID", requireProjectPermission(s.deps.Store, permissions.ProjectReleaseRead), s.getABExperiment)
	protected.PATCH("/projects/:projectID/ab-experiments/:experimentID/traffic", requireProjectPermission(s.deps.Store, permissions.ProjectReleasePublish), s.updateABExperimentTraffic)
	protected.POST("/projects/:projectID/ab-experiments/:experimentID/stop", requireProjectPermission(s.deps.Store, permissions.ProjectReleasePublish), s.stopABExperiment)
	protected.POST("/projects/:projectID/ab-experiments/:experimentID/finish", requireProjectPermission(s.deps.Store, permissions.ProjectReleasePublish), s.finishABExperiment)

	protected.GET("/projects/:projectID/pods", requireProjectPermission(s.deps.Store, permissions.ProjectRuntimeRead), s.listPods)
	protected.GET("/projects/:projectID/pods/:podName", requireProjectPermission(s.deps.Store, permissions.ProjectRuntimeRead), s.getPod)
	protected.GET("/projects/:projectID/pods/:podName/logs", requireProjectPermission(s.deps.Store, permissions.ProjectRuntimeRead), s.getPodLogs)
	protected.POST("/projects/:projectID/pods/:podName/exec", requireProjectPermission(s.deps.Store, permissions.ProjectRuntimeTerminal), s.execPodCommand)
	protected.PATCH("/projects/:projectID/pods/:podName/config", requireProjectPermission(s.deps.Store, permissions.ProjectRuntimeConfig), s.updatePodConfig)
	protected.GET("/projects/:projectID/metrics", requireProjectPermission(s.deps.Store, permissions.ProjectRuntimeRead), s.projectMetrics)
	protected.GET("/clusters/:clusterID/metrics", requirePermission(s.deps.Store, permissions.ClusterRead), s.clusterMetrics)
}

func cors(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if allowedOrigin == "*" || origin == allowedOrigin || origin == "" {
			c.Header("Access-Control-Allow-Origin", func() string {
				if origin != "" {
					return origin
				}
				return allowedOrigin
			}())
		}
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Space-ID")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func (s *Server) requireSpace(c *gin.Context) (auth.Claims, bool) {
	claims, ok := auth.ClaimsFrom(c)
	if !ok || strings.TrimSpace(claims.SpaceID) == "" {
		writeError(c, http.StatusForbidden, "space_required", "请先选择空间")
		return auth.Claims{}, false
	}
	if _, err := s.deps.Store.Space(c.Request.Context(), claims.UserID, claims.SpaceID); err != nil {
		writeStoreError(c, err)
		return auth.Claims{}, false
	}
	return claims, true
}

func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func writeStoreError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "internal_error"
	message := err.Error()
	switch {
	case errors.Is(err, store.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, store.ErrForbidden):
		status, code = http.StatusForbidden, "forbidden"
	case errors.Is(err, store.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, store.ErrInvalidInput):
		status, code = http.StatusBadRequest, "invalid_request"
		message = strings.TrimPrefix(message, store.ErrInvalidInput.Error()+": ")
	}
	writeError(c, status, code, message)
}
