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
	cfg, drift, err := LoadConfig(nonExistent, false)
	if err != nil {
		t.Fatalf("LoadConfig(nonexistent) error: %v", err)
	}
	if drift != nil {
		t.Errorf("expected no drift for a nonexistent config, got %+v", drift)
	}
	if cfg.Port != 8765 {
		t.Errorf("expected default Port 8765, got %d", cfg.Port)
	}
	if cfg.SocketPath == "" {
		t.Errorf("expected non-empty SocketPath")
	}
	if _, err := os.Stat(nonExistent + ".sha256"); !os.IsNotExist(err) {
		t.Errorf("expected no digest sidecar written for a nonexistent config, stat err=%v", err)
	}

	// 2. Invalid JSON returns error
	badJSONPath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badJSONPath, []byte("{invalid-json"), 0600); err != nil {
		t.Fatalf("write bad JSON: %v", err)
	}
	_, _, err = LoadConfig(badJSONPath, false)
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
	cfg, drift, err = LoadConfig(validPath, false)
	if err != nil {
		t.Fatalf("LoadConfig(valid) error: %v", err)
	}
	if drift != nil {
		t.Errorf("expected no drift on a first-ever load (trust-on-first-use), got %+v", drift)
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
	// A first-ever load has no sidecar to compare against, so LoadConfig
	// establishes one (issue #124 contract #1) instead of refusing.
	sidecarData, err := os.ReadFile(validPath + ".sha256")
	if err != nil {
		t.Fatalf("expected LoadConfig to write a digest sidecar on first load: %v", err)
	}
	if got := parseDigestSidecar(sidecarData); got != sha256Hex([]byte(validJSON)) {
		t.Errorf("sidecar digest = %q, want sha256(%q) = %q", got, validJSON, sha256Hex([]byte(validJSON)))
	}

	// 4. Reloading the same, unchanged file matches its own new sidecar.
	cfg, drift, err = LoadConfig(validPath, false)
	if err != nil {
		t.Fatalf("LoadConfig(valid) second load error: %v", err)
	}
	if drift != nil {
		t.Errorf("expected no drift reloading an unchanged file, got %+v", drift)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected Port 9090 on reload, got %d", cfg.Port)
	}
}

// TestConfig_SecretLiteralRejected covers issue #124 contract #3: a
// harness.json with an env value that looks like a hand-embedded secret
// must fail LoadConfig with the offending JSON path in the error, in every
// location Env can appear (tools, shared_tools, profiles.*.tools). A
// reference ($VAR or op://) or a short value must load cleanly.
func TestConfig_SecretLiteralRejected(t *testing.T) {
	longSecret := strings.Repeat("a", 40)

	write := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "harness.json")
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		return p
	}

	cases := []struct {
		name       string
		body       string
		wantErr    bool
		wantInPath string // substring the error's JSON path must contain
	}{
		{
			name: "literal in shared_tools",
			body: `{"shared_tools":{"gh":{"name":"gh","command":"gh","shared_allow":true,
				"env":{"GITHUB_TOKEN":"` + longSecret + `"}}}}`,
			wantErr:    true,
			wantInPath: "shared_tools.gh.env",
		},
		{
			name: "literal in profiles.*.tools",
			body: `{"profiles":{"work":{"workspace_roots":["/x"],"tools":{"gh":{"name":"gh",
				"command":"gh","env":{"GITHUB_PAT":"` + longSecret + `"}}}}}}`,
			wantErr:    true,
			wantInPath: "profiles.work.tools.gh.env",
		},
		{
			name: "literal in top-level tools",
			body: `{"tools":{"gh":{"name":"gh","command":"gh",
				"env":{"API_KEY":"` + longSecret + `"}}}}`,
			wantErr:    true,
			wantInPath: "tools.gh.env",
		},
		{
			name: "env_files-style $VAR reference is not a literal",
			body: `{"tools":{"gh":{"name":"gh","command":"gh",
				"env":{"GITHUB_TOKEN":"$GITHUB_TOKEN"}}}}`,
			wantErr: false,
		},
		{
			name: "op:// reference is not a literal",
			body: `{"tools":{"gh":{"name":"gh","command":"gh",
				"env":{"GITHUB_TOKEN":"op://vault/item/field"}}}}`,
			wantErr: false,
		},
		{
			name: "short value under the length floor is not flagged",
			body: `{"tools":{"gh":{"name":"gh","command":"gh",
				"env":{"GITHUB_TOKEN":"short"}}}}`,
			wantErr: false,
		},
		{
			name: "long value whose key doesn't look credential-shaped is not flagged",
			body: `{"tools":{"gh":{"name":"gh","command":"gh",
				"env":{"DESCRIPTION":"` + longSecret + `"}}}}`,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := write(t, tc.body)
			_, _, err := LoadConfig(path, false)
			if tc.wantErr && err == nil {
				t.Fatalf("expected LoadConfig to reject a literal secret, got nil error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected LoadConfig to accept a non-literal value, got: %v", err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.wantInPath) {
				t.Errorf("error %q does not name the offending path %q", err.Error(), tc.wantInPath)
			}
		})
	}
}

