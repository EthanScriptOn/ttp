package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/deploymentconfig"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type deploymentResourceFileRequest struct {
	Name              string `json:"name"`
	Path              string `json:"path"`
	Format            string `json:"format"`
	Content           string `json:"content"`
	SortOrder         int    `json:"sort_order"`
	TargetID          string `json:"target_id"`
	GlobalResourceID  string `json:"global_resource_id"`
	BaseGlobalVersion int    `json:"base_global_version"`
}

func (s *Server) listDeploymentResourceFiles(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	targetID := strings.TrimSpace(c.Query("target_id"))
	files, err := s.deploymentResourceFilesForTarget(c.Request.Context(), project, targetID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"resources": files})
}

func (s *Server) createDeploymentResourceOverride(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	request, err := bindDeploymentResourceFileRequest(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	targetID := strings.TrimSpace(request.TargetID)
	target, err := s.deps.Store.GetDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, targetID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	input, err := normalizeDeploymentResourceFileInput(request, target.Namespace)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}
	overrideInput := deploymentResourceOverrideInput(input, request.GlobalResourceID)
	if overrideInput.GlobalResourceID != "" {
		global, found, lookupErr := s.globalDeploymentResource(c.Request.Context(), project, overrideInput.GlobalResourceID)
		if lookupErr != nil {
			writeStoreError(c, lookupErr)
			return
		}
		if !found {
			writeStoreError(c, store.ErrNotFound)
			return
		}
		overrideInput.BaseGlobalVersion = global.Version
		overrideInput.BaseGlobalContent = global.Content
	}
	override, err := s.deps.Store.CreateDeploymentResourceOverride(c.Request.Context(), project.SpaceID, project.ID, target.ID, overrideInput)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "创建环境 Kubernetes 资源覆盖", project.Name+" · "+target.Name+" · "+override.Path)
	c.JSON(http.StatusCreated, gin.H{"resource": deploymentResourceFileFromOverride(override, target.Name)})
}

func (s *Server) updateDeploymentResourceOverride(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	request, err := bindDeploymentResourceFileRequest(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	target, err := s.deps.Store.GetDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, strings.TrimSpace(request.TargetID))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	input, err := normalizeDeploymentResourceFileInput(request, target.Namespace)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}
	overrideInput := deploymentResourceOverrideInput(input, request.GlobalResourceID)
	if request.BaseGlobalVersion > 0 && overrideInput.GlobalResourceID != "" {
		global, found, lookupErr := s.globalDeploymentResource(c.Request.Context(), project, overrideInput.GlobalResourceID)
		if lookupErr != nil {
			writeStoreError(c, lookupErr)
			return
		}
		if !found || request.BaseGlobalVersion != global.Version {
			writeError(c, http.StatusConflict, "deployment_resource_global_changed", "全局资源文件已再次更新，请重新生成合并结果")
			return
		}
		overrideInput.BaseGlobalVersion = global.Version
		overrideInput.BaseGlobalContent = global.Content
	}
	override, err := s.deps.Store.UpdateDeploymentResourceOverride(c.Request.Context(), project.SpaceID, project.ID, target.ID, c.Param("overrideID"), overrideInput)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "保存环境 Kubernetes 资源覆盖", project.Name+" · "+target.Name+" · "+override.Path)
	c.JSON(http.StatusOK, gin.H{"resource": deploymentResourceFileFromOverride(override, target.Name)})
}

func (s *Server) deleteDeploymentResourceOverride(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	targetID := strings.TrimSpace(c.Query("target_id"))
	if err := s.deps.Store.DeleteDeploymentResourceOverride(c.Request.Context(), project.SpaceID, project.ID, targetID, c.Param("overrideID")); err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "删除环境 Kubernetes 资源覆盖", project.Name+" · "+targetID+" · "+c.Param("overrideID"))
	c.Status(http.StatusNoContent)
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
	expectedNamespace, err := s.deploymentNamespaceForTarget(c, project, request.TargetID)
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

