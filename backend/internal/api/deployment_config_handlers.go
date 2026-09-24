package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/deploymentconfig"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type deploymentConfigRequest struct {
	Manifest string `json:"manifest"`
	Format   string `json:"format"`
}

func (s *Server) getDeploymentConfig(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	config, validation, err := s.resolveDeploymentConfig(c, project)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}

	targetFormat := strings.TrimSpace(c.Query("format"))
	if targetFormat != "" && strings.TrimSpace(config.Manifest) != "" && !strings.EqualFold(targetFormat, config.Format) {
		converted, convertedValidation, convertErr := deploymentconfig.Convert(config.Manifest, targetFormat, config.Namespace)
		if convertErr != nil {
			writeDeploymentConfigError(c, convertErr)
			return
		}
		config.Manifest = converted
		config.Format = convertedValidation.Format
		validation = convertedValidation
	}
	writeDeploymentConfig(c, config, validation)
}

func (s *Server) saveDeploymentConfig(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	var request deploymentConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "部署配置请求格式不正确")
		return
	}
	manifest := strings.TrimSpace(request.Manifest)
	format, err := deploymentconfig.NormalizeFormat(request.Format)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}
	if strings.TrimSpace(request.Format) == "" {
		format = deploymentconfig.DetectFormat(manifest)
	}
	if format == "json" && deploymentconfig.DetectFormat(manifest) != "json" {
		writeError(c, http.StatusBadRequest, "invalid_manifest", "当前内容不是有效的 JSON，请切换为 YAML 或先格式化")
		return
	}
	validated, err := deploymentconfig.Validate(manifest, project.Namespace)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}
	saved, err := s.deps.Store.SaveDeploymentConfig(c.Request.Context(), project.SpaceID, project.ID, store.SaveDeploymentConfigInput{Manifest: manifest, Format: format})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "保存部署配置", project.Name)
	writeDeploymentConfig(c, saved, validated)
}

func (s *Server) validateDeploymentConfig(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	var request deploymentConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "部署配置请求格式不正确")
		return
	}
	validated, err := deploymentconfig.Validate(strings.TrimSpace(request.Manifest), project.Namespace)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}
	writeDeploymentConfig(c, domain.DeploymentConfig{ProjectID: project.ID, Namespace: project.Namespace, Format: validated.Format, Manifest: strings.TrimSpace(request.Manifest)}, validated)
}

func (s *Server) convertDeploymentConfig(c *gin.Context) {
	project, ok := s.deploymentProjectForRequest(c)
	if !ok {
		return
	}
	var request deploymentConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "部署配置转换请求格式不正确")
		return
	}
	manifest, validated, err := deploymentconfig.Convert(strings.TrimSpace(request.Manifest), request.Format, project.Namespace)
	if err != nil {
		writeDeploymentConfigError(c, err)
		return
	}
	writeDeploymentConfig(c, domain.DeploymentConfig{ProjectID: project.ID, Namespace: project.Namespace, Format: validated.Format, Manifest: manifest}, validated)
}

func (s *Server) deploymentProjectForRequest(c *gin.Context) (domain.Project, bool) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return domain.Project{}, false
	}
	project, err := s.deps.Store.GetProject(c.Request.Context(), claims.SpaceID, c.Param("projectID"))
	if err != nil {
		writeStoreError(c, err)
		return domain.Project{}, false
	}
	return project, true
}

func (s *Server) resolveDeploymentConfig(c *gin.Context, project domain.Project) (domain.DeploymentConfig, deploymentconfig.Validation, error) {
	return s.resolveDeploymentConfigWithContext(c.Request.Context(), project)
}

func (s *Server) resolveDeploymentConfigWithContext(ctx context.Context, project domain.Project) (domain.DeploymentConfig, deploymentconfig.Validation, error) {
	target, err := s.deps.Store.GetDeploymentTarget(ctx, project.SpaceID, project.ID, "")
	if err != nil {
		return domain.DeploymentConfig{}, deploymentconfig.Validation{}, err
	}
	return s.resolveDeploymentConfigForTargetWithContext(ctx, project, target)
}