// TestConfig_SharedToolsRequireAllow covers issue #124 contract #4:
// shared_tools is the cross-profile pool every profile can resolve
// (ResolveServer), so a listed tool must set shared_allow: true to opt in;
// LoadConfig refuses to load a shared_tools entry that has not.
func TestConfig_SharedToolsRequireAllow(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "harness.json")
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		return p
	}

	t.Run("missing shared_allow is rejected", func(t *testing.T) {
		path := write(t, `{"shared_tools":{"subagent-worker":{"name":"subagent-worker","command":"sw"}}}`)
		_, _, err := LoadConfig(path, false)
		if err == nil {
			t.Fatalf("expected LoadConfig to reject a shared_tools entry without shared_allow, got nil error")
		}
		if !strings.Contains(err.Error(), "shared_tools.subagent-worker") {
			t.Errorf("error %q does not name the offending tool", err.Error())
		}
	})

	t.Run("explicit shared_allow true loads cleanly", func(t *testing.T) {
		path := write(t, `{"shared_tools":{"subagent-worker":{"name":"subagent-worker","command":"sw","shared_allow":true}}}`)
		cfg, _, err := LoadConfig(path, false)
		if err != nil {
			t.Fatalf("expected LoadConfig to accept an allow-listed shared_tools entry, got: %v", err)
		}
		if !cfg.SharedTools["subagent-worker"].SharedAllow {
			t.Errorf("expected SharedAllow to round-trip as true")
		}
	})
}

// TestConfig_ResolveServer_ProfileEqualsWorkspace covers issue #124
// contract #2: profiles.<name>.workspace_roots is a workspace's identity,
// so an explicit -profile that does not name a declared, non-empty root is
// refused rather than silently falling through to the shared-tools pool.
func TestConfig_ResolveServer_ProfileEqualsWorkspace(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]ProfileConfig{
			"work": {
				WorkspaceRoots: []string{"/Users/x/workspace"},
				Tools: map[string]ToolConfig{
					"basic-memory": {Name: "basic-memory", Command: "bm-work"},
				},
			},
			"personal": {
				WorkspaceRoots: []string{"/Users/x/zitian"},
				Tools: map[string]ToolConfig{
					"basic-memory": {Name: "basic-memory", Command: "bm-personal"},
				},
			},
			"undeclared": {
				// No WorkspaceRoots: a profile section with no identity is
				// not a resolvable workspace.
				Tools: map[string]ToolConfig{
					"basic-memory": {Name: "basic-memory", Command: "bm-undeclared"},
				},
			},
		},
		SharedTools: map[string]ToolConfig{
			"subagent-worker": {Name: "subagent-worker", Command: "sw", SharedAllow: true},
		},
	}

	t.Run("declared profile resolves its own tool, never another profile's", func(t *testing.T) {
		tool, ok := cfg.ResolveServer("work", "", "basic-memory")
		if !ok || tool.Config.Command != "bm-work" {
			t.Fatalf("expected work profile's own basic-memory, got %+v ok=%v", tool, ok)
		}
		other, ok := cfg.ResolveServer("personal", "", "basic-memory")
		if !ok || other.Config.Command != "bm-personal" {
			t.Fatalf("expected personal profile's own basic-memory, got %+v ok=%v", other, ok)
		}
		if tool.Config.Command == other.Config.Command {
			t.Fatalf("work and personal resolved to the same tool config; cross-profile leak")
		}
	})

	t.Run("unknown profile name is refused, not silently resolved via shared_tools", func(t *testing.T) {
		_, ok := cfg.ResolveServer("does-not-exist", "", "subagent-worker")
		if ok {
			t.Fatalf("expected an unknown -profile to be refused rather than falling through to shared_tools")
		}
	})

	t.Run("profile with no workspace_roots is refused", func(t *testing.T) {
		_, ok := cfg.ResolveServer("undeclared", "", "basic-memory")
		if ok {
			t.Fatalf("expected a profile with empty workspace_roots to be refused")
		}
	})

	t.Run("declared profile without the requested tool still falls back to shared_tools", func(t *testing.T) {
		tool, ok := cfg.ResolveServer("work", "", "subagent-worker")
		if !ok || tool.Config.Command != "sw" {
			t.Fatalf("expected work profile to fall back to the allow-listed shared_tools entry, got %+v ok=%v", tool, ok)
		}
	})
}

