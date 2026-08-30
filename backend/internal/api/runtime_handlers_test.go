package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/config"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type clusterListingStore struct {
	store.Store
	space    domain.Space
	clusters []store.Cluster
}

func (s clusterListingStore) Space(_ context.Context, _ uint64, _ string) (domain.Space, error) {
	return s.space, nil
}

func (s clusterListingStore) ListClusters(_ context.Context, spaceID string) ([]store.Cluster, error) {
	if spaceID != s.space.ID {
		return nil, store.ErrNotFound
	}
	return s.clusters, nil
}

func TestListClustersUsesSpaceStore(t *testing.T) {
	const spaceID = "space-test"
	data := clusterListingStore{
		space: domain.Space{ID: spaceID, Name: "测试空间"},
		clusters: []store.Cluster{{
			ID: "persisted-cluster", SpaceID: spaceID, Name: "持久化集群", Provider: "kubernetes", Status: "active",
		}},
	}
	server := New(Dependencies{
		Config: config.Config{JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:  data,
	})
	user := domain.User{ID: 7, Username: "tester", IsSuperAdmin: true}
	token, _, err := auth.NewManager("test-secret", 60).Issue(user, spaceID, "admin")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list clusters: expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Items []store.Cluster `json:"items"`
		Total int             `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 1 || len(body.Items) != 1 || body.Items[0].ID != "persisted-cluster" {
		t.Fatalf("unexpected clusters response: %#v", body)
	}
}

func TestClusterMetricsRejectsClusterOutsideCurrentSpace(t *testing.T) {
	const spaceID = "space-test"
	data := clusterListingStore{
		space:    domain.Space{ID: spaceID, Name: "测试空间"},
		clusters: []store.Cluster{{ID: "owned-cluster", SpaceID: spaceID, Name: "本空间集群"}},
	}
	server := New(Dependencies{
		Config: config.Config{JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:  data,
	})
	user := domain.User{ID: 7, Username: "tester", IsSuperAdmin: true}
	token, _, err := auth.NewManager("test-secret", 60).Issue(user, spaceID, "admin")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/clusters/foreign-cluster/metrics", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign cluster metrics: expected 404, got %d: %s", response.Code, response.Body.String())
	}
}
