package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

// TestHelperFakeMCPServer is not a test. The hub starts the test binary with
// OMCA_FAKE_MCP_SERVER=1 as a tool subprocess. It answers each request with the
// ID it received, which is the hub-internal ID. A method error answers like a
// real server that does not know the method.
func TestHelperFakeMCPServer(t *testing.T) {
	if os.Getenv("OMCA_FAKE_MCP_SERVER") != "1" {
		t.Skip("helper process only")
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req JSONRPCRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil || len(req.ID) == 0 {
			continue
		}
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
		if req.Method == "server/discover" {
			resp.Error = &JSONRPCError{Code: -32601, Message: "Method not found: server/discover"}
		} else {
			resp.Result = json.RawMessage(fmt.Sprintf(`{"method":%q}`, req.Method))
		}
		out, _ := json.Marshal(resp)
		fmt.Println(string(out))
	}
	os.Exit(0)
}

func fakeMCPTool(name string) ToolConfig {
	return ToolConfig{
		Name:    name,
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestHelperFakeMCPServer$"},
		Env:     map[string]string{"OMCA_FAKE_MCP_SERVER": "1"},
	}
}

// A client matches a response to its request by ID. The hub rewrites the ID
// toward the tool, so it must restore the client ID on every forwarded
// response, the error responses included. Before the fix, the client got the
// internal "req-N" ID, and a host that sends a non-cached request first
// (Antigravity sends server/discover) waited for a reply that never came.
func TestHub_ForwardedResponsesEchoClientRequestID(t *testing.T) {
	sockPath := shortSocketPath(t)
	cfg := &Config{
		SocketPath: sockPath,
		Tools:      map[string]ToolConfig{"fake": fakeMCPTool("fake")},
	}
	h := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Hub.Start failed: %v", err)
	}
	defer h.Close()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	reader := bufio.NewReader(conn)

	if _, err := conn.Write([]byte(`{"type":"bridge_attach","server":"fake","host_name":"test"}` + "\n")); err != nil {
		t.Fatalf("write attach: %v", err)
	}

	cases := []struct {
		line    string
		wantID  string
		wantErr bool
	}{
		// Forwarded error: the first request that Antigravity sends.
		{`{"jsonrpc":"2.0","id":7,"method":"server/discover"}`, `7`, true},
		// Served from the cache that the hub filled at tool start.
		{`{"jsonrpc":"2.0","id":8,"method":"initialize","params":{}}`, `8`, false},
		// Forwarded result with a string ID that looks like a hub-internal ID.
		{`{"jsonrpc":"2.0","id":"req-1","method":"ping"}`, `"req-1"`, false},
		{`{"jsonrpc":"2.0","id":"client-x","method":"tools/call","params":{}}`, `"client-x"`, false},
	}
	for _, c := range cases {
		if _, err := conn.Write([]byte(c.line + "\n")); err != nil {
			t.Fatalf("write %s: %v", c.line, err)
		}
		respLine, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read response to %s: %v", c.line, err)
		}
		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(respLine), &resp); err != nil {
			t.Fatalf("unmarshal %q: %v", respLine, err)
		}
		if string(resp.ID) != c.wantID {
			t.Errorf("request %s: response ID = %s, want %s (response %s)", c.line, resp.ID, c.wantID, respLine)
		}
		if (resp.Error != nil) != c.wantErr {
			t.Errorf("request %s: error = %+v, want error %v", c.line, resp.Error, c.wantErr)
		}
	}
}

// The forwarding call must not change the request of its caller.
func TestManagedTool_CallRawKeepsCallerRequestID(t *testing.T) {
	sup := NewSupervisor()
	tool := sup.GetOrCreateTool("fake", fakeMCPTool("fake"))
	defer func() { _ = tool.Stop() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req := &JSONRPCRequest{JSONRPC: "2.0", ID: json.RawMessage(`42`), Method: "ping"}
	resp, err := tool.HandleClientRequest(ctx, req)
	if err != nil {
		t.Fatalf("HandleClientRequest: %v", err)
	}
	if string(req.ID) != `42` {
		t.Errorf("caller request ID changed to %s, want 42", req.ID)
	}
	if string(resp.ID) != `42` {
		t.Errorf("response ID = %s, want 42", resp.ID)
	}
	if string(resp.Result) != `{"method":"ping"}` {
		t.Errorf("response result = %s, want the ping echo from the tool", resp.Result)
	}
}
