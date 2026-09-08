package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func (s *Server) listPods(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	target, ok := s.deploymentTargetForProject(c, project)
	if !ok {
		return
	}
	if !s.ensureRuntimeTarget(c, project, target) {
		return
	}
	items, err := s.deps.Runtime.ListPodsInNamespace(c.Request.Context(), target.ClusterID, target.Namespace, project.ID)
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (s *Server) getPod(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	target, ok := s.deploymentTargetForProject(c, project)
	if !ok {
		return
	}
	if !s.ensureRuntimeTarget(c, project, target) {
		return
	}
	item, err := s.deps.Runtime.GetPod(c.Request.Context(), runtime.PodRef{ClusterID: target.ClusterID, Namespace: target.Namespace, Name: c.Param("podName"), ProjectID: project.ID})
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) getPodLogs(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	target, ok := s.deploymentTargetForProject(c, project)
	if !ok {
		return
	}
	if !s.ensureRuntimeTarget(c, project, target) {
		return
	}
	tail := 200
	if raw := c.Query("tail_lines"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 10000 {
			writeError(c, http.StatusBadRequest, "invalid_request", "日志行数不正确")
			return
		}
		tail = parsed
	}
	logs, err := s.deps.Runtime.GetPodLogs(c.Request.Context(), runtime.PodLogRequest{PodRef: runtime.PodRef{ClusterID: target.ClusterID, Namespace: target.Namespace, Name: c.Param("podName"), ProjectID: project.ID}, Container: c.Query("container"), TailLines: tail})
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

func (s *Server) execPodCommand(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	target, ok := s.deploymentTargetForProject(c, project)
	if !ok {
		return
	}
	if !s.ensureRuntimeTarget(c, project, target) {
		return
	}
	var input struct {
		Container string `json:"container"`
		Command   string `json:"command" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "命令不能为空")
		return
	}
	result, err := s.deps.Runtime.ExecPodCommand(c.Request.Context(), runtime.PodExecRequest{
		PodRef:    runtime.PodRef{ClusterID: target.ClusterID, Namespace: target.Namespace, Name: c.Param("podName"), ProjectID: project.ID},
		Container: input.Container,
		Command:   input.Command,
	})
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	s.recordAudit(c, "进入 Pod 终端", project.Name+" · "+c.Param("podName"))
	c.JSON(http.StatusOK, result)
}

func (s *Server) updatePodConfig(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	target, ok := s.deploymentTargetForProject(c, project)
	if !ok {
		return
	}
	if !s.ensureRuntimeTarget(c, project, target) {
		return
	}
	var update runtime.PodConfigUpdate
	if err := c.ShouldBindJSON(&update); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Pod 配置格式不正确")
		return
	}
	item, err := s.deps.Runtime.UpdatePodConfig(c.Request.Context(), runtime.PodRef{ClusterID: target.ClusterID, Namespace: target.Namespace, Name: c.Param("podName"), ProjectID: project.ID}, update)
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	s.recordAudit(c, "修改 Pod 配置", project.Name+" · "+c.Param("podName"))
	c.JSON(http.StatusOK, item)
}

func (s *Server) projectMetrics(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	target, ok := s.deploymentTargetForProject(c, project)
	if !ok {
		return
	}
	if !s.ensureRuntimeTarget(c, project, target) {
		return
	}
	item, err := s.deps.Runtime.GetProjectMetricsInNamespace(c.Request.Context(), target.ClusterID, target.Namespace, project.ID)
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) clusterMetrics(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	clusterID := c.Param("clusterID")
	clusters, err := s.deps.Store.ListClusters(c.Request.Context(), claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	var ownedCluster store.Cluster
	owned := false
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			owned = true
			ownedCluster = cluster
			break
		}
	}
	if !owned {
		writeError(c, http.StatusNotFound, "not_found", "集群不存在")
		return
	}
	if err := s.registerRuntimeCluster(c.Request.Context(), ownedCluster); err != nil {
		writeRuntimeError(c, err)
		return
	}
	item, err := s.deps.Runtime.GetClusterMetrics(c.Request.Context(), clusterID)
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) listClusters(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	items, err := s.deps.Store.ListClusters(c.Request.Context(), claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func writeRuntimeError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "runtime_error"
	message := err.Error()
	switch {
	case errors.Is(err, runtime.ErrProviderNotConfigured):
		status, code, message = http.StatusServiceUnavailable, "runtime_provider_not_configured", "运行时未配置"
	case errors.Is(err, runtime.ErrClusterNotFound), errors.Is(err, runtime.ErrProjectNotFound), errors.Is(err, runtime.ErrPodNotFound), errors.Is(err, runtime.ErrContainerNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, store.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "集群不存在"
	case errors.Is(err, runtime.ErrClusterManagementUnsupported):
		status, code, message = http.StatusNotImplemented, "cluster_management_unsupported", "当前运行时不支持集群动态接入"
	case errors.Is(err, runtime.ErrEnvironmentCleanupUnsupported):
		status, code, message = http.StatusNotImplemented, "environment_cleanup_unsupported", "当前运行时不支持安全清理环境资源"
	case errors.Is(err, runtime.ErrEnvironmentCleanupFailed):
		status, code = http.StatusBadGateway, "environment_cleanup_failed"
	case errors.Is(err, runtime.ErrClusterRegistrationFailed):
		status, code, message = http.StatusBadGateway, "cluster_connection_failed", "集群连接失败，请检查 API Server、kubeconfig 路径和 Context"
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusGatewayTimeout, "provider_timeout", "集群连接超时，请稍后重试"
	}
	writeError(c, status, code, message)
}
