package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// JSONRPCRequest represents an inbound JSON-RPC 2.0 request or notification.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents an outbound JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError defines the standard JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ManagedTool manages the lifecycle and multiplexing of one singleton MCP tool process.
type ManagedTool struct {
	mu           sync.Mutex
	config       ToolConfig
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	status       string
	pid          int
	startTime    time.Time
	restarts     int
	reqSeq       uint64
	pending      map[string]chan *JSONRPCResponse
	initResult   json.RawMessage
	toolsResult  json.RawMessage
	stopCh       chan struct{}
}

// Supervisor coordinates multiple singleton MCP tool processes.
type Supervisor struct {
	mu    sync.RWMutex
	tools map[string]*ManagedTool
}

// NewSupervisor creates a supervisor instance.
func NewSupervisor() *Supervisor {
	return &Supervisor{
		tools: make(map[string]*ManagedTool),
	}
}

// RegisterTool registers a tool definition with the supervisor.
func (s *Supervisor) RegisterTool(cfg ToolConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[cfg.Name] = &ManagedTool{
		config:  cfg,
		status:  "REGISTERED",
		pending: make(map[string]chan *JSONRPCResponse),
		stopCh:  make(chan struct{}),
	}
}

// GetTool returns the managed tool by name.
func (s *Supervisor) GetTool(name string) (*ManagedTool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tools[name]
	return t, ok
}

// ListTools returns all registered tools and their current status.
func (s *Supervisor) ListTools() map[string]ToolStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := make(map[string]ToolStats, len(s.tools))
	for name, t := range s.tools {
		t.mu.Lock()
		uptime := time.Duration(0)
		if !t.startTime.IsZero() && t.status == "RUNNING" {
			uptime = time.Since(t.startTime)
		}
		stats[name] = ToolStats{
			Name:         name,
			Status:       t.status,
			PID:          t.pid,
			Uptime:       uptime,
			RestartCount: t.restarts,
			Command:      t.config.Command,
		}
		t.mu.Unlock()
	}
	return stats
}

// ToolStats represents the runtime stats of a managed tool.
type ToolStats struct {
	Name         string        `json:"name"`
	Status       string        `json:"status"`
	PID          int           `json:"pid"`
	Uptime       time.Duration `json:"uptime"`
	RestartCount int           `json:"restart_count"`
	Command      string        `json:"command"`
}

// Start spawns the tool subprocess and performs initial handshake.
func (t *ManagedTool) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status == "RUNNING" {
		return nil
	}

	return t.startLocked(ctx)
}

func (t *ManagedTool) startLocked(ctx context.Context) error {
	t.status = "STARTING"
	cmd := exec.CommandContext(ctx, t.config.Command, t.config.Args...)
	if t.config.WorkingDir != "" {
		cmd.Dir = t.config.WorkingDir
	}
	cmd.Env = os.Environ()
	for k, v := range t.config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.status = "FAILED"
		return fmt.Errorf("hub: tool %s stdin pipe: %w", t.config.Name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		t.status = "FAILED"
		return fmt.Errorf("hub: tool %s stdout pipe: %w", t.config.Name, err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		t.status = "FAILED"
		return fmt.Errorf("hub: start tool %s: %w", t.config.Name, err)
	}

	t.cmd = cmd
	t.stdin = stdin
	t.pid = cmd.Process.Pid
	t.startTime = time.Now()
	t.status = "RUNNING"

	// Start reader loop in background
	go t.readLoop(stdout)

	// Perform initialize handshake
	initReq := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"init-1"`),
		Method:  "initialize",
		Params: json.RawMessage(`{
			"protocolVersion": "2024-11-05",
			"capabilities": {},
			"clientInfo": {"name": "omca-hub", "version": "1.0.0"}
		}`),
	}

	initResp, err := t.callRawLocked(ctx, initReq)
	if err == nil && initResp != nil {
		t.initResult = initResp.Result
		// Send notifications/initialized
		notif := &JSONRPCRequest{
			JSONRPC: "2.0",
			Method:  "notifications/initialized",
		}
		data, _ := json.Marshal(notif)
		data = append(data, '\n')
		_, _ = t.stdin.Write(data)

		// Fetch and cache tools list
		toolsReq := &JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`"tools-1"`),
			Method:  "tools/list",
		}
		toolsResp, err := t.callRawLocked(ctx, toolsReq)
		if err == nil && toolsResp != nil {
			t.toolsResult = toolsResp.Result
		}
	}

	return nil
}

func (t *ManagedTool) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp JSONRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		if len(resp.ID) > 0 {
			idStr := string(resp.ID)
			t.mu.Lock()
			ch, ok := t.pending[idStr]
			if ok {
				delete(t.pending, idStr)
			}
			t.mu.Unlock()

			if ok && ch != nil {
				ch <- &resp
			}
		}
	}

	t.mu.Lock()
	t.status = "STOPPED"
	t.pid = 0
	// Notify remaining pending requests
	for k, ch := range t.pending {
		delete(t.pending, k)
		close(ch)
	}
	t.mu.Unlock()
}

func (t *ManagedTool) callRawLocked(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	if t.stdin == nil {
		return nil, errors.New("tool stdin not available")
	}

	seq := atomic.AddUint64(&t.reqSeq, 1)
	reqID := fmt.Sprintf("req-%d", seq)
	req.ID = json.RawMessage(fmt.Sprintf("%q", reqID))

	ch := make(chan *JSONRPCResponse, 1)
	t.pending[fmt.Sprintf("%q", reqID)] = ch

	data, err := json.Marshal(req)
	if err != nil {
		delete(t.pending, fmt.Sprintf("%q", reqID))
		return nil, err
	}
	data = append(data, '\n')

	if _, err := t.stdin.Write(data); err != nil {
		delete(t.pending, fmt.Sprintf("%q", reqID))
		return nil, fmt.Errorf("write stdin: %w", err)
	}

	select {
	case <-ctx.Done():
		delete(t.pending, fmt.Sprintf("%q", reqID))
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("tool subprocess closed connection")
		}
		return resp, nil
	}
}

// HandleClientRequest processes a JSON-RPC request from an attached client.
func (t *ManagedTool) HandleClientRequest(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.status != "RUNNING" {
		if err := t.startLocked(ctx); err != nil {
			t.mu.Unlock()
			return nil, err
		}
	}

	// 1. Intercept "initialize": return cached handshake result
	if req.Method == "initialize" {
		cached := t.initResult
		t.mu.Unlock()
		if len(cached) > 0 {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  cached,
			}, nil
		}
		t.mu.Lock()
	}

	// 2. Intercept "tools/list": return cached tools list if available
	if req.Method == "tools/list" {
		cached := t.toolsResult
		t.mu.Unlock()
		if len(cached) > 0 {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  cached,
			}, nil
		}
		t.mu.Lock()
	}

	// 3. Forward all other calls (e.g. tools/call, prompts/list, etc.)
	resp, err := t.callRawLocked(ctx, req)
	t.mu.Unlock()
	if err != nil {
		return nil, err
	}
	resp.ID = req.ID
	return resp, nil
}

// Stop gracefully terminates the managed tool process.
func (t *ManagedTool) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	t.status = "STOPPED"
	return nil
}

// StopAll stops all managed tool processes.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tools {
		_ = t.Stop()
	}
}
