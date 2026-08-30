package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// NewHandler returns a standalone runtime HTTP adapter. It is safe to mount
// below /runtime in the main service.
func NewHandler(service *Service) http.Handler {
	if service == nil {
		service = NewService(nil)
	}
	return &handler{service: service}
}

type handler struct{ service *Service }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts, ok := runtimePathParts(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	ctx := r.Context()
	switch {
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "clusters" && parts[2] == "pods":
		pods, err := h.service.ListPods(ctx, parts[1], r.URL.Query().Get("project_id"))
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pods)
	case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "clusters" && parts[2] == "pods":
		pod, err := h.service.GetPod(ctx, PodRef{ClusterID: parts[1], Namespace: parts[3], Name: parts[4]})
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pod)
	case r.Method == http.MethodGet && len(parts) == 6 && parts[0] == "clusters" && parts[2] == "pods" && parts[5] == "logs":
		tailLines := 0
		if raw := r.URL.Query().Get("tail_lines"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 || parsed > 10000 {
				writeError(w, http.StatusBadRequest, "invalid_request", "tail_lines must be between 0 and 10000")
				return
			}
			tailLines = parsed
		}
		logs, err := h.service.GetPodLogs(ctx, PodLogRequest{PodRef: PodRef{ClusterID: parts[1], Namespace: parts[3], Name: parts[4]}, Container: r.URL.Query().Get("container"), TailLines: tailLines})
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
	case (r.Method == http.MethodPatch || r.Method == http.MethodPut) && len(parts) == 6 && parts[0] == "clusters" && parts[2] == "pods" && parts[5] == "config":
		var update PodConfigUpdate
		if !decodeJSON(w, r, &update) {
			return
		}
		pod, err := h.service.UpdatePodConfig(ctx, PodRef{ClusterID: parts[1], Namespace: parts[3], Name: parts[4]}, update)
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pod)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "clusters" && parts[2] == "metrics":
		metrics, err := h.service.GetClusterMetrics(ctx, parts[1])
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, metrics)
	case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "clusters" && parts[2] == "projects" && parts[4] == "metrics":
		metrics, err := h.service.GetProjectMetrics(ctx, parts[1], parts[3])
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, metrics)
	default:
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (h *handler) writeProviderError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "provider_error"
	switch {
	case errors.Is(err, ErrClusterNotFound), errors.Is(err, ErrProjectNotFound), errors.Is(err, ErrPodNotFound), errors.Is(err, ErrContainerNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusRequestTimeout, "request_cancelled"
	}
	writeError(w, status, code, err.Error())
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain one JSON value")
		return false
	}
	return true
}

func runtimePathParts(path string) ([]string, bool) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil, false
	}
	rawParts := strings.Split(trimmed, "/")
	parts := make([]string, len(rawParts))
	for i, raw := range rawParts {
		part, err := url.PathUnescape(raw)
		if err != nil || part == "" {
			return nil, false
		}
		parts[i] = part
	}
	return parts, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
