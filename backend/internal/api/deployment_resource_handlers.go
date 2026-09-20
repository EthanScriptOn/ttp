package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/deploymentconfig"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type deploymentResourceFileRequest struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Format    string `json:"format"`
	Content   string `json:"content"`
	SortOrder int    `json:"sort_order"`
}

func (s *Server) listDeploymentResourceFiles(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	files, err := s.deps.Store.ListDeploymentResourceFiles(c.Request.Context(), project.SpaceID, project.ID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"resources": files})
}

func (s *Server) createDeploymentResourceFile(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	request, err := bindDeploymentResourceFileRequest(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	expectedNamespace, err := s.defaultDeploymentNamespace(c, project)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	input, err := normalizeDeploymentResourceFileInput(request, expectedNamespace)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}
	file, err := s.deps.Store.CreateDeploymentResourceFile(c.Request.Context(), project.SpaceID, project.ID, input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "创建 Kubernetes 资源文件", project.Name+" · "+file.Path)
	c.JSON(http.StatusCreated, gin.H{"resource": file})
}

func (s *Server) updateDeploymentResourceFile(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	request, err := bindDeploymentResourceFileRequest(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	expectedNamespace, err := s.defaultDeploymentNamespace(c, project)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	input, err := normalizeDeploymentResourceFileInput(request, expectedNamespace)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}
	file, err := s.deps.Store.UpdateDeploymentResourceFile(c.Request.Context(), project.SpaceID, project.ID, c.Param("resourceID"), input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "保存 Kubernetes 资源文件", project.Name+" · "+file.Path)
	c.JSON(http.StatusOK, gin.H{"resource": file})
}

func (s *Server) deleteDeploymentResourceFile(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	if err := s.deps.Store.DeleteDeploymentResourceFile(c.Request.Context(), project.SpaceID, project.ID, c.Param("resourceID")); err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "删除 Kubernetes 资源文件", project.Name+" · "+c.Param("resourceID"))
	c.Status(http.StatusNoContent)
}

func (s *Server) validateDeploymentResourceFile(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	request, err := bindDeploymentResourceFileRequest(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	expectedNamespace, err := s.defaultDeploymentNamespace(c, project)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	input, err := normalizeDeploymentResourceFileInput(request, expectedNamespace)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"resource": domain.DeploymentResourceFile{
		Name: input.Name, Path: input.Path, Format: input.Format, Content: input.Content,
		APIVersion: input.APIVersion, Kind: input.Kind, ResourceName: input.ResourceName, Namespace: input.Namespace,
		ReleaseSupported: input.ReleaseSupported,
	}})
}

func (s *Server) defaultDeploymentNamespace(c *gin.Context, project domain.Project) (string, error) {
	target, err := s.deps.Store.GetDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, "")
	if err != nil {
		return "", err
	}
	return s.generatedDeploymentNamespace(c, project, target.Environment)
}

func bindDeploymentResourceFileRequest(c *gin.Context) (deploymentResourceFileRequest, error) {
	var request deploymentResourceFileRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		return request, errors.New("Kubernetes 资源文件请求格式不正确")
	}
	return request, nil
}

func normalizeDeploymentResourceFileInput(request deploymentResourceFileRequest, expectedNamespace string) (store.SaveDeploymentResourceFileInput, error) {
	name := strings.TrimSpace(request.Name)
	path := strings.TrimSpace(request.Path)
	if name == "" && path != "" {
		name = filepath.Base(path)
	}
	if path == "" && name != "" {
		path = name
	}
	content := strings.TrimSpace(request.Content)
	if strings.TrimSpace(expectedNamespace) != "" {
		var err error
		content, err = deploymentconfig.RetargetNamespace(content, expectedNamespace)
		if err != nil {
			return store.SaveDeploymentResourceFileInput{}, err
		}
	}
	validated, err := deploymentconfig.ValidateResourceFile(content, request.Format, expectedNamespace)
	if err != nil {
		return store.SaveDeploymentResourceFileInput{}, err
	}
	resource := validated.Resources[0]
	return store.SaveDeploymentResourceFileInput{
		Name: name, Path: path, Format: validated.Format, Content: content, SortOrder: request.SortOrder,
		APIVersion: resource.APIVersion, Kind: resource.Kind, ResourceName: resource.Name, Namespace: resource.Namespace,
		ReleaseSupported: deploymentconfig.IsReleaseSupportedKind(resource.Kind),
	}, nil
}
