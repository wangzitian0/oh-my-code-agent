package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AttachMessage is the initial handshake frame sent by a bridge client or admin CLI.
type AttachMessage struct {
	Type     string `json:"type"`                // "bridge_attach" or "admin_request"
	Server   string `json:"server,omitempty"`    // target tool name, e.g. "basic-memory", "subagent-worker"
	Profile  string `json:"profile,omitempty"`   // target profile, e.g. "work", "personal"
	Cwd      string `json:"cwd,omitempty"`       // caller working directory for auto-detection
	HostName string `json:"host_name,omitempty"` // e.g. "claude-code", "antigravity", "codex"
	Action   string `json:"action,omitempty"`    // for admin_request: "status", "worker_list", "worker_kill"
	TargetID string `json:"target_id,omitempty"` // worker ID for worker_kill
}

// ConnectedHost tracks an active client session.
type ConnectedHost struct {
	ID        string    `json:"id"`
	HostName  string    `json:"host_name"`
	Target    string    `json:"target"`
	Connected time.Time `json:"connected"`
}

// Hub represents the resident harness hub daemon.
type Hub struct {
	mu         sync.RWMutex
	config     *Config
	supervisor *Supervisor
	reaper     *IdleReaper
	pool       *WorkerPool
	arbiter    *StorageArbiter
	listener   net.Listener
	httpServer *http.Server
	hosts      map[string]*ConnectedHost
	hostSeq    uint64
	startTime  time.Time
	stopCh     chan struct{}
}

// New creates a new Hub instance.
func New(cfg *Config) *Hub {
	sup := NewSupervisor()

	// 1. Register Shared Tools
	for _, tc := range cfg.SharedTools {
		key := "shared:" + tc.Name
		sup.RegisterTool(ToolConfig{
			Name:       key,
			Command:    tc.Command,
			Args:       tc.Args,
			Env:        tc.Env,
			WorkingDir: tc.WorkingDir,
		})
		if _, exists := sup.GetTool(tc.Name); !exists {
			sup.RegisterTool(tc)
		}
	}

	// 2. Register Profile Tools
	for pName, pCfg := range cfg.Profiles {
		envMap, _ := LoadEnvFiles(pCfg.EnvFiles)
		for _, tc := range pCfg.Tools {
			hydrated := HydrateToolConfig(tc, envMap)
			key := pName + ":" + tc.Name
			hydrated.Name = key
			sup.RegisterTool(hydrated)
		}
	}

	// 3. Register Legacy Tools
	for _, tc := range cfg.Tools {
		if _, exists := sup.GetTool(tc.Name); !exists {
			sup.RegisterTool(tc)
		}
	}

	reaper := NewIdleReaper(sup, cfg.GetIdleTimeout())

	return &Hub{
		config:     cfg,
		supervisor: sup,
		reaper:     reaper,
		pool:       NewWorkerPool(),
		arbiter:    NewStorageArbiter(""),
		hosts:      make(map[string]*ConnectedHost),
		stopCh:     make(chan struct{}),
	}
}

// Supervisor returns the internal tool supervisor.
func (h *Hub) Supervisor() *Supervisor {
	return h.supervisor
}

// Reaper returns the internal idle reaper.
func (h *Hub) Reaper() *IdleReaper {
	return h.reaper
}

// Pool returns the worker pool.
func (h *Hub) Pool() *WorkerPool {
	return h.pool
}

// Arbiter returns the storage arbiter.
func (h *Hub) Arbiter() *StorageArbiter {
	return h.arbiter
}

// Start launches the Unix domain socket listener, optional HTTP server, and idle reaper.
func (h *Hub) Start(ctx context.Context) error {
	h.startTime = time.Now()

	// Ensure run directory exists
	dir := filepath.Dir(h.config.SocketPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("hub: create run dir %s: %w", dir, err)
	}

	// Remove stale socket if present
	_ = os.Remove(h.config.SocketPath)

	l, err := net.Listen("unix", h.config.SocketPath)
	if err != nil {
		return fmt.Errorf("hub: listen unix socket %s: %w", h.config.SocketPath, err)
	}
	h.listener = l
	_ = os.Chmod(h.config.SocketPath, 0600) // Restrict socket to owner-only for defense-in-depth

	// Accept loop
	go h.acceptLoop(l)

	// Start Idle Reaper
	h.reaper.Start(ctx)

	// Optional SSE/HTTP server
	if h.config.Port > 0 {
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(h.DashboardSnapshot())
		})
		mux.HandleFunc("/workers", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(h.pool.ListActiveWorkers())
		})
		mux.HandleFunc("/workers/kill", func(w http.ResponseWriter, r *http.Request) {
			id := r.URL.Query().Get("id")
			w.Header().Set("Content-Type", "application/json")
			if err := h.pool.KillWorker(id); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "killed": id})
		})

		h.httpServer = &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%d", h.config.Port),
			Handler: mux,
		}
		go func() {
			_ = h.httpServer.ListenAndServe()
		}()
	}

	return nil
}

