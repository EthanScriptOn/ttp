package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
)

const (
	terminalSubprotocol = "ttp-terminal.v1"
	terminalReadLimit   = 64 << 10
)

type terminalClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type terminalServerMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code,omitempty"`
}

// podTerminal upgrades the authenticated project request into a browser
// terminal and bridges it to the runtime's Kubernetes PTY. Authorization and
// project permission checks happen before the upgrade, so an untrusted caller
// cannot use the socket as a cluster probe.
func (s *Server) podTerminal(c *gin.Context) {
	project, ok := s.projectMetadataForRequest(c)
	if !ok {
		return
	}
	target, ok := s.deploymentTargetForProject(c, project)
	if !ok || !s.ensureRuntimeTarget(c, project, target) {
		return
	}

	originAllowed := strings.TrimSpace(s.deps.Config.AllowedOrigin)
	upgrader := websocket.Upgrader{
		ReadBufferSize:  16 << 10,
		WriteBufferSize: 16 << 10,
		Subprotocols:    []string{terminalSubprotocol},
		CheckOrigin: func(r *http.Request) bool {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			return terminalOriginAllowed(originAllowed, origin)
		},
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(terminalReadLimit)

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	stdinReader, stdinWriter := io.Pipe()
	sizes := make(chan runtime.TerminalSize, 4)
	// A useful default prevents a shell from waiting for a resize event when a
	// browser has not measured the xterm element yet.
	sizes <- runtime.TerminalSize{Columns: 120, Rows: 32}
	writer := &terminalSocketWriter{conn: conn}
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- s.deps.Runtime.StreamPodTerminal(ctx, runtime.PodTerminalRequest{
			PodRef: runtime.PodRef{
				ClusterID: target.ClusterID,
				Namespace: target.Namespace,
				Name:      c.Param("podName"),
				ProjectID: project.ID,
			},
			Container: strings.TrimSpace(c.Query("container")),
		}, stdinReader, writer, writer, sizes)
	}()
	s.recordAudit(c, "打开 Pod 终端", project.Name+" · "+c.Param("podName"))

	type readResult struct {
		messageType int
		payload     []byte
		err         error
	}
	readResults := make(chan readResult, 1)
	go func() {
		for {
			messageType, payload, readErr := conn.ReadMessage()
			select {
			case readResults <- readResult{messageType: messageType, payload: payload, err: readErr}:
			case <-ctx.Done():
				return
			}
			if readErr != nil {
				return
			}
		}
	}()

	for {
		select {
		case result := <-readResults:
			if result.err != nil {
				cancel()
				_ = stdinWriter.Close()
				close(sizes)
				<-streamDone
				s.recordAudit(c, "关闭 Pod 终端", project.Name+" · "+c.Param("podName"))
				return
			}
			if result.messageType != websocket.TextMessage && result.messageType != websocket.BinaryMessage {
				continue
			}
			if err := handleTerminalClientMessage(result.payload, stdinWriter, sizes); err != nil {
				_ = writer.send(terminalServerMessage{Type: "error", Message: err.Error()})
				cancel()
				_ = stdinWriter.Close()
				close(sizes)
				<-streamDone
				return
			}
		case streamErr := <-streamDone:
			cancel()
			_ = stdinWriter.Close()
			close(sizes)
			if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
				_ = writer.send(terminalServerMessage{Type: "error", Message: streamErr.Error()})
			}
			_ = writer.send(terminalServerMessage{Type: "exit", Code: terminalExitCode(streamErr)})
			s.recordAudit(c, "关闭 Pod 终端", project.Name+" · "+c.Param("podName"))
			return
		}
	}
}

func terminalOriginAllowed(allowed, origin string) bool {
	if origin == "" || allowed == "" || allowed == "*" || origin == allowed {
		return true
	}
	allowedURL, allowedErr := url.Parse(allowed)
	originURL, originErr := url.Parse(origin)
	if allowedErr != nil || originErr != nil || allowedURL.Port() != originURL.Port() {
		return false
	}
	isLoopback := func(host string) bool {
		switch strings.ToLower(host) {
		case "localhost", "127.0.0.1", "::1":
			return true
		default:
			return false
		}
	}
	return isLoopback(allowedURL.Hostname()) && isLoopback(originURL.Hostname())
}

func handleTerminalClientMessage(payload []byte, stdin io.Writer, sizes chan runtime.TerminalSize) error {
	var message terminalClientMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return errors.New("终端消息格式不正确")
	}
	switch strings.ToLower(strings.TrimSpace(message.Type)) {
	case "input":
		if message.Data == "" {
			return nil
		}
		_, err := io.WriteString(stdin, message.Data)
		return err
	case "resize":
		if message.Cols == 0 || message.Rows == 0 || message.Cols > 500 || message.Rows > 200 {
			return errors.New("终端尺寸不正确")
		}
		select {
		case sizes <- runtime.TerminalSize{Columns: message.Cols, Rows: message.Rows}:
		default:
			// Keep the most recent viewport when a browser is rapidly resized.
			select {
			case <-sizes:
			default:
			}
			select {
			case sizes <- runtime.TerminalSize{Columns: message.Cols, Rows: message.Rows}:
			default:
			}
		}
		return nil
	case "ping":
		return nil
	default:
		return errors.New("未知的终端消息类型")
	}
}

func terminalExitCode(err error) int {
	if err == nil || errors.Is(err, context.Canceled) {
		return 0
	}
	return 1
}

type terminalSocketWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *terminalSocketWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if err := w.send(terminalServerMessage{Type: "output", Data: string(data)}); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (w *terminalSocketWriter) send(message terminalServerMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.TextMessage, payload)
}
