package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func (s *Server) listProjects(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	items, err := s.deps.Store.ListProjects(c.Request.Context(), claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	for index := range items {
		items[index] = s.enrichProject(c.Request.Context(), items[index])
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (s *Server) createProject(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	var input store.CreateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "项目配置格式不正确")
		return
	}
	if err := store.ValidateCreateProjectInput(input); err != nil {
		writeStoreError(c, err)
		return
	}
	// The repository URL is the source of truth. Never trust a client-supplied
	// ID because it could point a project at another registered repository.
	if strings.TrimSpace(input.ID) == "" {
		input.ID = uuid.NewString()
	}
	if strings.TrimSpace(input.ClusterID) == "" {
		writeError(c, http.StatusBadRequest, "invalid_request", "创建项目时必须选择集群")
		return
	}
	if _, err := s.deps.Store.GetCluster(c.Request.Context(), claims.SpaceID, strings.TrimSpace(input.ClusterID)); err != nil {
		writeStoreError(c, err)
		return
	}
	input.RepositoryID = repositoryID(input.ID, input.RepositoryURL)
	if err := s.registerRepository(input.RepositoryID, input.RepositoryURL); err != nil {
		writeProviderError(c, err)
		return
	}
	project, err := s.deps.Store.CreateProject(c.Request.Context(), claims.SpaceID, input)
	if err != nil {
		if unregistrar, supported := s.deps.Git.(git.RepositoryUnregistrar); supported {
			_ = unregistrar.UnregisterRepository(input.RepositoryID, input.RepositoryURL)
		}
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "创建项目", project.Name)
	c.JSON(http.StatusCreated, s.enrichProject(c.Request.Context(), project))
}

func (s *Server) getProject(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	project, err := s.deps.Store.GetProject(c.Request.Context(), claims.SpaceID, c.Param("projectID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, s.enrichProject(c.Request.Context(), project))
}

func (s *Server) updateProject(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	var input store.UpdateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "项目配置格式不正确")
		return
	}
	if err := store.ValidateUpdateProjectInput(input); err != nil {
		writeStoreError(c, err)
		return
	}
	var previousProject domain.Project
	var previousProjectLoaded bool
	var err error
	if input.RepositoryURL != nil {
		previousProject, err = s.deps.Store.GetProject(c.Request.Context(), claims.SpaceID, c.Param("projectID"))
		if err != nil {
			writeStoreError(c, err)
			return
		}
		previousProjectLoaded = true
		repositoryURL := strings.TrimSpace(*input.RepositoryURL)
		if repositoryURL == "" {
			writeError(c, http.StatusBadRequest, "invalid_request", "代码仓库地址不能为空")
			return
		}
		nextRepositoryID := repositoryID(previousProject.ID, repositoryURL)
		input.RepositoryID = &nextRepositoryID
	}
	if input.RepositoryURL != nil {
		if err := s.registerRepository(valueOrString(input.RepositoryID), *input.RepositoryURL); err != nil {
			writeProviderError(c, err)
			return
		}
	}
	project, err := s.deps.Store.UpdateProject(c.Request.Context(), claims.SpaceID, c.Param("projectID"), input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if previousProjectLoaded && previousProject.RepositoryURL != project.RepositoryURL {
		if err := s.deps.Store.DeleteProjectGitCredential(c.Request.Context(), claims.SpaceID, project.ID); err != nil {
			writeStoreError(c, err)
			return
		}
		if unregistrar, supported := s.deps.Git.(git.RepositoryUnregistrar); supported {
			if err := unregistrar.UnregisterRepository(previousProject.RepositoryID, previousProject.RepositoryURL); err != nil {
				writeProviderError(c, err)
				return
			}
		}
	}
	s.recordAudit(c, "更新项目设置", project.Name)
	c.JSON(http.StatusOK, s.enrichProject(c.Request.Context(), project))
}

// enrichProject adds read-only runtime and release summary fields. The
// project store remains responsible for configuration; a provider outage
// should leave the project visible with an explicit unknown health state.
func (s *Server) enrichProject(ctx context.Context, project domain.Project) domain.Project {
	project.DeploymentTargetCount = 0
	project.DefaultTargetID = ""
	if s.deps.Store != nil {
		if targets, err := s.deps.Store.ListDeploymentTargets(ctx, project.SpaceID, project.ID); err == nil && len(targets) > 0 {
			project.DeploymentTargetCount = len(targets)
			project.DefaultTargetID = targets[0].ID
		}
	}
	project.Health = "unknown"
	project.PodCount = 0
	project.HealthyPodCount = 0
	if s.deps.Runtime != nil {
		if err := s.ensureRuntimeCluster(ctx, project.SpaceID, project.ClusterID); err == nil {
			if pods, err := s.deps.Runtime.ListPodsInNamespace(ctx, project.ClusterID, project.Namespace, project.ID); err == nil {
				project.PodCount = len(pods)
				for _, pod := range pods {
					if pod.Ready {
						project.HealthyPodCount++
					}
				}
				switch {
				case project.PodCount == 0:
					project.Health = "unknown"
				case project.HealthyPodCount == project.PodCount:
					project.Health = "healthy"
				default:
					project.Health = "degraded"
				}
			}
		}
	}
	if s.deps.Release != nil {
		if releases := s.deps.Release.List(project.ID); len(releases) > 0 {
			latest := releases[0]
			project.LastRelease = latest.CreatedAt.UTC().Format("2006-01-02 15:04")
			if len(latest.Commits) > 0 {
				project.LastCommit = latest.Commits[0].ShortSHA
			}
		}
	}
	return project
}

func (s *Server) listBranches(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	items, err := s.deps.Git.ListBranches(c.Request.Context(), project.RepositoryID)
	if err != nil {
		writeProviderError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) listCommits(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	branch := strings.TrimSpace(c.Query("branch"))
	if branch == "" {
		branch = project.DefaultBranch
	}
	limit := 200
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(c, http.StatusBadRequest, "invalid_request", "limit 必须在 1 到 200 之间")
			return
		}
		limit = parsed
	}
	items, err := s.deps.Git.ListCommits(c.Request.Context(), project.RepositoryID, branch, limit)
	if err != nil {
		writeProviderError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) listTags(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	provider, supported := s.deps.Git.(git.TagProvider)
	if !supported {
		c.JSON(http.StatusOK, gin.H{"items": []git.Tag{}, "supported": false})
		return
	}
	limit := 200
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			writeError(c, http.StatusBadRequest, "invalid_request", "tag 数量必须在 1 到 500 之间")
			return
		}
		limit = parsed
	}
	items, err := provider.ListTags(c.Request.Context(), project.RepositoryID, limit)
	if err != nil {
		writeProviderError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "supported": true, "total": len(items)})
}

