// Package builder hosts the detached ttp-builder HTTP process. It deliberately
// contains no Kubernetes client or TTP database access.
package builder

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
)

type Server struct {
	token    string
	executor imagebuild.Builder
	sem      chan struct{}
}

func NewServer(token string, executor imagebuild.Builder, maxConcurrent int) (*Server, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("builder token is required")
	}
	if executor == nil {
		return nil, errors.New("build executor is required")
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &Server{token: token, executor: executor, sem: make(chan struct{}, maxConcurrent)}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /v1/preflight", s.preflight)
	mux.HandleFunc("POST /v1/builds", s.build)
	return mux
}

func (s *Server) preflight(w http.ResponseWriter, r *http.Request) {
	if s == nil || !imagebuild.ValidBearerToken(r.Header.Get("Authorization"), s.token) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var payload struct {
		Request imagebuild.Request `json:"request"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid preflight request"})
		return
	}
	if err := payload.Request.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	checker, ok := s.executor.(imagebuild.PreflightChecker)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "builder preflight is unsupported"})
		return
	}
	if err := checker.Preflight(r.Context(), payload.Request); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) build(w http.ResponseWriter, r *http.Request) {
	if s == nil || !imagebuild.ValidBearerToken(r.Header.Get("Authorization"), s.token) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var payload struct {
		Request imagebuild.Request `json:"request"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid build request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid build request"})
		return
	}
	if err := payload.Request.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-r.Context().Done():
		writeJSON(w, http.StatusRequestTimeout, map[string]string{"error": "build request cancelled"})
		return
	}
	result, err := s.executor.Build(r.Context(), payload.Request)
	if err != nil {
		logs := result.Logs
		var failure *imagebuild.Failure
		if errors.As(err, &failure) && len(failure.Logs) > 0 {
			logs = failure.Logs
		}
		message := strings.TrimSpace(err.Error())
		if message == "" {
			message = "image build failed"
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": message, "logs": logs})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
