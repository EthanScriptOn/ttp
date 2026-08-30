package release

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
)

// NewHandler adapts Service to a small REST surface. Mount it at /releases or
// use it as a standalone handler; paths are relative to that mount point.
func NewHandler(service *Service) http.Handler {
	if service == nil {
		service = NewService(nil)
	}
	return &handler{service: service}
}

type handler struct{ service *Service }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts, ok := releasePathParts(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}

	switch {
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "releases":
		writeJSON(w, http.StatusOK, h.service.List(r.URL.Query().Get("project_id")))
	case r.Method == http.MethodPost && len(parts) == 1 && parts[0] == "releases":
		var input CreateInput
		if !decodeJSON(w, r, &input) {
			return
		}
		release, duplicate, err := h.service.Create(r.Context(), input)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		status := http.StatusCreated
		if duplicate {
			status = http.StatusOK
		}
		writeJSON(w, status, map[string]any{"release": release, "duplicate": duplicate})
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "releases":
		release, err := h.service.Get(parts[1])
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, release)
	case r.Method == http.MethodPatch && len(parts) == 3 && parts[0] == "releases" && parts[2] == "progress":
		var request struct {
			Progress int    `json:"progress"`
			Stage    string `json:"stage"`
			Message  string `json:"message"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		release, err := h.service.UpdateProgress(parts[1], request.Progress, request.Stage, request.Message)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"release": release})
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "releases" && parts[2] == "cancel":
		var request struct {
			Message string `json:"message"`
		}
		if r.Body != nil && r.ContentLength != 0 && !decodeJSON(w, r, &request) {
			return
		}
		release, err := h.service.Cancel(parts[1], strings.TrimSpace(request.Message))
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"release": release})
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "releases" && parts[2] == "fail":
		var request struct {
			Error string `json:"error"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		release, err := h.service.Fail(parts[1], request.Error)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"release": release})
	case r.Method == http.MethodDelete && len(parts) == 4 && parts[0] == "releases" && parts[2] == "commits":
		release, err := h.service.RemoveCommit(parts[1], parts[3])
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, release)
	case (r.Method == http.MethodPost || r.Method == http.MethodPatch) && len(parts) == 3 && parts[0] == "releases" && parts[2] == "status":
		var request struct {
			Status Status `json:"status"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		release, err := h.service.Transition(parts[1], request.Status)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, release)
	default:
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal_error"
	switch {
	case errors.Is(err, ErrInvalidRelease), errors.Is(err, ErrInvalidStatus):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, ErrInvalidStatusFlow), errors.Is(err, ErrReleaseImmutable), errors.Is(err, ErrLastCommit):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, ErrReleaseNotFound), errors.Is(err, git.ErrRepositoryNotFound), errors.Is(err, git.ErrBranchNotFound), errors.Is(err, git.ErrCommitNotFound):
		status, code = http.StatusNotFound, "not_found"
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

func releasePathParts(path string) ([]string, bool) {
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
