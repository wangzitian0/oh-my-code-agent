package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfig_DefaultsAndLoad(t *testing.T) {
	if DefaultSocketPath() == "" {
		t.Errorf("expected non-empty DefaultSocketPath")
	}
	if DefaultConfigPath() == "" {
		t.Errorf("expected non-empty DefaultConfigPath")
	}

	// 1. Non-existent path returns default
	nonExistent := filepath.Join(t.TempDir(), "nonexistent.json")
	cfg, err := LoadConfig(nonExistent)
	if err != nil {
		t.Fatalf("LoadConfig(nonexistent) error: %v", err)
	}
	if cfg.Port != 8765 {
		t.Errorf("expected default Port 8765, got %d", cfg.Port)
	}
	if cfg.SocketPath == "" {
		t.Errorf("expected non-empty SocketPath")
	}

	// 2. Invalid JSON returns error
	badJSONPath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badJSONPath, []byte("{invalid-json"), 0600); err != nil {
		t.Fatalf("write bad JSON: %v", err)
	}
	_, err = LoadConfig(badJSONPath)
	if err == nil {
		t.Fatalf("expected error for invalid JSON, got nil")
	}

	// 3. Valid JSON with custom port and empty socket path falls back to default
	validJSON := `{
		"port": 9090,
		"tools": {
			"dummy": {
				"name": "dummy",
				"command": "echo"
			}
		}
	}`
	validPath := filepath.Join(t.TempDir(), "valid.json")
	if err := os.WriteFile(validPath, []byte(validJSON), 0600); err != nil {
		t.Fatalf("write valid JSON: %v", err)
	}
	cfg, err = LoadConfig(validPath)
	if err != nil {
		t.Fatalf("LoadConfig(valid) error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected Port 9090, got %d", cfg.Port)
	}
	if _, ok := cfg.Tools["dummy"]; !ok {
		t.Errorf("expected dummy tool registered")
	}
	if cfg.SocketPath != DefaultSocketPath() {
		t.Errorf("expected SocketPath to fallback to DefaultSocketPath, got %s", cfg.SocketPath)
	}
}

func TestWorkerPool_ConcurrencyLimits(t *testing.T) {
	pool := NewWorkerPool()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	releases := make([]func(), 0, 5)
	for i := 0; i < 5; i++ {
		release, err := pool.AcquireTaskSlot(ctx)
		if err != nil {
			t.Fatalf("failed to acquire task slot %d: %v", i, err)
		}
		releases = append(releases, release)
	}

	// 6th acquire must block/timeout
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer shortCancel()
	_, err := pool.AcquireTaskSlot(shortCtx)
	if err == nil {
		t.Fatalf("expected 6th slot acquire to timeout, but succeeded")
	}

	// Release one and retry
	releases[0]()
	releases = releases[1:]

	rel, err := pool.AcquireTaskSlot(ctx)
	if err != nil {
		t.Fatalf("expected to acquire released slot, got error: %v", err)
	}
	releases = append(releases, rel)

	// Clean up remaining
	for _, r := range releases {
		r()
	}

	stats := pool.Stats()
	if stats.ActiveTasks != 0 {
		t.Errorf("expected 0 active tasks after cleanup, got %d", stats.ActiveTasks)
	}
	if stats.TotalExecuted != 6 {
		t.Errorf("expected 6 total executed, got %d", stats.TotalExecuted)
	}
}

func TestWorkerPool_BatchSlots(t *testing.T) {
	pool := NewWorkerPool()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	releases := make([]func(), 0, 50)
	for i := 0; i < 50; i++ {
		release, err := pool.AcquireBatchSlot(ctx)
		if err != nil {
			t.Fatalf("failed to acquire batch slot %d: %v", i, err)
		}
		releases = append(releases, release)
	}

	stats := pool.Stats()
	if stats.ActiveBatch != 50 {
		t.Errorf("expected 50 active batch slots, got %d", stats.ActiveBatch)
	}

	// 51st acquire must timeout
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer shortCancel()
	_, err := pool.AcquireBatchSlot(shortCtx)
	if err == nil {
		t.Fatalf("expected 51st batch slot acquire to timeout, but succeeded")
	}

	// Clean up
	for _, r := range releases {
		r()
	}

	stats = pool.Stats()
	if stats.ActiveBatch != 0 {
		t.Errorf("expected 0 active batch slots after cleanup, got %d", stats.ActiveBatch)
	}
}