func (s *Server) deploymentNamespaceForTarget(c *gin.Context, project domain.Project, targetID string) (string, error) {
	if strings.TrimSpace(targetID) == "" {
		return s.defaultDeploymentNamespace(c, project)
	}
	target, err := s.deps.Store.GetDeploymentTarget(c.Request.Context(), project.SpaceID, project.ID, strings.TrimSpace(targetID))
	if err != nil {
		return "", err
	}
	return target.Namespace, nil
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

func deploymentResourceOverrideInput(input store.SaveDeploymentResourceFileInput, globalResourceID string) store.SaveDeploymentResourceOverrideInput {
	return store.SaveDeploymentResourceOverrideInput{
		GlobalResourceID: strings.TrimSpace(globalResourceID), Name: input.Name, Path: input.Path, Format: input.Format,
		Content: input.Content, SortOrder: input.SortOrder, APIVersion: input.APIVersion, Kind: input.Kind,
		ResourceName: input.ResourceName, Namespace: input.Namespace, ReleaseSupported: input.ReleaseSupported,
	}
}

func (s *Server) globalDeploymentResource(ctx context.Context, project domain.Project, resourceID string) (domain.DeploymentResourceFile, bool, error) {
	files, err := s.deps.Store.ListDeploymentResourceFiles(ctx, project.SpaceID, project.ID)
	if err != nil {
		return domain.DeploymentResourceFile{}, false, err
	}
	for _, file := range files {
		if file.ID == resourceID {
			return file, true, nil
		}
	}
	return domain.DeploymentResourceFile{}, false, nil
}

func (s *Server) deploymentResourceFilesForTarget(ctx context.Context, project domain.Project, targetID string) ([]domain.DeploymentResourceFile, error) {
	globalFiles, err := s.deps.Store.ListDeploymentResourceFiles(ctx, project.SpaceID, project.ID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(targetID) == "" {
		for index := range globalFiles {
			globalFiles[index].Scope = "global"
			globalFiles[index].GlobalResourceID = globalFiles[index].ID
			globalFiles[index].GlobalVersion = globalFiles[index].Version
		}
		return globalFiles, nil
	}
	target, err := s.deps.Store.GetDeploymentTarget(ctx, project.SpaceID, project.ID, targetID)
	if err != nil {
		return nil, err
	}
	overrides, err := s.deps.Store.ListDeploymentResourceOverrides(ctx, project.SpaceID, project.ID, target.ID)
	if err != nil {
		return nil, err
	}
	overridesByGlobal := make(map[string]domain.DeploymentResourceOverride)
	environmentOnly := make([]domain.DeploymentResourceOverride, 0)
	for _, override := range overrides {
		if override.GlobalResourceID == "" {
			environmentOnly = append(environmentOnly, override)
			continue
		}
		overridesByGlobal[override.GlobalResourceID] = override
	}
	result := make([]domain.DeploymentResourceFile, 0, len(globalFiles)+len(environmentOnly))
	for _, global := range globalFiles {
		globalContent, err := retargetResourceContent(global.Content, target.Namespace)
		if err != nil {
			return nil, err
		}
		if override, exists := overridesByGlobal[global.ID]; exists {
			file := deploymentResourceFileFromOverride(override, target.Name)
			file.ID = global.ID
			file.GlobalResourceID = global.ID
			file.GlobalVersion = global.Version
			file.GlobalContent = globalContent
			file.BaseContent, err = retargetResourceContent(override.BaseGlobalContent, target.Namespace)
			if err != nil {
				return nil, err
			}
			file.Content, err = retargetResourceContent(override.Content, target.Namespace)
			if err != nil {
				return nil, err
			}
			globalVersionChanged := override.BaseGlobalVersion != global.Version
			file.GlobalChanged = globalVersionChanged && file.Content != file.GlobalContent
			if globalVersionChanged && !file.GlobalChanged {
				file.BaseGlobalVersion = global.Version
				file.BaseContent = file.GlobalContent
			}
			result = append(result, file)
			continue
		}
		global.Content = globalContent
		global.Namespace = target.Namespace
		global.Scope = "inherited"
		global.TargetID = target.ID
		global.GlobalResourceID = global.ID
		global.GlobalVersion = global.Version
		global.GlobalContent = globalContent
		result = append(result, global)
	}
	for _, override := range environmentOnly {
		file := deploymentResourceFileFromOverride(override, target.Name)
		file.Content, err = retargetResourceContent(override.Content, target.Namespace)
		if err != nil {
			return nil, err
		}
		result = append(result, file)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SortOrder == result[j].SortOrder {
			return result[i].Path < result[j].Path
		}
		return result[i].SortOrder < result[j].SortOrder
	})
	return result, nil
}

func deploymentResourceFileFromOverride(override domain.DeploymentResourceOverride, environmentLabel string) domain.DeploymentResourceFile {
	scope := "environment"
	if override.GlobalResourceID != "" {
		scope = "overridden"
	}
	return domain.DeploymentResourceFile{
		ID: override.ID, ProjectID: override.ProjectID, Name: override.Name, Path: override.Path, Format: override.Format,
		Content: override.Content, APIVersion: override.APIVersion, Kind: override.Kind, ResourceName: override.ResourceName,
		Namespace: override.Namespace, SortOrder: override.SortOrder, Version: override.Version, ReleaseSupported: override.ReleaseSupported,
		UpdatedAt: override.UpdatedAt, Scope: scope, TargetID: override.TargetID, GlobalResourceID: override.GlobalResourceID,
		OverrideID: override.ID, BaseGlobalVersion: override.BaseGlobalVersion,
	}
}

func retargetResourceContent(content, namespace string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return content, nil
	}
	return deploymentconfig.RetargetNamespace(content, namespace)
}
