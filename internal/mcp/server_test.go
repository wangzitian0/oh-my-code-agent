package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// decodeLines splits out's newline-delimited content into individual
// []byte messages, mirroring exactly how a real MCP stdio client would read
// Serve's output (bufio.Scanner over stdout, one JSON value per line) —
// this is also this test file's own proof that Serve never emits an
// embedded newline inside one message, since a message that did would
// silently split into two lines here and fail whatever assertion expected
// one.
func decodeLines(t *testing.T, out []byte) []map[string]any {
	t.Helper()
	var msgs []map[string]any
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("decodeLines: line %q did not decode as one JSON value: %v", line, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// staticStatus returns a StatusFunc always answering with a fixed,
// recognizable StatusResult — enough for these protocol-mechanics tests,
// which are not re-testing ComputeStatus's own logic (status_test.go's
// job).
func staticStatus(result StatusResult, err error) StatusFunc {
	return func() (StatusResult, error) { return result, err }
}

// staticQuery returns an ArtifactFunc always answering with a fixed
// report.Artifact — this file's protocol-mechanics tests (initialize
// handshake, tools/list shape, JSON-RPC error handling, ...) exercise
// omca_status only and never call omca_query, so a fixed, mostly-empty
// Artifact is enough; query_test.go exercises ComputeQuery/queryToolHandler
// themselves in depth.
func staticQuery(result report.Artifact, err error) ArtifactFunc {
	return func() (report.Artifact, error) { return result, err }
}

// testRegistry builds the two-tool Registry (omca_status + omca_query) this
// file's protocol-mechanics tests exercise Serve against — factored out
// once Serve stopped taking status/query as its own positional parameters
// (issue #25's round-4 audit, Registry's own doc comment) so every call
// site below only needs to name its two StatusFunc/ArtifactFunc values, not
// re-spell NewRegistry(StatusToolEntry(...), QueryToolEntry(...)) at each
// one.
func testRegistry(status StatusFunc, query ArtifactFunc) Registry {
	return NewRegistry(StatusToolEntry(status), QueryToolEntry(query))
}

// TestServe_InitializeHandshake proves the initialize request/
// notifications-initialized notification pair issue #15's "stdio JSON-RPC
// 2.0 MCP server" AC requires: initialize gets exactly one response naming
// this server's protocol version and tools capability, and the follow-up
// notification gets no response at all (JSON-RPC 2.0: notifications never
// get one).
func TestServe_InitializeHandshake(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test-client","version":"0.0.1"}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("got %d response messages, want 1 (the notification must not get one): %v", len(msgs), msgs)
	}
	result, ok := msgs[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize response has no result object: %v", msgs[0])
	}
	if result["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v, want %q", result["protocolVersion"], protocolVersion)
	}
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities is not an object: %v", result)
	}
	if _, ok := caps["tools"]; !ok {
		t.Errorf("capabilities has no tools entry: %v", caps)
	}
}

// TestServe_ToolsList_NamesOmcaStatusAndOmcaQuery proves tools/list reports
// exactly the tools named in the Registry Serve was given (here, the
// two-tool testRegistry -- omca_propose/omca_stage's own tools/list
// presence is proven separately, propose_test.go/stage_test.go), each with
// a non-empty description and inputSchema, in registration order
// (Registry's own doc comment: tools/list projects registry.entries in
// order, never a random map-iteration order).
func TestServe_ToolsList_NamesOmcaStatusAndOmcaQuery(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	result := msgs[0]["result"].(map[string]any)
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %v, want exactly two entries", result["tools"])
	}
	wantNames := []string{toolNameStatus, toolNameQuery}
	for i, wantName := range wantNames {
		tool := tools[i].(map[string]any)
		if tool["name"] != wantName {
			t.Errorf("tools[%d].name = %v, want %q", i, tool["name"], wantName)
		}
		if tool["description"] == "" {
			t.Errorf("tools[%d].description is empty", i)
		}
		if _, ok := tool["inputSchema"]; !ok {
			t.Errorf("tools[%d] has no inputSchema", i)
		}
	}
}

