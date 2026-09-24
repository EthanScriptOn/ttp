package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/release"
)

func TestListReleasesSupportsPagination(t *testing.T) {
	server := testServer()
	for index := 0; index < 12; index++ {
		_, _, err := server.deps.Release.Create(context.Background(), release.CreateInput{
			SpaceID: "space-lab", ProjectID: "reverse-lab", RepositoryID: "demo-repo",
			Branch: "main", Name: fmt.Sprintf("release-%02d", index), CreatedBy: 1, ForceNew: true,
			Strategy: release.StrategyRolling,
			Traffic:  release.TrafficSplit{StablePercent: 100},
		})
		if err != nil {
			t.Fatalf("create release %d: %v", index, err)
		}
	}

	handler := server.Router()
	token := loginForTest(t, handler)
	response := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/releases?page=2&page_size=5", token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("list paginated releases: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Items    []release.Release `json:"items"`
		Total    int               `json:"total"`
		Page     int               `json:"page"`
		PageSize int               `json:"page_size"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 5 || body.Total != 12 || body.Page != 2 || body.PageSize != 5 {
		t.Fatalf("unexpected pagination response: items=%d total=%d page=%d page_size=%d", len(body.Items), body.Total, body.Page, body.PageSize)
	}

	searched := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/releases?page=1&page_size=10&q=release-11", token, "")
	if searched.Code != http.StatusOK {
		t.Fatalf("search releases: %d %s", searched.Code, searched.Body.String())
	}
	if err := json.Unmarshal(searched.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Total != 1 || body.Items[0].Name != "release-11" {
		t.Fatalf("unexpected search response: items=%d total=%d first=%q", len(body.Items), body.Total, body.Items[0].Name)
	}

	byUsername := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/releases?page=1&page_size=10&q=admin", token, "")
	if byUsername.Code != http.StatusOK {
		t.Fatalf("search releases by creator username: %d %s", byUsername.Code, byUsername.Body.String())
	}
	if err := json.Unmarshal(byUsername.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 10 || body.Total != 12 {
		t.Fatalf("unexpected creator username search response: items=%d total=%d", len(body.Items), body.Total)
	}

	invalid := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/releases?page=0", token, "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid page: got %d, want %d", invalid.Code, http.StatusBadRequest)
	}
}
