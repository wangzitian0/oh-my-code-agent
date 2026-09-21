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
	mu          sync.Mutex
	writeMu     sync.Mutex
	pendingMu   sync.Mutex
	config      ToolConfig
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	status      string
	pid         int
	startTime   time.Time
	lastActive  time.Time
	restarts    int
	reqSeq      uint64
	pending     map[string]chan *JSONRPCResponse
	initResult  json.RawMessage
	toolsResult json.RawMessage
	stopCh      chan struct{}
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
		config:     cfg,
		status:     "REGISTERED",
		lastActive: time.Now(),
		pending:    make(map[string]chan *JSONRPCResponse),
		stopCh:     make(chan struct{}),
	}
}

// GetTool returns the managed tool by name or key.
func (s *Supervisor) GetTool(name string) (*ManagedTool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tools[name]
	return t, ok
}

// GetOrCreateTool returns an existing tool or registers and returns a new tool.
func (s *Supervisor) GetOrCreateTool(key string, cfg ToolConfig) *ManagedTool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tools[key]; ok {
		return t
	}
	t := &ManagedTool{
		config:     cfg,
		status:     "REGISTERED",
		lastActive: time.Now(),
		pending:    make(map[string]chan *JSONRPCResponse),
		stopCh:     make(chan struct{}),
	}
	s.tools[key] = t
	return t
}

// ListManagedTools returns all currently managed tools.
func (s *Supervisor) ListManagedTools() []*ManagedTool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*ManagedTool, 0, len(s.tools))
	for _, t := range s.tools {
		list = append(list, t)
	}
	return list
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
		idle := time.Since(t.lastActive)
		if t.lastActive.IsZero() {
			idle = 0
		}
		stats[name] = ToolStats{
			Name:         name,
			Status:       t.status,
			PID:          t.pid,
			Uptime:       uptime,
			IdleDuration: idle,
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
	IdleDuration time.Duration `json:"idle_duration"`
	RestartCount int           `json:"restart_count"`
	Command      string        `json:"command"`
}

// Name returns the tool's configured name.
func (t *ManagedTool) Name() string {
	return t.config.Name
}

// Status returns the tool's current lifecycle state.
func (t *ManagedTool) Status() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

// LastActive returns the timestamp of the last incoming request.
func (t *ManagedTool) LastActive() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastActive
}

// Touch refreshes the tool's active timestamp.
func (t *ManagedTool) Touch() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastActive = time.Now()
}

// Stop gracefully terminates the tool subprocess and resets its status to REGISTERED.
// Cached capabilities (initialize & tools/list) are preserved.
func (t *ManagedTool) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status != "RUNNING" && t.status != "STARTING" {
		return nil
	}

	t.status = "STOPPING"
	if t.stdin != nil {
		_ = t.stdin.Close()
	}

	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() {
			_ = t.cmd.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = t.cmd.Process.Kill()
		}
	}

	t.cmd = nil
	t.stdin = nil
	t.pid = 0
	t.status = "REGISTERED"

	t.pendingMu.Lock()
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
	t.pendingMu.Unlock()

	return nil
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
	t.lastActive = time.Now()
	t.status = "RUNNING"

	// Start reader loop in background
	go t.readLoop(cmd, stdout)

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

	initCtx, initCancel := context.WithTimeout(ctx, 10*time.Second)
	defer initCancel()
	initResp, err := t.callRawLocked(initCtx, initReq)
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
		toolsResp, err := t.callRawLocked(initCtx, toolsReq)
		if err == nil && toolsResp != nil {
			t.toolsResult = toolsResp.Result
		}
	}

	return nil
}

func (t *ManagedTool) readLoop(cmd *exec.Cmd, stdout io.Reader) {
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

		idKey := string(resp.ID)
		t.pendingMu.Lock()
		ch, ok := t.pending[idKey]
		if ok {
			delete(t.pending, idKey)
		}
		t.pendingMu.Unlock()

		if ok {
			ch <- &resp
		}
	}

	// Process exited or stdout closed
	t.mu.Lock()
	if t.cmd == cmd && t.status == "RUNNING" {
		t.status = "STOPPED"
	}
	t.mu.Unlock()

	// Drain remaining pending channels
	t.pendingMu.Lock()
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
	t.pendingMu.Unlock()
}