// TestServe_ToolsCall_OmcaStatus_ReturnsStructuredAndTextContent proves
// tools/call for omca_status returns both a human-readable text block and a
// structuredContent object carrying the exact StatusFunc result — the
// "small, well-scoped hand-roll" this package's doc.go describes, verified
// end to end through the wire format a real client would actually parse.
func TestServe_ToolsCall_OmcaStatus_ReturnsStructuredAndTextContent(t *testing.T) {
	want := StatusResult{
		WorktreeID: "worktree:sha256:abc",
		Hosts: []HostStatus{
			{Host: "codex", Managed: true, GenerationID: "generation:sha256:def", ExcludedMCPServers: 5, ExcludedSkills: 7, Detail: "managed: current generation generation:sha256:def"},
		},
	}
	input := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"omca_status","arguments":{}}}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(want, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	result := msgs[0]["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatalf("isError = true on a successful status call: %v", result)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content = %v, want at least one block", result["content"])
	}
	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Errorf("content[0].type = %v, want %q", block["type"], "text")
	}
	text, _ := block["text"].(string)
	if !strings.Contains(text, "worktree:sha256:abc") {
		t.Errorf("content[0].text does not mention the worktree ID: %q", text)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent missing or not an object: %v", result)
	}
	if structured["worktreeId"] != "worktree:sha256:abc" {
		t.Errorf("structuredContent.worktreeId = %v, want %q", structured["worktreeId"], "worktree:sha256:abc")
	}
}

// TestServe_ToolsCall_StatusFuncError_ReturnsToolLevelError proves a
// StatusFunc failure is reported as CallToolResult.isError:true (a normal,
// successful JSON-RPC response the client's LLM can see and react to), not
// a JSON-RPC protocol-level error — the MCP spec's own documented
// distinction (see server.go's handleToolsCall doc comment).
func TestServe_ToolsCall_StatusFuncError_ReturnsToolLevelError(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"omca_status","arguments":{}}}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, errors.New("boom")), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if _, hasErr := msgs[0]["error"]; hasErr {
		t.Fatalf("got a JSON-RPC protocol-level error for a tool-level failure: %v", msgs[0])
	}
	result := msgs[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("isError = %v, want true", result["isError"])
	}
	content := result["content"].([]any)
	block := content[0].(map[string]any)
	if !strings.Contains(block["text"].(string), "boom") {
		t.Errorf("content[0].text = %q, want it to mention the underlying error", block["text"])
	}
}

// TestServe_ToolsCall_UnknownTool_ReturnsProtocolLevelError proves calling
// any tool name other than omca_status is a JSON-RPC protocol-level error
// (unlike a StatusFunc failure — see the previous test), matching "finding
// the tool" being a protocol concern per the MCP spec.
func TestServe_ToolsCall_UnknownTool_ReturnsProtocolLevelError(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"omca_propose","arguments":{}}}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	errObj, ok := msgs[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("no JSON-RPC error object for an unknown tool: %v", msgs[0])
	}
	if code, _ := errObj["code"].(float64); int(code) != codeInvalidParams {
		t.Errorf("error.code = %v, want %d", errObj["code"], codeInvalidParams)
	}
}

// TestServe_UnknownMethod_Request_ReturnsMethodNotFound proves an
// unrecognized method on a request (has an id) gets a proper JSON-RPC
// "method not found" error — this is where omca_query/omca_propose/
// omca_stage (M4 scope) land today if a client tries them as top-level
// methods rather than tools/call names.
func TestServe_UnknownMethod_Request_ReturnsMethodNotFound(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":6,"method":"omca_query"}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	errObj, ok := msgs[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("no JSON-RPC error object: %v", msgs[0])
	}
	if code, _ := errObj["code"].(float64); int(code) != codeMethodNotFound {
		t.Errorf("error.code = %v, want %d", errObj["code"], codeMethodNotFound)
	}
}

// TestServe_UnknownMethod_Notification_NoResponse proves the same unknown
// method, sent as a notification (no id), gets no response whatsoever —
// distinguishing "silently ignore" (notification) from "tell the caller"
// (request), per JSON-RPC 2.0.
func TestServe_UnknownMethod_Notification_NoResponse(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"some/unknown/notification"}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("Serve wrote output for an unknown-method notification, want none: %q", out.String())
	}
}

