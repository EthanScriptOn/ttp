package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

// clusterConnectionView is deliberately separate from store.Cluster so the
// API can report a connection result without ever returning kubeconfig data.
type clusterConnectionView struct {
	Cluster    store.Cluster             `json:"cluster"`
	Connected  bool                      `json:"connected"`
	Connection runtime.ClusterConnection `json:"connection,omitempty"`
	Message    string                    `json:"message,omitempty"`
}

func (s *Server) getCluster(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	cluster, err := s.deps.Store.GetCluster(c.Request.Context(), claims.SpaceID, c.Param("clusterID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, cluster)
}

func (s *Server) createCluster(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	var input store.CreateClusterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "集群配置格式不正确")
		return
	}
	cluster, err := s.deps.Store.CreateCluster(c.Request.Context(), claims.SpaceID, input)
	if err != nil {
		writeStoreError(c, err)
		return
	}

	view := s.checkAndPersistCluster(c, cluster)
	s.recordAudit(c, "添加集群", cluster.Name)
	c.JSON(http.StatusCreated, view)
}

func (s *Server) updateCluster(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	var input store.UpdateClusterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "集群配置格式不正确")
		return
	}
	cluster, err := s.deps.Store.UpdateCluster(c.Request.Context(), claims.SpaceID, c.Param("clusterID"), input)
	if err != nil {
		writeStoreError(c, err)
		return
	}

	view := s.checkAndPersistCluster(c, cluster)
	s.recordAudit(c, "更新集群配置", cluster.Name)
	c.JSON(http.StatusOK, view)
}

func (s *Server) testCluster(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	cluster, err := s.deps.Store.GetCluster(c.Request.Context(), claims.SpaceID, c.Param("clusterID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	view := s.checkAndPersistCluster(c, cluster)
	s.recordAudit(c, "测试集群连接", cluster.Name)
	c.JSON(http.StatusOK, view)
}

// checkAndPersistCluster keeps configuration changes usable even when the
// target is temporarily unreachable. The failed connection is represented by
// offline status and a safe UI message; the saved kubeconfig path never leaves
// the server process.
func (s *Server) checkAndPersistCluster(c *gin.Context, cluster store.Cluster) clusterConnectionView {
	view := clusterConnectionView{Cluster: cluster}
	if s.deps.Runtime == nil {
		view.Message = "运行时未配置，集群暂不可用"
		view.Cluster = s.markClusterOffline(c, cluster)
		return view
	}

	ctx := c.Request.Context()
	err := s.registerRuntimeCluster(ctx, cluster)
	if err == nil {
		view.Connection, err = s.deps.Runtime.CheckCluster(ctx, cluster.ID)
	}
	if err != nil {
		view.Message = clusterConnectionMessage(err)
		view.Cluster = s.markClusterOffline(c, cluster)
		return view
	}
	view.Connected = true
	view.Message = "连接成功"
	view.Cluster = s.markClusterActive(c, cluster)
	return view
}

func (s *Server) markClusterOffline(c *gin.Context, cluster store.Cluster) store.Cluster {
	status := "offline"
	updated, err := s.deps.Store.UpdateCluster(c.Request.Context(), cluster.SpaceID, cluster.ID, store.UpdateClusterInput{Status: &status})
	if err != nil {
		return cluster
	}
	return updated
}

func (s *Server) markClusterActive(c *gin.Context, cluster store.Cluster) store.Cluster {
	status := "active"
	updated, err := s.deps.Store.UpdateCluster(c.Request.Context(), cluster.SpaceID, cluster.ID, store.UpdateClusterInput{Status: &status})
	if err != nil {
		return cluster
	}
	return updated
}

func clusterConnectionMessage(err error) string {
	if err == nil {
		return "连接成功"
	}
	if errors.Is(err, runtime.ErrClusterManagementUnsupported) {
		return "当前运行时不支持动态接入集群"
	}
	return "连接失败，请检查 API Server 地址、kubeconfig 路径和 Context"
}
