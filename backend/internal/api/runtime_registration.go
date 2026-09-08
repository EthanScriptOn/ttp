package api

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

// ensureRuntimeCluster restores a persisted cluster connection lazily. The
// Kubernetes client registry is process-local, while the store is durable; a
// lazy registration means a service restart does not require an operator to
// revisit the cluster-management page before reading or deploying a project.
func (s *Server) ensureRuntimeCluster(ctx context.Context, spaceID, clusterID string) error {
	if s.deps.Store == nil {
		return store.ErrNotFound
	}
	cluster, err := s.deps.Store.GetCluster(ctx, spaceID, clusterID)
	if err != nil {
		return err
	}
	return s.registerRuntimeCluster(ctx, cluster)
}

func (s *Server) registerRuntimeCluster(ctx context.Context, cluster store.Cluster) error {
	if s.deps.Runtime == nil {
		return runtime.ErrClusterManagementUnsupported
	}
	if !strings.EqualFold(cluster.ConnectionMode, store.ClusterConnectionInCluster) && strings.TrimSpace(cluster.KubeconfigPath) == "" {
		// An empty path is the supported form for a cluster registered from
		// process configuration.
		return nil
	}

	var err error
	if strings.EqualFold(cluster.ConnectionMode, store.ClusterConnectionInCluster) {
		err = s.deps.Runtime.RegisterCluster(ctx, cluster.ID, store.ClusterConnectionInCluster, "", "")
	} else {
		err = s.deps.Runtime.RegisterCluster(ctx, cluster.ID, store.ClusterConnectionKubeconfig, cluster.KubeconfigPath, cluster.KubeContext)
	}
	// Observation-only providers can already have a client registered at
	// startup. They do not need dynamic registration for this operation.
	if errors.Is(err, runtime.ErrClusterManagementUnsupported) {
		return nil
	}
	return err
}

func (s *Server) ensureRuntimeTarget(c *gin.Context, project domain.Project, target domain.DeploymentTarget) bool {
	if err := s.ensureRuntimeCluster(c.Request.Context(), project.SpaceID, target.ClusterID); err != nil {
		writeRuntimeError(c, err)
		return false
	}
	return true
}
