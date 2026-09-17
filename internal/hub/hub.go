package hub

import (
	"bufio"
	"context"
	"encoding/json"
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

// AttachMessage is the initial handshake frame sent by a bridge client.
type AttachMessage struct {
	Type     string `json:"type"`               // "bridge_attach"
	Server   string `json:"server"`             // target tool name, e.g. "basic-memory", "subagent-worker"
	Profile  string `json:"profile,omitempty"`  // target profile, e.g. "work", "personal"
	Cwd      string `json:"cwd,omitempty"`      // caller working directory for auto-detection
	HostName string `json:"host_name,omitempty"`// e.g. "claude-code", "antigravity", "codex"
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
		h.dispatchLine(ctx, targetKey, firstLine, writer)
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
		h.dispatchLine(ctx, targetKey, line, writer)
	}
}

func (h *Hub) dispatchLine(ctx context.Context, targetTool string, line []byte, writer *bufio.Writer) {
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

	// Apply worker pool semaphore if tool is a subagent execution tool
	var release func()
	if strings.HasSuffix(targetTool, "subagent-worker") && req.Method == "tools/call" {
		var toolCall struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(req.Params, &toolCall)

		var err error
		if toolCall.Name == "subagent_batch" {
			release, err = h.pool.AcquireBatchSlot(ctx)
		} else if toolCall.Name == "subagent_task" || toolCall.Name == "subagent_code_transform" {
			release, err = h.pool.AcquireTaskSlot(ctx)
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
		if release != nil {
			defer release()
		}
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