func (s *Server) projectForRequest(c *gin.Context) (project domain.Project, ok bool) {
	claims, valid := s.requireSpace(c)
	if !valid {
		return domain.Project{}, false
	}
	item, err := s.deps.Store.GetProject(c.Request.Context(), claims.SpaceID, c.Param("projectID"))
	if err != nil {
		writeStoreError(c, err)
		return domain.Project{}, false
	}
	if err := s.configureProjectRepository(c.Request.Context(), item); err != nil {
		writeProviderError(c, err)
		return domain.Project{}, false
	}
	return item, true
}

func (s *Server) configureProjectRepository(ctx context.Context, project domain.Project) error {
	if err := s.registerRepository(project.RepositoryID, project.RepositoryURL); err != nil {
		return err
	}
	credential, err := s.deps.Store.GetProjectGitCredential(ctx, project.SpaceID, project.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: project Git credential lookup failed", git.ErrInvalidProviderConfig)
	}
	registry, supported := s.deps.Git.(git.RepositoryCredentialRegistry)
	if !supported {
		return fmt.Errorf("%w: project Git credential is not supported by this provider", git.ErrInvalidProviderConfig)
	}
	token, err := s.credentialCipher.open(credential.TokenCiphertext)
	if err != nil {
		return fmt.Errorf("%w: project Git credential is invalid", git.ErrInvalidProviderConfig)
	}
	return registry.ConfigureRepositoryCredential(project.RepositoryID, project.RepositoryURL, git.RepositoryCredential{
		Provider: credential.Provider,
		Username: credential.Username,
		Token:    token,
	})
}

func (s *Server) registerRepository(repositoryID, repositoryURL string) error {
	registrar, ok := s.deps.Git.(git.RepositoryRegistrar)
	if !ok {
		return nil
	}
	return registrar.RegisterRepository(repositoryID, repositoryURL)
}

func valueOrString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func repositoryID(projectID, url string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(projectID) + "\x00" + strings.TrimSpace(url)))
	return "repo-" + hex.EncodeToString(sum[:8])
}

func writeProviderError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "provider_error"
	switch {
	case errors.Is(err, git.ErrInvalidProviderConfig):
		status, code = http.StatusBadRequest, "invalid_provider_config"
	case errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusGatewayTimeout, "provider_timeout"
	case errors.Is(err, git.ErrProviderHTTP), errors.Is(err, git.ErrProviderResponse):
		status, code = http.StatusBadGateway, "provider_error"
	case errors.Is(err, git.ErrRepositoryConflict):
		status, code = http.StatusConflict, "repository_conflict"
	case errors.Is(err, git.ErrRepositoryNotFound), errors.Is(err, git.ErrBranchNotFound), errors.Is(err, git.ErrCommitNotFound):
		status, code = http.StatusNotFound, "not_found"
	}
	writeError(c, status, code, err.Error())
}
