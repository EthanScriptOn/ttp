package api

import (
	"context"
	"sync"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/config"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type recordingClusterRegistrar struct {
	*runtime.DemoProvider
	mu       sync.Mutex
	cluster  string
	path     string
	context  string
	register int
}

func (p *recordingClusterRegistrar) RegisterKubeconfigWithContext(clusterID, kubeconfigPath, kubeContext string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cluster = clusterID
	p.path = kubeconfigPath
	p.context = kubeContext
	p.register++
	return nil
}

func (p *recordingClusterRegistrar) snapshot() (string, string, string, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cluster, p.path, p.context, p.register
}

func TestEnsureRuntimeClusterRestoresPersistedKubeconfig(t *testing.T) {
	data := store.NewMemoryWithFixtures()
	cluster, err := data.CreateCluster(context.Background(), "space-lab", store.CreateClusterInput{
		ID: "persisted-cluster", Name: "持久化集群", Provider: "kubernetes",
		ConnectionMode: store.ClusterConnectionKubeconfig, KubeconfigPath: "/etc/cicd/cluster.config", KubeContext: "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	registrar := &recordingClusterRegistrar{DemoProvider: runtime.NewDemoProvider()}
	server := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   data,
		Runtime: runtime.NewService(registrar),
	})

	if err := server.ensureRuntimeCluster(context.Background(), "space-lab", cluster.ID); err != nil {
		t.Fatal(err)
	}
	gotCluster, gotPath, gotContext, count := registrar.snapshot()
	if gotCluster != cluster.ID || gotPath != "/etc/cicd/cluster.config" || gotContext != "prod" || count != 1 {
		t.Fatalf("registration = (%q, %q, %q, %d), want persisted connection", gotCluster, gotPath, gotContext, count)
	}
}