func (h *Hub) acceptLoop(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-h.stopCh:
				return
			default:
				continue
			}
		}
		go h.handleConn(conn)
	}
}

func (h *Hub) handleConn(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// 1. Read first frame to identify client intent
	firstLine, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}

	var attach AttachMessage
	_ = json.Unmarshal(firstLine, &attach)

	// Check if this is an admin request from CLI
	if attach.Type == "admin_request" {
		h.handleAdminRequest(attach, writer)
		return
	}

	serverName := attach.Server
	hostName := attach.HostName
	if hostName == "" {
		hostName = "agent-host"
	}

	// Resolve server to instanceKey
	targetKey := serverName
	if res, ok := h.config.ResolveServer(attach.Profile, attach.Cwd, serverName); ok {
		targetKey = res.InstanceKey
		if _, exists := h.supervisor.GetTool(targetKey); !exists {
			envMap, _ := LoadEnvFiles(res.EnvFiles)
			hydrated := HydrateToolConfig(res.Config, envMap)
			hydrated.Name = targetKey
			_ = h.supervisor.GetOrCreateTool(targetKey, hydrated)
		}
	} else if _, exists := h.supervisor.GetTool(targetKey); !exists {
		// If neither resolved nor registered, fall back to serverName as key
		targetKey = serverName
	}

	connID := fmt.Sprintf("conn-%d", atomic.AddUint64(&h.hostSeq, 1))
	h.registerHost(connID, hostName, targetKey)
	defer h.unregisterHost(connID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// If firstLine was not attach frame, it might be the first JSON-RPC request!
	if attach.Type != "bridge_attach" {
		h.dispatchLine(ctx, targetKey, hostName, firstLine, writer)
	}

	// 2. Loop remaining lines
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		if len(line) == 0 {
			continue
		}
		h.dispatchLine(ctx, targetKey, hostName, line, writer)
	}
}

func (h *Hub) handleAdminRequest(attach AttachMessage, writer *bufio.Writer) {
	var payload []byte
	switch attach.Action {
	case "status":
		payload, _ = json.Marshal(h.DashboardSnapshot())
	case "worker_list":
		payload, _ = json.Marshal(h.pool.ListActiveWorkers())
	case "worker_kill":
		err := h.pool.KillWorker(attach.TargetID)
		if err != nil {
			payload, _ = json.Marshal(map[string]any{"status": "error", "message": err.Error()})
		} else {
			payload, _ = json.Marshal(map[string]any{"status": "ok", "killed": attach.TargetID})
		}
	default:
		payload, _ = json.Marshal(map[string]any{"status": "error", "message": fmt.Sprintf("unknown admin action %q", attach.Action)})
	}
	payload = append(payload, '\n')
	h.mu.Lock()
	_, _ = writer.Write(payload)
	_ = writer.Flush()
	h.mu.Unlock()
}

func (h *Hub) dispatchLine(ctx context.Context, targetTool string, hostName string, line []byte, writer *bufio.Writer) {
	var req JSONRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}

	// Intercept notifications
	if len(req.ID) == 0 {
		return
	}

	// Route to target tool
	tool, ok := h.supervisor.GetTool(targetTool)
	if !ok {
		resp := &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32601,
				Message: fmt.Sprintf("tool %q not registered in hub", targetTool),
			},
		}
		h.writeResponse(writer, resp)
		return
	}

	// Route subagent-worker tool calls with active worker tracking and SLA watchdog
	if strings.HasSuffix(targetTool, "subagent-worker") && req.Method == "tools/call" {
		h.handleSubagentCall(ctx, targetTool, hostName, tool, &req, writer)
		return
	}

	resp, err := tool.HandleClientRequest(ctx, &req)
	if err != nil {
		resp = &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32603,
				Message: err.Error(),
			},
		}
	}

	h.writeResponse(writer, resp)
}

