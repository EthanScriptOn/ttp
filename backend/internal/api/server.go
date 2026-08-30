package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/config"
	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type Dependencies struct {
	Config  config.Config
	Store   store.Store
	Auth    *auth.Manager
	Git     git.Provider
	Release *release.Service
	Runtime *runtime.Service
}

type Server struct {
	deps   Dependencies
	router *gin.Engine
}

func New(deps Dependencies) *Server {
	if deps.Auth == nil {
		deps.Auth = auth.NewManager(deps.Config.JWTSecret, deps.Config.JWTMinutes)
	}
	if deps.Git == nil {
		deps.Git = git.NewDemoProvider()
	}
	if deps.Release == nil {
		deps.Release = release.NewService(deps.Git)
	}
	if deps.Runtime == nil {
		deps.Runtime = runtime.NewService(nil)
	}
	router := gin.New()
	router.Use(gin.Recovery(), cors(deps.Config.AllowedOrigin))
	server := &Server{deps: deps, router: router}
	server.routes()
	return server
}

func (s *Server) Router() http.Handler { return s.router }

func (s *Server) routes() {
	api := s.router.Group("/api")
	api.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "demo": s.deps.Config.DemoMode}) })
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

	protected.GET("/git/service-account", requirePermission(s.deps.Store, permissions.ProjectRead), s.gitServiceAccount)
	protected.GET("/clusters", requirePermission(s.deps.Store, permissions.ClusterRead), s.listClusters)
	protected.GET("/clusters/:clusterID", requirePermission(s.deps.Store, permissions.ClusterRead), s.getCluster)
	protected.POST("/clusters", requirePermission(s.deps.Store, permissions.ClusterManage), s.createCluster)
	protected.PATCH("/clusters/:clusterID", requirePermission(s.deps.Store, permissions.ClusterManage), s.updateCluster)
	protected.POST("/clusters/:clusterID/test", requirePermission(s.deps.Store, permissions.ClusterManage), s.testCluster)
	protected.GET("/audit-logs", requirePermission(s.deps.Store, permissions.AuditRead), s.listAuditLogs)

	protected.GET("/projects", requirePermission(s.deps.Store, permissions.ProjectRead), s.listProjects)
	protected.POST("/projects", requirePermission(s.deps.Store, permissions.ProjectCreate), s.createProject)
	protected.GET("/projects/:projectID", requirePermission(s.deps.Store, permissions.ProjectRead), s.getProject)
	protected.PATCH("/projects/:projectID", requirePermission(s.deps.Store, permissions.ProjectUpdate), s.updateProject)
	protected.GET("/projects/:projectID/deployment-targets", requirePermission(s.deps.Store, permissions.ProjectRead), s.listDeploymentTargets)
	protected.POST("/projects/:projectID/deployment-targets", requirePermission(s.deps.Store, permissions.ProjectUpdate), s.createDeploymentTarget)
	protected.PATCH("/projects/:projectID/deployment-targets/:targetID", requirePermission(s.deps.Store, permissions.ProjectUpdate), s.updateDeploymentTarget)
	protected.DELETE("/projects/:projectID/deployment-targets/:targetID", requirePermission(s.deps.Store, permissions.ProjectUpdate), s.deleteDeploymentTarget)
	protected.GET("/projects/:projectID/git/branches", requirePermission(s.deps.Store, permissions.ProjectRead), s.listBranches)
	protected.GET("/projects/:projectID/git/commits", requirePermission(s.deps.Store, permissions.ReleaseRead), s.listCommits)
	protected.GET("/projects/:projectID/git/tags", requirePermission(s.deps.Store, permissions.ReleaseRead), s.listTags)
	protected.GET("/projects/:projectID/git/access", requirePermission(s.deps.Store, permissions.ReleaseRead), s.gitRepositoryAccess)
	protected.POST("/projects/:projectID/release-preparation", requirePermission(s.deps.Store, permissions.ReleaseCreate), s.prepareRelease)
	protected.GET("/projects/:projectID/deployment-config", requirePermission(s.deps.Store, permissions.ProjectRead), s.getDeploymentConfig)
	protected.PUT("/projects/:projectID/deployment-config", requirePermission(s.deps.Store, permissions.ProjectUpdate), s.saveDeploymentConfig)
	protected.POST("/projects/:projectID/deployment-config/validate", requirePermission(s.deps.Store, permissions.ProjectRead), s.validateDeploymentConfig)
	protected.POST("/projects/:projectID/deployment-config/convert", requirePermission(s.deps.Store, permissions.ProjectRead), s.convertDeploymentConfig)

	protected.GET("/projects/:projectID/releases", requirePermission(s.deps.Store, permissions.ReleaseRead), s.listReleases)
	protected.GET("/projects/:projectID/release-batches", requirePermission(s.deps.Store, permissions.ReleaseRead), s.listReleaseBatches)
	protected.POST("/projects/:projectID/releases", requirePermission(s.deps.Store, permissions.ReleaseCreate), s.createRelease)
	protected.GET("/projects/:projectID/releases/:releaseID", requirePermission(s.deps.Store, permissions.ReleaseRead), s.getRelease)
	protected.POST("/projects/:projectID/releases/:releaseID/publish", requirePermission(s.deps.Store, permissions.ReleasePublish), s.publishRelease)
	protected.POST("/projects/:projectID/releases/:releaseID/targets/:targetID/publish", requirePermission(s.deps.Store, permissions.ReleasePublish), s.publishReleaseTarget)
	protected.POST("/projects/:projectID/releases/:releaseID/merge-main", requirePermission(s.deps.Store, permissions.ReleasePublish), s.mergeReleaseToMain)
	protected.POST("/projects/:projectID/releases/:releaseID/targets/:targetID/retry", requirePermission(s.deps.Store, permissions.ReleasePublish), s.retryReleaseTarget)
	protected.PATCH("/projects/:projectID/releases/:releaseID/progress", requirePermission(s.deps.Store, permissions.ReleasePublish), s.updateReleaseProgress)
	protected.POST("/projects/:projectID/releases/:releaseID/cancel", requirePermission(s.deps.Store, permissions.ReleasePublish), s.cancelRelease)
	protected.POST("/projects/:projectID/releases/:releaseID/fail", requirePermission(s.deps.Store, permissions.ReleasePublish), s.failRelease)
	protected.DELETE("/projects/:projectID/releases/:releaseID/commits/:sha", requirePermission(s.deps.Store, permissions.ReleaseUpdate), s.removeReleaseCommit)
	protected.POST("/projects/:projectID/release-batches/:batchID/close", requirePermission(s.deps.Store, permissions.ReleasePublish), s.closeReleaseBatch)

	protected.GET("/projects/:projectID/pods", requirePermission(s.deps.Store, permissions.RuntimeRead), s.listPods)
	protected.GET("/projects/:projectID/pods/:podName", requirePermission(s.deps.Store, permissions.RuntimeRead), s.getPod)
	protected.GET("/projects/:projectID/pods/:podName/logs", requirePermission(s.deps.Store, permissions.RuntimeRead), s.getPodLogs)
	protected.PATCH("/projects/:projectID/pods/:podName/config", requirePermission(s.deps.Store, permissions.RuntimeConfig), s.updatePodConfig)
	protected.GET("/projects/:projectID/metrics", requirePermission(s.deps.Store, permissions.RuntimeRead), s.projectMetrics)
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
	switch {
	case errors.Is(err, store.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, store.ErrForbidden):
		status, code = http.StatusForbidden, "forbidden"
	case errors.Is(err, store.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, store.ErrInvalidInput):
		status, code = http.StatusBadRequest, "invalid_request"
	}
	writeError(c, status, code, err.Error())
}
