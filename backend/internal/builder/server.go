// Package builder hosts the detached ttp-builder HTTP process. It deliberately
// contains no Kubernetes client or TTP database access.
package builder

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

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
	mux.HandleFunc("POST /v1/builds/stream", s.buildStream)
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

func (s *Server) buildStream(w http.ResponseWriter, r *http.Request) {
	if s == nil || !imagebuild.ValidBearerToken(r.Header.Get("Authorization"), s.token) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	request, ok := decodeBuildRequest(w, r)
	if !ok {
		return
	}
	if err := request.Validate(); err != nil {
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

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "streaming responses are unsupported"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	var writeMu sync.Mutex
	writeEvent := func(event any) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := encoder.Encode(event); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	emit := func(entry imagebuild.LogEntry) {
		if strings.TrimSpace(entry.Line) == "" {
			return
		}
		_ = writeEvent(map[string]any{"type": "log", "log": entry})
	}

	var result imagebuild.Result
	var err error
	if streamer, supported := s.executor.(imagebuild.StreamingBuilder); supported {
		result, err = streamer.BuildWithLogs(r.Context(), request, emit)
	} else {
		result, err = s.executor.Build(r.Context(), request)
		for _, entry := range result.Logs {
			emit(entry)
		}
	}
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
		// Streaming builders have already emitted their logs. The logs field is
		// still included for clients that only consume the terminal event.
		_ = writeEvent(map[string]any{"type": "error", "error": message, "logs": logs})
		return
	}
	// The individual log events have already been flushed to the client. Do
	// not append the complete log history to the terminal result and recreate
	// the same end-of-build burst that this endpoint is meant to avoid.
	result.Logs = nil
	_ = writeEvent(map[string]any{"type": "result", "result": result})
}

func decodeBuildRequest(w http.ResponseWriter, r *http.Request) (imagebuild.Request, bool) {
	var payload struct {
		Request imagebuild.Request `json:"request"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid build request"})
		return imagebuild.Request{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid build request"})
		return imagebuild.Request{}, false
	}
	return payload.Request, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