func (s *Server) resolveDeploymentConfigForTargetWithContext(ctx context.Context, project domain.Project, target domain.DeploymentTarget) (domain.DeploymentConfig, deploymentconfig.Validation, error) {
	resourceFiles, filesErr := s.deploymentResourceFilesForTarget(ctx, project, target.ID)
	if filesErr == nil && len(resourceFiles) > 0 {
		resolved := domain.DeploymentConfig{ProjectID: project.ID, Namespace: target.Namespace, Format: "yaml", Version: 1, Files: make([]domain.DeploymentResourceFile, 0, len(resourceFiles))}
		validated := deploymentconfig.Validation{Format: "yaml", Resources: make([]deploymentconfig.Resource, 0, len(resourceFiles))}
		for _, file := range resourceFiles {
			content := file.Content
			if strings.TrimSpace(content) != "" {
				retargeted, err := deploymentconfig.RetargetNamespace(content, target.Namespace)
				if err != nil {
					return domain.DeploymentConfig{}, deploymentconfig.Validation{}, err
				}
				content = retargeted
			}
			file.Content = content
			file.Namespace = target.Namespace
			fileValidation, err := deploymentconfig.ValidateResourceFile(content, file.Format, target.Namespace)
			if err != nil {
				return domain.DeploymentConfig{}, deploymentconfig.Validation{}, fmt.Errorf("资源文件 %s 校验失败：%w", file.Path, err)
			}
			file.APIVersion = fileValidation.Resources[0].APIVersion
			file.Kind = fileValidation.Resources[0].Kind
			file.ResourceName = fileValidation.Resources[0].Name
			file.ReleaseSupported = deploymentconfig.IsReleaseSupportedKind(file.Kind)
			resolved.Files = append(resolved.Files, file)
			if file.Version > resolved.Version {
				resolved.Version = file.Version
			}
			validated.Resources = append(validated.Resources, fileValidation.Resources...)
		}
		resolved.ResourceCount = len(validated.Resources)
		resolved.Resources = deploymentResources(validated.Resources)
		resolved.Capabilities = deploymentCapabilities(validated.Resources)
		resolved.IsDefault = false
		return resolved, validated, nil
	}
	if filesErr != nil && !errors.Is(filesErr, store.ErrNotFound) {
		return domain.DeploymentConfig{}, deploymentconfig.Validation{}, filesErr
	}
	stored, err := s.deps.Store.GetDeploymentConfig(ctx, project.SpaceID, project.ID)
	if errors.Is(err, store.ErrNotFound) {
		// An empty configuration is a valid project setup state. Return it so
		// the editor can create the first manifest, but never invent an image or
		// Kubernetes resource on the user's behalf.
		stored = domain.DeploymentConfig{
			ProjectID: project.ID,
			Namespace: target.Namespace,
		}
	} else if err != nil {
		return domain.DeploymentConfig{}, deploymentconfig.Validation{}, err
	}
	manifest := stored.Manifest
	if strings.TrimSpace(manifest) != "" {
		manifest, err = deploymentconfig.RetargetNamespace(manifest, target.Namespace)
		if err != nil {
			return domain.DeploymentConfig{}, deploymentconfig.Validation{}, err
		}
	}
	if strings.TrimSpace(manifest) == "" {
		stored.Manifest = ""
		stored.Namespace = target.Namespace
		stored.IsDefault = false
		return stored, deploymentconfig.Validation{}, nil
	}
	validated, err := deploymentconfig.Validate(manifest, target.Namespace)
	if err != nil {
		return domain.DeploymentConfig{}, deploymentconfig.Validation{}, err
	}
	stored.Manifest = manifest
	stored.Namespace = target.Namespace
	stored.Format = normalizedStoredFormat(stored.Format, validated.Format)
	stored.ResourceCount = len(validated.Resources)
	stored.Resources = deploymentResources(validated.Resources)
	stored.IsDefault = false
	return stored, validated, nil
}

func normalizedStoredFormat(value, fallback string) string {
	format, err := deploymentconfig.NormalizeFormat(value)
	if err != nil || format == "yaml" && fallback == "json" {
		return fallback
	}
	return format
}

func writeDeploymentConfig(c *gin.Context, config domain.DeploymentConfig, validated deploymentconfig.Validation) {
	config.ResourceCount = len(validated.Resources)
	config.Resources = deploymentResources(validated.Resources)
	config.Capabilities = deploymentCapabilities(validated.Resources)
	if config.Format == "" {
		config.Format = validated.Format
	}
	c.JSON(http.StatusOK, gin.H{"config": config})
}

func deploymentResources(items []deploymentconfig.Resource) []domain.DeploymentResource {
	result := make([]domain.DeploymentResource, 0, len(items))
	for _, item := range items {
		result = append(result, domain.DeploymentResource{APIVersion: item.APIVersion, Kind: item.Kind, Name: item.Name, Namespace: item.Namespace, ReleaseSupported: deploymentconfig.IsReleaseSupportedKind(item.Kind)})
	}
	return result
}

func deploymentCapabilities(items []deploymentconfig.Resource) domain.DeploymentConfigCapabilities {
	capabilities := domain.DeploymentConfigCapabilities{SupportedKinds: deploymentconfig.ReleaseSupportedKinds(), UnsupportedResources: []domain.DeploymentResource{}}
	for _, resource := range deploymentResources(items) {
		if !resource.ReleaseSupported {
			capabilities.UnsupportedResources = append(capabilities.UnsupportedResources, resource)
		}
	}
	return capabilities
}

func writeDeploymentConfigError(c *gin.Context, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeStoreError(c, err)
		return
	}
	writeError(c, http.StatusBadRequest, "invalid_manifest", err.Error())
}

func deploymentConfigReady(config domain.DeploymentConfig) bool {
	return config.Version >= 1 && (len(config.Files) > 0 || strings.TrimSpace(config.Manifest) != "")
}

func deploymentResourceContents(files []domain.DeploymentResourceFile) []string {
	result := make([]string, 0, len(files))
	for _, file := range files {
		if strings.TrimSpace(file.Content) != "" {
			result = append(result, file.Content)
		}
	}
	return result
}