func (t *ManagedTool) callRawLocked(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	seq := atomic.AddUint64(&t.reqSeq, 1)
	reqID := fmt.Sprintf("internal-%d", seq)
	req.ID = json.RawMessage(fmt.Sprintf("%q", reqID))

	ch := make(chan *JSONRPCResponse, 1)
	t.pendingMu.Lock()
	t.pending[fmt.Sprintf("%q", reqID)] = ch
	t.pendingMu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		t.pendingMu.Lock()
		delete(t.pending, fmt.Sprintf("%q", reqID))
		t.pendingMu.Unlock()
		return nil, err
	}
	data = append(data, '\n')

	t.writeMu.Lock()
	_, err = t.stdin.Write(data)
	t.writeMu.Unlock()
	if err != nil {
		t.pendingMu.Lock()
		delete(t.pending, fmt.Sprintf("%q", reqID))
		t.pendingMu.Unlock()
		return nil, fmt.Errorf("write stdin: %w", err)
	}

	select {
	case <-ctx.Done():
		t.pendingMu.Lock()
		delete(t.pending, fmt.Sprintf("%q", reqID))
		t.pendingMu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("tool subprocess closed connection")
		}
		return resp, nil
	}
}

func (t *ManagedTool) callRaw(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	seq := atomic.AddUint64(&t.reqSeq, 1)
	reqID := fmt.Sprintf("req-%d", seq)
	req.ID = json.RawMessage(fmt.Sprintf("%q", reqID))

	ch := make(chan *JSONRPCResponse, 1)
	t.pendingMu.Lock()
	t.pending[fmt.Sprintf("%q", reqID)] = ch
	t.pendingMu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		t.pendingMu.Lock()
		delete(t.pending, fmt.Sprintf("%q", reqID))
		t.pendingMu.Unlock()
		return nil, err
	}
	data = append(data, '\n')

	t.writeMu.Lock()
	_, err = t.stdin.Write(data)
	t.writeMu.Unlock()
	if err != nil {
		t.pendingMu.Lock()
		delete(t.pending, fmt.Sprintf("%q", reqID))
		t.pendingMu.Unlock()
		return nil, fmt.Errorf("write stdin: %w", err)
	}

	select {
	case <-ctx.Done():
		t.pendingMu.Lock()
		delete(t.pending, fmt.Sprintf("%q", reqID))
		t.pendingMu.Unlock()
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
	t.Touch()

	// 1. Intercept "initialize": return cached handshake result if present
	if req.Method == "initialize" {
		t.mu.Lock()
		cached := t.initResult
		t.mu.Unlock()
		if len(cached) > 0 {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  cached,
			}, nil
		}
	}

	// 2. Intercept "tools/list": return cached tools list if present
	if req.Method == "tools/list" {
		t.mu.Lock()
		cached := t.toolsResult
		t.mu.Unlock()
		if len(cached) > 0 {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  cached,
			}, nil
		}
	}

	// 3. Otherwise, ensure tool is running and dispatch
	t.mu.Lock()
	if t.status != "RUNNING" {
		if err := t.startLocked(ctx); err != nil {
			t.mu.Unlock()
			return nil, err
		}
	}
	t.mu.Unlock()

	// If initialize/tools list was populated by startLocked, return the cached version
	if req.Method == "initialize" {
		t.mu.Lock()
		cached := t.initResult
		t.mu.Unlock()
		if len(cached) > 0 {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  cached,
			}, nil
		}
	}
	if req.Method == "tools/list" {
		t.mu.Lock()
		cached := t.toolsResult
		t.mu.Unlock()
		if len(cached) > 0 {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  cached,
			}, nil
		}
	}

	resp, err := t.callRaw(ctx, req)
	if err != nil {
		return nil, err
	}

	// Echo client request ID back
	resp.ID = req.ID
	return resp, nil
}

// StopAll stops all managed tool processes.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tools {
		_ = t.Stop()
	}
}
