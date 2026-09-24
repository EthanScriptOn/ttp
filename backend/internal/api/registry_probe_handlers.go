package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

var registryProbeTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

type registryTagResponse struct {
	Tags []string `json:"tags"`
}

type registryCatalogResponse struct {
	Repositories []string `json:"repositories"`
}

func (s *Server) testRegistryPull(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	cluster, err := s.deps.Store.GetCluster(c.Request.Context(), claims.SpaceID, c.Param("clusterID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	connection, err := s.deps.Store.GetImageRegistryConnection(c.Request.Context(), claims.SpaceID, c.Param("connectionID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	credential, err := s.openImageRegistryCredential(connection)
	if err != nil {
		s.recordAudit(c, "测试集群拉取镜像", cluster.Name+" · "+connection.Name)
		writeRegistryPullTestError(c, fmt.Errorf("镜像仓库凭证不可用"))
		return
	}
	if err := s.registerRuntimeCluster(c.Request.Context(), cluster); err != nil {
		writeRuntimeError(c, err)
		return
	}
	image, err := s.registryProbeImage(c.Request.Context(), claims.SpaceID, connection, credential)
	if err != nil {
		s.recordAudit(c, "测试集群拉取镜像", cluster.Name+" · "+connection.Name)
		writeRegistryPullTestError(c, err)
		return
	}
	result, err := s.deps.Runtime.TestRegistryPull(c.Request.Context(), cluster.ID, runtime.RegistryPullTestRequest{
		Image:      image,
		Registry:   connection.Registry,
		AuthType:   connection.AuthType,
		Username:   credential.Username,
		Secret:     credential.Secret,
		SecretName: connection.PullSecretName,
	})
	if err != nil {
		s.recordAudit(c, "测试集群拉取镜像", cluster.Name+" · "+connection.Name)
		writeRegistryPullTestError(c, err)
		return
	}
	s.recordAudit(c, "测试集群拉取镜像", cluster.Name+" · "+connection.Name)
	c.JSON(http.StatusOK, gin.H{"result": result, "message": result.Message, "image": result.Image})
}

func (s *Server) registryProbeImage(ctx context.Context, spaceID string, connection domain.ImageRegistryConnection, credential imageRegistryCredentialPayload) (string, error) {
	projects, err := s.deps.Store.ListProjects(ctx, spaceID)
	if err != nil {
		return "", fmt.Errorf("读取项目镜像配置失败：%w", err)
	}
	host := strings.ToLower(strings.TrimSpace(connection.Registry))
	candidates := make([]string, 0, len(projects))
	seen := make(map[string]struct{})
	for _, project := range projects {
		if strings.TrimSpace(project.RegistryConnectionID) != connection.ID {
			continue
		}
		repository := strings.TrimSpace(project.ImageRepository)
		if repository == "" {
			repository, err = imagebuild.ImageRepositoryForProject(connection.Registry, project.ID)
			if err != nil {
				continue
			}
		}
		repositoryHost, hostErr := imagebuild.RegistryHost(repository)
		if hostErr != nil || repositoryHost != host {
			continue
		}
		path := strings.TrimPrefix(repository, repositoryHost+"/")
		if path == repository || path == "" {
			continue
		}
		if _, exists := seen[path]; !exists {
			seen[path] = struct{}{}
			candidates = append(candidates, path)
		}
	}

	for _, repository := range candidates {
		tag, tagErr := s.registryProbeTag(ctx, connection, credential, repository)
		if tagErr == nil {
			return host + "/" + repository + ":" + tag, nil
		}
	}

	catalog, catalogErr := s.registryProbeCatalog(ctx, connection, credential)
	if catalogErr == nil {
		for _, repository := range catalog {
			if _, exists := seen[repository]; exists {
				continue
			}
			tag, tagErr := s.registryProbeTag(ctx, connection, credential, repository)
			if tagErr == nil {
				return host + "/" + repository + ":" + tag, nil
			}
		}
	}
	if catalogErr != nil {
		return "", fmt.Errorf("镜像仓库没有可用于拉取测试的镜像：无法读取仓库目录（%v）", catalogErr)
	}
	return "", fmt.Errorf("镜像仓库没有可用于拉取测试的镜像，请先向该仓库推送一个项目镜像")
}

func (s *Server) registryProbeTag(ctx context.Context, connection domain.ImageRegistryConnection, credential imageRegistryCredentialPayload, repository string) (string, error) {
	response, err := registryProbeRequest(ctx, connection, credential, "/"+repository+"/tags/list")
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("读取镜像标签失败（HTTP %d）", response.StatusCode)
	}
	var payload registryTagResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return "", fmt.Errorf("解析镜像标签失败：%w", err)
	}
	for index := len(payload.Tags) - 1; index >= 0; index-- {
		if registryProbeTagPattern.MatchString(payload.Tags[index]) {
			return payload.Tags[index], nil
		}
	}
	return "", errors.New("镜像仓库没有可用标签")
}

func (s *Server) registryProbeCatalog(ctx context.Context, connection domain.ImageRegistryConnection, credential imageRegistryCredentialPayload) ([]string, error) {
	response, err := registryProbeRequest(ctx, connection, credential, "/_catalog?n=100")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("读取镜像仓库目录失败（HTTP %d）", response.StatusCode)
	}
	var payload registryCatalogResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("解析镜像仓库目录失败：%w", err)
	}
	return payload.Repositories, nil
}

func registryProbeRequest(ctx context.Context, connection domain.ImageRegistryConnection, credential imageRegistryCredentialPayload, path string) (*http.Response, error) {
	scheme := "https"
	if isLocalRegistryHost(connection.Registry) {
		scheme = "http"
	}
	requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, scheme+"://"+strings.TrimSpace(connection.Registry)+"/v2"+path, nil)
	if err != nil {
		return nil, err
	}
	if connection.AuthType == "token" {
		req.Header.Set("Authorization", "Bearer "+credential.Secret)
	} else {
		req.SetBasicAuth(credential.Username, credential.Secret)
	}
	return (&http.Client{Timeout: 15 * time.Second}).Do(req)
}

func writeRegistryPullTestError(c *gin.Context, err error) {
	status, code := http.StatusBadGateway, "registry_pull_test_failed"
	message := "K3s 节点镜像拉取测试失败"
	switch {
	case errors.Is(err, runtime.ErrRegistryPullTestUnsupported):
		status, code, message = http.StatusNotImplemented, "registry_pull_test_unsupported", "当前运行时不支持集群节点镜像拉取测试"
	case errors.Is(err, runtime.ErrProviderNotConfigured):
		status, code, message = http.StatusServiceUnavailable, "runtime_provider_not_configured", "运行时未配置"
	case errors.Is(err, runtime.ErrClusterNotFound), errors.Is(err, store.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "目标集群不存在"
	case errors.Is(err, runtime.ErrInvalidKubernetesInput):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusGatewayTimeout, "provider_timeout", "K3s 节点镜像拉取测试超时"
	default:
		message = err.Error()
	}
	writeError(c, status, code, message)
}