func TestWorkerPool_RateLimiter(t *testing.T) {
	pool := NewWorkerPool()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for i := 0; i < 15; i++ {
		rel, err := pool.AcquireTaskSlot(ctx)
		if err != nil {
			t.Fatalf("task slot %d error: %v", i, err)
		}
		rel()
	}
}

func TestStorageArbiter_StatsAndLock(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.db")

	// 1. Stats on non-existent file
	arbiter := NewStorageArbiter(dbFile)
	stats := arbiter.Stats()
	if stats.Status != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND status, got %s", stats.Status)
	}

	// 2. Stats on created file
	if err := os.WriteFile(dbFile, []byte("sqlite-data-content"), 0600); err != nil {
		t.Fatalf("create db file: %v", err)
	}
	stats = arbiter.Stats()
	if stats.Status != "READY" || stats.SizeBytes <= 0 {
		t.Errorf("expected READY status and positive size, got status=%s size=%d", stats.Status, stats.SizeBytes)
	}

	// 3. Stats on empty path
	emptyArbiter := NewStorageArbiter("")
	emptyStats := emptyArbiter.Stats()
	if emptyStats.Status != "READY" {
		t.Errorf("expected READY for empty path, got %s", emptyStats.Status)
	}

	// 4. WithWriteLock success
	ctx := context.Background()
	called := false
	err := arbiter.WithWriteLock(ctx, func() error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("WithWriteLock failed: err=%v called=%v", err, called)
	}

	// 5. WithWriteLock error propagation
	expectedErr := errors.New("write failed")
	err = arbiter.WithWriteLock(ctx, func() error {
		return expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	// 6. WithWriteLock canceled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = arbiter.WithWriteLock(canceledCtx, func() error {
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestSupervisor_RegistryAndCache(t *testing.T) {
	sup := NewSupervisor()
	sup.RegisterTool(ToolConfig{
		Name:    "mock-tool",
		Command: "echo",
	})

	tool, ok := sup.GetTool("mock-tool")
	if !ok || tool == nil {
		t.Fatalf("expected tool mock-tool to be found")
	}

	_, ok = sup.GetTool("nonexistent")
	if ok {
		t.Fatalf("expected nonexistent tool to not be found")
	}

	list := sup.ListTools()
	if len(list) != 1 || list["mock-tool"].Status != "REGISTERED" {
		t.Errorf("unexpected ListTools result: %+v", list)
	}

	// Test intercepting initialize and tools/list using cached results
	tool.initResult = json.RawMessage(`{"capabilities":{"tools":{}}}`)
	tool.toolsResult = json.RawMessage(`{"tools":[{"name":"test"}]}`)

	ctx := context.Background()
	initReq := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"1"`),
		Method:  "initialize",
	}
	resp, err := tool.HandleClientRequest(ctx, initReq)
	if err != nil {
		t.Fatalf("HandleClientRequest initialize error: %v", err)
	}
	if string(resp.Result) != string(tool.initResult) {
		t.Errorf("expected cached init result, got %s", string(resp.Result))
	}

	toolsReq := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"2"`),
		Method:  "tools/list",
	}
	resp, err = tool.HandleClientRequest(ctx, toolsReq)
	if err != nil {
		t.Fatalf("HandleClientRequest tools/list error: %v", err)
	}
	if string(resp.Result) != string(tool.toolsResult) {
		t.Errorf("expected cached tools result, got %s", string(resp.Result))
	}

	// Stop & StopAll
	_ = tool.Stop()
	sup.StopAll()
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "omca-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return filepath.Join(dir, "s.sock")
}

