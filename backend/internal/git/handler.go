package git

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// NewHandler returns a standalone handler. It can be mounted at /git by a
// main service or served directly in a small development process.
func NewHandler(provider Provider) http.Handler {
	if provider == nil {
		provider = NewDemoProvider()
	}
	return &handler{provider: provider}
}

type handler struct{ provider Provider }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts, ok := pathParts(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}

	ctx := r.Context()
	switch {
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "repositories":
		items, err := h.provider.ListRepositories(ctx)
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "repositories" && parts[2] == "branches":
		items, err := h.provider.ListBranches(ctx, parts[1])
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "repositories" && parts[2] == "commits":
		branch := r.URL.Query().Get("branch")
		if branch == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "branch is required")
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 200")
				return
			}
			limit = parsed
		}
		items, err := h.provider.ListCommits(ctx, parts[1], branch, limit)
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case r.Method == http.MethodGet && len(parts) == 4 && parts[0] == "repositories" && parts[2] == "commits":
		item, err := h.provider.GetCommit(ctx, parts[1], parts[3])
		if err != nil {
			h.writeProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (h *handler) writeProviderError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "provider_error"
	message := "git provider request failed"
	switch {
	case errors.Is(err, ErrRepositoryNotFound), errors.Is(err, ErrBranchNotFound), errors.Is(err, ErrCommitNotFound):
		status, code, message = http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusRequestTimeout, "request_cancelled", err.Error()
	}
	writeError(w, status, code, message)
}

func pathParts(path string) ([]string, bool) {
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
