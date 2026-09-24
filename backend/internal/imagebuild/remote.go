package imagebuild

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RemoteConfig describes the separate ttp-builder process. A remote builder
// is intentionally opt-in: the TTP API starts without one by default.
type RemoteConfig struct {
	URL     string
	Token   string
	Timeout time.Duration
}

type Remote struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewRemote(config RemoteConfig) (*Remote, error) {
	baseURL, err := normalizeBuilderURL(config.URL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.Token) == "" {
		return nil, fmt.Errorf("builder token is required")
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	return &Remote{baseURL: baseURL, token: config.Token, client: &http.Client{Timeout: timeout}}, nil
}

func (r *Remote) Build(ctx context.Context, request Request) (Result, error) {
	if r == nil || r.client == nil {
		return Result{}, ErrNotConfigured
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	body, err := json.Marshal(struct {
		Request Request `json:"request"`
	}{Request: request})
	if err != nil {
		return Result{}, fmt.Errorf("encode build request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/v1/builds", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("create builder request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+r.token)
	response, err := r.client.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("call image builder: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 2<<20)
	var payload struct {
		Result Result     `json:"result"`
		Logs   []LogEntry `json:"logs"`
		Error  string     `json:"error"`
	}
	if err := json.NewDecoder(limited).Decode(&payload); err != nil {
		return Result{}, fmt.Errorf("decode image builder response: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return Result{}, ErrUnauthorized
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(payload.Error)
		if message == "" {
			message = "image builder rejected the request"
		}
		return Result{Logs: payload.Logs}, &Failure{Err: errorsFromBuilder(message), Logs: payload.Logs}
	}
	if !IsDigest(payload.Result.Digest) || !strings.Contains(payload.Result.Image, "@"+payload.Result.Digest) {
		return Result{}, fmt.Errorf("image builder returned no immutable image digest")
	}
	return payload.Result, nil
}

// BuildWithLogs uses the builder's newline-delimited stream endpoint. The
// regular Build method intentionally remains available for older builders and
// callers that only need the final artifact.
func (r *Remote) BuildWithLogs(ctx context.Context, request Request, onLog LogFunc) (Result, error) {
	if r == nil || r.client == nil {
		return Result{}, ErrNotConfigured
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	body, err := json.Marshal(struct {
		Request Request `json:"request"`
	}{Request: request})
	if err != nil {
		return Result{}, fmt.Errorf("encode build request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/v1/builds/stream", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("create builder stream request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/x-ndjson")
	httpRequest.Header.Set("Authorization", "Bearer "+r.token)
	response, err := r.client.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("call image builder stream: %w", err)
	}
	defer response.Body.Close()

	// Older detached builders do not know the streaming endpoint. Fall back
	// to the original request/response contract so upgrading the control plane
	// does not make an otherwise healthy builder unusable.
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		response.Body.Close()
		result, err := r.Build(ctx, request)
		if onLog != nil {
			for _, entry := range result.Logs {
				onLog(entry)
			}
		}
		return result, err
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return Result{}, ErrUnauthorized
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var payload struct {
			Logs  []LogEntry `json:"logs"`
			Error string     `json:"error"`
		}
		if decodeErr := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&payload); decodeErr != nil {
			return Result{}, fmt.Errorf("decode image builder stream error: %w", decodeErr)
		}
		message := strings.TrimSpace(payload.Error)
		if message == "" {
			message = "image builder rejected the request"
		}
		return Result{Logs: payload.Logs}, &Failure{Err: errorsFromBuilder(message), Logs: payload.Logs}
	}

	var result Result
	var streamedLogs []LogEntry
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 8<<20))
	scanner.Buffer(make([]byte, 16<<10), 1<<20)
	for scanner.Scan() {
		var event struct {
			Type   string     `json:"type"`
			Log    LogEntry   `json:"log"`
			Result Result     `json:"result"`
			Error  string     `json:"error"`
			Logs   []LogEntry `json:"logs"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return Result{Logs: streamedLogs}, fmt.Errorf("decode image builder stream event: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(event.Type)) {
		case "log":
			if strings.TrimSpace(event.Log.Line) == "" {
				continue
			}
			streamedLogs = append(streamedLogs, event.Log)
			if onLog != nil {
				onLog(event.Log)
			}
		case "result":
			result = event.Result
		case "error":
			if len(event.Logs) > len(streamedLogs) {
				for _, entry := range event.Logs[len(streamedLogs):] {
					streamedLogs = append(streamedLogs, entry)
					if onLog != nil {
						onLog(entry)
					}
				}
			}
			return Result{Logs: streamedLogs}, &Failure{Err: errorsFromBuilder(event.Error), Logs: streamedLogs}
		}
	}
	if err := scanner.Err(); err != nil {
		return Result{Logs: streamedLogs}, fmt.Errorf("read image builder stream: %w", err)
	}
	result.Logs = streamedLogs
	if !IsDigest(result.Digest) || !strings.Contains(result.Image, "@"+result.Digest) {
		return Result{Logs: streamedLogs}, fmt.Errorf("image builder returned no immutable image digest")
	}
	return result, nil
}

func (r *Remote) Preflight(ctx context.Context, request Request) error {
	if r == nil || r.client == nil {
		return ErrNotConfigured
	}
	if err := request.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		Request Request `json:"request"`
	}{Request: request})
	if err != nil {
		return fmt.Errorf("encode builder preflight request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/v1/preflight", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create builder preflight request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.token)
	response, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("call image builder preflight: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return fmt.Errorf("decode image builder preflight response: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ErrUnauthorized
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if strings.TrimSpace(payload.Error) != "" {
			return fmt.Errorf("%w: %s", ErrRegistryPreflightFailed, payload.Error)
		}
		return fmt.Errorf("%w: image builder preflight failed", ErrRegistryPreflightFailed)
	}
	return nil
}

func errorsFromBuilder(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "image build failed"
	}
	return fmt.Errorf("%s", message)
}

func normalizeBuilderURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("builder URL is invalid")
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return "", fmt.Errorf("builder URL must use HTTPS outside loopback")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ValidBearerToken compares the builder token without leaking timing
// information. It is exported so the detached HTTP server uses exactly the
// same verification semantics as any future transport.
func ValidBearerToken(header, expected string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) || strings.TrimSpace(expected) == "" {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