// TestServe_MalformedJSON_ReturnsParseErrorWithNullID proves a line that
// does not even decode as JSON gets a Parse error response with id:null
// (JSON-RPC 2.0's required shape when the original id cannot be
// determined), and Serve keeps running afterward rather than treating it as
// fatal.
func TestServe_MalformedJSON_ReturnsParseErrorWithNullID(t *testing.T) {
	input := "not json at all\n" + `{"jsonrpc":"2.0","id":7,"method":"tools/list"}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2 (a parse error, then the still-processed next line): %v", len(msgs), msgs)
	}
	errObj, ok := msgs[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("first response has no error object: %v", msgs[0])
	}
	if code, _ := errObj["code"].(float64); int(code) != codeParseError {
		t.Errorf("error.code = %v, want %d", errObj["code"], codeParseError)
	}
	if id, hasID := msgs[0]["id"]; !hasID || id != nil {
		t.Errorf("id = %v (present=%v), want null", id, hasID)
	}
	if _, hasResult := msgs[1]["result"]; !hasResult {
		t.Errorf("Serve did not keep processing after the malformed line: %v", msgs[1])
	}
}

// TestServe_TypeMismatchJSON_EchoesKnownID_UsesInvalidRequestCode is a
// regression test for a real bug this PR's own review caught: a message
// that IS valid JSON but does not shape a valid jsonrpcRequest (here,
// "method" holds a number instead of a string) used to be treated
// identically to genuinely unparseable input — id forced to null and coded
// -32700 Parse error — even though encoding/json.Unmarshal had already
// successfully populated req.ID before returning the type-mismatch error.
// A strict client correlating responses by id would never match that
// response to its pending request and could hang forever. This proves the
// fix: the known id is echoed back, and the error code is -32600 Invalid
// Request (the JSON parsed fine; it just isn't a valid request), not
// -32700.
func TestServe_TypeMismatchJSON_EchoesKnownID_UsesInvalidRequestCode(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":42,"method":123}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1: %v", len(msgs), msgs)
	}
	if id, _ := msgs[0]["id"].(float64); int(id) != 42 {
		t.Errorf("id = %v, want 42 (the id encoding/json had already successfully parsed before hitting the type mismatch on \"method\")", msgs[0]["id"])
	}
	errObj, ok := msgs[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error object: %v", msgs[0])
	}
	if code, _ := errObj["code"].(float64); int(code) != codeInvalidRequest {
		t.Errorf("error.code = %v, want %d (Invalid Request, not Parse error -- the JSON itself was syntactically valid)", errObj["code"], codeInvalidRequest)
	}
	// Copilot review finding on this PR: the message text used to always
	// say "parse error: ..." regardless of which code was returned,
	// misleading a client/log reader into thinking a syntactically invalid
	// message was received when it was not.
	msg, _ := errObj["message"].(string)
	if !strings.HasPrefix(msg, "invalid request:") {
		t.Errorf("error.message = %q, want it prefixed \"invalid request:\" (matching codeInvalidRequest), not \"parse error:\"", msg)
	}
}

// TestServe_InitializedSentAsRequest_StillGetsAResponse is a regression
// test for a second real bug this PR's own review caught: the
// "notifications/initialized"/"initialized" case used to `return nil`
// unconditionally, bypassing the shared isNotification check every other
// method branch goes through. A message using this method name WITH an id
// (a malformed client, or one that — plausibly, given this server's own
// lenient bare-"initialized" alias — expects an ack) would be silently
// dropped, leaving that client waiting forever for a response that never
// arrives. Routing this method through the same isNotification-gated path
// as everything else fixes it: a notification (no id) still gets no
// response, but a request (has an id) now gets one.
func TestServe_InitializedSentAsRequest_StillGetsAResponse(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":99,"method":"initialized"}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1 (a request with an id must get a response, even for the handshake method): %v", len(msgs), msgs)
	}
	if id, _ := msgs[0]["id"].(float64); int(id) != 99 {
		t.Errorf("id = %v, want 99", msgs[0]["id"])
	}
	if _, hasError := msgs[0]["error"]; hasError {
		t.Errorf("got an error response for a plain handshake-ack request: %v", msgs[0])
	}
}

// TestServe_InitializedSentAsNotification_NoResponse is the companion
// negative control: the normal, well-behaved client case (no id) still
// gets no response at all, proving the fix above did not turn every
// "initialized" message into a response unconditionally.
func TestServe_InitializedSentAsNotification_NoResponse(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("Serve wrote output for a notifications/initialized notification, want none: %q", out.String())
	}
}

// TestServe_EOF_ReturnsNilError proves a clean client disconnect (stdin
// closes) ends Serve without error — the expected end-of-session outcome,
// not a failure.
func TestServe_EOF_ReturnsNilError(t *testing.T) {
	if err := Serve(strings.NewReader(""), &bytes.Buffer{}, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve on empty input: %v", err)
	}
}

// TestServe_NoOutputOtherThanValidMessages proves nothing but complete,
// valid JSON-RPC lines ever reaches the writer — the stdio transport's own
// documented requirement ("the server MUST NOT write anything to its stdout
// that is not a valid MCP message").
func TestServe_NoOutputOtherThanValidMessages(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	for i, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d is not valid JSON: %q: %v", i, line, err)
		}
		if m["jsonrpc"] != "2.0" {
			t.Errorf("line %d missing jsonrpc:2.0 envelope: %v", i, m)
		}
	}
}
