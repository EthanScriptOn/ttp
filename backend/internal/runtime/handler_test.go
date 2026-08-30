package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerListsPodsAndMetrics(t *testing.T) {
	handler := NewHandler(nil)
	request := httptest.NewRequest(http.MethodGet, "/clusters/demo-cluster/pods?project_id=reverse-lab", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected pod list 200, got %d", recorder.Code)
	}
	var pods []Pod
	if err := json.Unmarshal(recorder.Body.Bytes(), &pods); err != nil {
		t.Fatal(err)
	}
	if len(pods) != 2 {
		t.Fatalf("expected two pods, got %d", len(pods))
	}

	request = httptest.NewRequest(http.MethodGet, "/clusters/demo-cluster/metrics", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected metrics 200, got %d", recorder.Code)
	}
}

func TestHandlerUpdatesPodConfig(t *testing.T) {
	body := `{"config":{"LOG_LEVEL":"debug"},"environment":{"TRACE":"1"}}`
	request := httptest.NewRequest(http.MethodPatch, "/clusters/demo-cluster/pods/lab/reverse-lab-api-7d9f8c6d4b-x2k9m/config", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	NewHandler(nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var detail PodDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Config["LOG_LEVEL"] != "debug" || detail.Environment["TRACE"] != "1" {
		t.Fatalf("unexpected updated detail: %#v", detail)
	}
}

func TestHandlerMapsUnknownCluster(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/clusters/missing/metrics", nil)
	recorder := httptest.NewRecorder()
	NewHandler(nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}