// TestHub_SharedToolsRegisteredOnlyUnderCanonicalKey covers the registration
// half of contract #2 (issue #124), which the ResolveServer tests above
// cannot see: New() used to register every shared tool a second time under
// its plain name, so Supervisor.ListTools reported the same process twice
// (the plain copy mislabelled profile-scoped, since CrossProfile keys off
// the "shared:" prefix) and handleConnection's by-name fallback could reach
// it for a request whose explicit -profile ResolveServer had just refused.
// Reported by Copilot on PR #136.
func TestHub_SharedToolsRegisteredOnlyUnderCanonicalKey(t *testing.T) {
	cfg := &Config{
		SocketPath: filepath.Join(t.TempDir(), "hub.sock"),
		SharedTools: map[string]ToolConfig{
			// Map key deliberately differs from Name: ResolveServer builds
			// "shared:"+mapKey, so registering under Name would key the
			// process somewhere the lookup never goes.
			"subagent-worker": {Name: "sw-binary", Command: "sw", SharedAllow: true},
		},
	}
	h := New(cfg)

	if _, ok := h.supervisor.GetTool("shared:subagent-worker"); !ok {
		t.Errorf("expected the shared tool registered under its canonical key shared:subagent-worker (the map key ResolveServer uses)")
	}
	for _, leaked := range []string{"subagent-worker", "sw-binary", "shared:sw-binary"} {
		if _, ok := h.supervisor.GetTool(leaked); ok {
			t.Errorf("shared tool is also registered under %q; only the canonical shared:<mapKey> registration may exist", leaked)
		}
	}

	tools := h.supervisor.ListTools()
	if len(tools) != 1 {
		t.Errorf("ListTools() reported %d entries for one shared tool, want 1 (a duplicate is what `omca hub status` was double-listing): %v", len(tools), tools)
	}
	if ts, ok := tools["shared:subagent-worker"]; ok && !ts.CrossProfile {
		t.Error("the canonical shared registration must report CrossProfile=true so `omca hub status` labels it cross-profile")
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

func TestHub_ProbeSocket_And_StaleSocketRecovery(t *testing.T) {
	sockPath := shortSocketPath(t)

	// 1. Non-existent socket -> ProbeSocket returns false
	if ProbeSocket(sockPath) {
		t.Fatalf("expected non-existent socket to be not alive")
	}

	// 2. Start listener -> ProbeSocket returns true
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	if !ProbeSocket(sockPath) {
		t.Fatalf("expected listening socket to be probed alive")
	}

	// 3. Close listener without unlinking (simulates crash leaving stale socket file)
	_ = l.Close()
	if ProbeSocket(sockPath) {
		t.Fatalf("expected stale closed socket to probe as dead")
	}

	// 4. Starting Hub on stale socket path should self-heal (unlink and succeed)
	cfg := &Config{
		SocketPath: sockPath,
		Port:       0,
	}
	h := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := h.Start(ctx); err != nil {
		t.Fatalf("expected hub to self-heal stale socket and start, got %v", err)
	}
	defer h.Close()

	// 5. Trying to start another Hub while first is running should fail with error
	secondHub := New(cfg)
	if err := secondHub.Start(ctx); err == nil {
		t.Fatalf("expected second hub start to fail due to live socket, got nil")
	}
}