func TestHub_LifecycleAndSocket(t *testing.T) {
	sockPath := shortSocketPath(t)

	cfg := &Config{
		SocketPath: sockPath,
		Port:       0,
		Tools:      make(map[string]ToolConfig),
	}

	h := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := h.Start(ctx); err != nil {
		t.Fatalf("Hub.Start failed: %v", err)
	}

	snap := h.DashboardSnapshot()
	if snap.Storage.Status != "READY" {
		t.Errorf("unexpected storage status: %s", snap.Storage.Status)
	}
	if h.Supervisor() == nil || h.Pool() == nil || h.Arbiter() == nil {
		t.Errorf("expected non-nil Hub subsystems")
	}

	if err := h.Close(); err != nil {
		t.Fatalf("Hub.Close failed: %v", err)
	}
}

func TestHub_ClientConnectionAndDispatch(t *testing.T) {
	sockPath := shortSocketPath(t)

	cfg := &Config{
		SocketPath: sockPath,
		Port:       0,
		Tools: map[string]ToolConfig{
			"registered-tool": {
				Name:    "registered-tool",
				Command: "echo",
			},
		},
	}

	h := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := h.Start(ctx); err != nil {
		t.Fatalf("Hub.Start failed: %v", err)
	}
	defer h.Close()

	// Connect client
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Dial unix error: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// 1. Send bridge_attach handshake
	attachFrame := `{"type":"bridge_attach","server":"unregistered-tool","host_name":"test-host"}` + "\n"
	if _, err := conn.Write([]byte(attachFrame)); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	// Verify host is registered in hub
	time.Sleep(50 * time.Millisecond)
	hosts := h.ActiveHosts()
	if len(hosts) != 1 || hosts[0].HostName != "test-host" {
		t.Fatalf("expected host registered, got %+v", hosts)
	}

	// 2. Send JSON-RPC request for unregistered tool -> should receive -32601 error
	reqLine := `{"jsonrpc":"2.0","id":"req-42","method":"tools/list"}` + "\n"
	if _, err := conn.Write([]byte(reqLine)); err != nil {
		t.Fatalf("write rpc line: %v", err)
	}

	respLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read rpc response: %v", err)
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal([]byte(respLine), &rpcResp); err != nil {
		t.Fatalf("unmarshal rpc response %q: %v", respLine, err)
	}
	if rpcResp.Error == nil || rpcResp.Error.Code != -32601 {
		t.Errorf("expected -32601 tool not registered error, got %+v", rpcResp.Error)
	}

	// 3. Send notification (no ID) and invalid JSON - should not crash
	_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/ping"}` + "\n"))
	_, _ = conn.Write([]byte(`invalid-json-frame` + "\n"))
	time.Sleep(50 * time.Millisecond)
}

func TestBridge_RunBridge(t *testing.T) {
	sockPath := shortSocketPath(t)

	// Set up dummy unix listener
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}
	defer l.Close()

	// Background server handler
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		// Read attach frame
		_, _ = reader.ReadString('\n')

		// Read request
		req, _ := reader.ReadString('\n')
		if strings.Contains(req, "test_ping") {
			// Write response
			_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":"pong"}` + "\n"))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stdin := bytes.NewBufferString(`{"jsonrpc":"2.0","id":"1","method":"test_ping"}` + "\n")
	stdoutR, stdoutW := io.Pipe()

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunBridge(ctx, "mock-tool", sockPath, "test-client", stdin, stdoutW)
		_ = stdoutW.Close()
	}()

	var stdout bytes.Buffer
	_, _ = io.Copy(&stdout, stdoutR)
	<-errCh

	if !strings.Contains(stdout.String(), "pong") {
		t.Errorf("expected bridge to pipe response containing 'pong', got %q", stdout.String())
	}
}