func (h *Hub) handleSubagentCall(ctx context.Context, targetTool string, hostName string, tool *ManagedTool, req *JSONRPCRequest, writer *bufio.Writer) {
	var toolCall struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	_ = json.Unmarshal(req.Params, &toolCall)

	type SubagentArgs struct {
		Prompt   string `json:"prompt"`
		Model    string `json:"model"`
		LeaseSec int    `json:"lease_seconds"`
		Tasks    []struct {
			ID     string `json:"id"`
			Prompt string `json:"prompt"`
		} `json:"tasks"`
	}
	var args SubagentArgs
	_ = json.Unmarshal(toolCall.Arguments, &args)

	isBatch := toolCall.Name == "subagent_batch"
	workerType := "task"
	defaultModel := "glm-5.3"
	if isBatch {
		workerType = "batch"
		defaultModel = "glm-5.3-flash"
	}
	model := args.Model
	if model == "" {
		model = defaultModel
	}

	promptPreview := args.Prompt
	if promptPreview == "" && len(args.Tasks) > 0 {
		promptPreview = fmt.Sprintf("[%d tasks] %s", len(args.Tasks), args.Tasks[0].Prompt)
	}
	if len(promptPreview) > 60 {
		promptPreview = promptPreview[:60] + "..."
	}

	// 150s SLA limit (empirical knee-of-the-curve)
	stepTimeout := DefaultStepTimeout
	if args.LeaseSec > 0 {
		declared := time.Duration(args.LeaseSec) * time.Second
		if declared > stepTimeout && declared <= time.Hour {
			stepTimeout = declared
		}
	}

	stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()

	var release func()
	var err error
	if isBatch {
		release, err = h.pool.AcquireBatchSlot(stepCtx)
	} else {
		release, err = h.pool.AcquireTaskSlot(stepCtx)
	}
	if err != nil {
		resp := &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32000,
				Message: fmt.Sprintf("rate limit / queue error: %v", err),
			},
		}
		h.writeResponse(writer, resp)
		return
	}
	defer release()

	workerID := fmt.Sprintf("%s-%d", toolCall.Name, time.Now().UnixNano()%10000000)
	activeWorker := &ActiveWorker{
		ID:            workerID,
		Type:          workerType,
		ToolName:      toolCall.Name,
		HostName:      hostName,
		Model:         model,
		PromptPreview: promptPreview,
		StartTime:     time.Now(),
		LastHeartbeat: time.Now(),
		Phase:         "running",
		DeclaredLease: time.Duration(args.LeaseSec) * time.Second,
	}
	h.pool.RegisterWorker(activeWorker, cancel)
	defer h.pool.UnregisterWorker(workerID)

	resp, err := tool.HandleClientRequest(stepCtx, req)
	if err != nil {
		if errors.Is(stepCtx.Err(), context.DeadlineExceeded) {
			resp = &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    -32000,
					Message: fmt.Sprintf("watchdog: step duration exceeded %v SLA limit (worker %s terminated by watchdog)", stepTimeout, workerID),
				},
			}
		} else {
			resp = &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    -32603,
					Message: err.Error(),
				},
			}
		}
	} else if resp != nil && len(resp.Result) > 0 {
		resp.Result = sanitizeWorkerResult(resp.Result)
	}

	h.writeResponse(writer, resp)
}

func sanitizeWorkerResult(raw json.RawMessage) json.RawMessage {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return raw
	}

	modified := false
	for i := range result.Content {
		if result.Content[i].Type == "text" {
			text := result.Content[i].Text
			if DetectNgramLoop(text, 8, 4) {
				text = text + "\n\n[Warning: repetitive degeneracy loop detected and flagged by omca watchdog]"
				result.Content[i].Text = text
				modified = true
			}
			trimmed := strings.TrimSpace(text)
			if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && !json.Valid([]byte(trimmed)) {
				repaired := JSONAutoRepair(trimmed)
				if json.Valid([]byte(repaired)) {
					result.Content[i].Text = repaired
					modified = true
				}
			}
		}
	}

	if modified {
		if data, err := json.Marshal(result); err == nil {
			return json.RawMessage(data)
		}
	}
	return raw
}

func (h *Hub) writeResponse(writer *bufio.Writer, resp *JSONRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	data = append(data, '\n')
	h.mu.Lock()
	_, _ = writer.Write(data)
	_ = writer.Flush()
	h.mu.Unlock()
}

func (h *Hub) registerHost(id, hostName, target string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hosts[id] = &ConnectedHost{
		ID:        id,
		HostName:  hostName,
		Target:    target,
		Connected: time.Now(),
	}
}

func (h *Hub) unregisterHost(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.hosts, id)
}

// ActiveHosts returns all currently attached host clients.
func (h *Hub) ActiveHosts() []ConnectedHost {
	h.mu.RLock()
	defer h.mu.RUnlock()
	list := make([]ConnectedHost, 0, len(h.hosts))
	for _, host := range h.hosts {
		list = append(list, *host)
	}
	return list
}

// Close gracefully stops the hub daemon, idle reaper, and all child processes.
func (h *Hub) Close() error {
	close(h.stopCh)
	if h.reaper != nil {
		h.reaper.Stop()
	}
	if h.listener != nil {
		_ = h.listener.Close()
	}
	_ = os.Remove(h.config.SocketPath)

	if h.httpServer != nil {
		_ = h.httpServer.Close()
	}

	h.supervisor.StopAll()
	return nil
}

