package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/version"
)

// protocolVersion is the MCP protocol date-version this stub server
// declares in its initialize response (verified against the official
// Model Context Protocol specification's 2025-06-18 schema.ts: InitializeResult
// carries protocolVersion/capabilities/serverInfo). This package does not
// negotiate a client-requested version beyond echoing this fixed value —
// there is exactly one behavior this server has ever offered, so there is
// nothing to negotiate.
const protocolVersion = "2025-06-18"

// toolNameStatus / toolNameQuery / toolNamePropose / toolNameStage are the
// four tools docs/project/roadmap.md's M4 milestone names in full
// (PR-11's original toolNameStatus, PR-20/issue #24's toolNameQuery, and
// PR-21/issue #25's toolNamePropose/toolNameStage) — see Registry's doc
// comment for why adding these two did not require touching this file's
// dispatch logic at all.
const (
	toolNameStatus  = "omca_status"
	toolNameQuery   = "omca_query"
	toolNamePropose = "omca_propose"
	toolNameStage   = "omca_stage"
)

// statusToolDescription is what tools/list reports for omca_status —
// deliberately small (docs/architecture/runtime.md §6's M4 exit-gate design
// goal, "tool schemas and default responses remain deliberately small",
// already the standard this M1 stub holds itself to).
const statusToolDescription = "Report the current OMCA-managed context: worktree/context identity, the current generation ID per managed host, the count of native user-global MCP servers and Skills excluded from this managed session versus an unmanaged native launch, an estimated context-cost delta, and whether this session's own host is running on a generation that has since been superseded by a newer activation (restartRequired). Takes no arguments."

// StatusFunc computes the current omca_status result on demand. Serve calls
// it fresh for every tools/call request (a status read is cheap — see
// status.go's ComputeStatus doc comment — and a long-lived MCP server must
// never answer from a value computed once at startup, since the generation
// omca_status reports on can change during the server's own lifetime, e.g.
// a restart-activated new generation).
type StatusFunc func() (StatusResult, error)

// toolHandler executes one tool's "tools/call" and returns the raw result
// value to render (see renderCallToolResult) or a tool-level error — the
// same isError:true-shaped outcome handleToolsCall's own doc comment
// distinguishes from a protocol-level *jsonrpcError. arguments is the
// tools/call params' raw, not-yet-decoded "arguments" object
// (json.RawMessage(nil) when the caller supplied none, mirroring
// toolsCallParams.Arguments' existing contract) — each handler decodes its
// own tool-specific shape; this generalized dispatch layer never inspects
// argument content itself.
type toolHandler func(arguments json.RawMessage) (any, error)

// ToolEntry is one registered tool: its tools/list definition paired with
// the handler tools/call dispatches to by name. Its fields are unexported
// (only this package knows toolDefinition/toolHandler's shape); a caller
// outside this package builds one only through a tool-specific constructor
// (StatusToolEntry, QueryToolEntry, ProposeToolEntry, StageToolEntry) and
// otherwise treats the returned value as opaque.
type ToolEntry struct {
	definition toolDefinition
	handler    toolHandler
}

// Registry is this server's complete tool surface: tools/list projects
// entries (in registration order, so tools/list is deterministic across
// calls rather than a random map-iteration order) and tools/call dispatches
// through byName. Building both from the SAME ordered slice — rather than,
// as the PR-11 stub did, tools/list and tools/call each independently
// hardcoding their own one-tool knowledge — is issue #24's round-4 audit
// generalization ("Generalize internal/mcp/server.go's tool dispatch into a
// real name-to-handler registry"): a tool registered here can never be
// present in tools/list but missing from tools/call's dispatch (or vice
// versa).
//
// issue #25's own round-4 audit goes one step further: Serve itself now
// takes an already-built Registry rather than a growing list of positional
// per-tool callback parameters (Serve(r, w, status StatusFunc, query
// ArtifactFunc) error, PR-20's shape) — a signature that would otherwise
// grow by one parameter for every future tool this server ever adds.
// cmd/omca/mcp.go's runMCP composes the full four-tool Registry itself
// (NewRegistry(StatusToolEntry(...), QueryToolEntry(...),
// ProposeToolEntry(...), StageToolEntry(...))) and hands the result to
// Serve — this file never grows again when a fifth tool arrives; only
// runMCP's own registry-composition call site does.
type Registry struct {
	entries []ToolEntry
	byName  map[string]toolHandler
}

// NewRegistry builds a Registry from entries, in the order given — that
// order is exactly what tools/list reports (toolsListResult). A later entry
// naming the same tool as an earlier one overwrites the earlier one's
// dispatch handler but not its position in tools/list ordering, matching
// Go's own map-literal "last write wins" convention; every production call
// site (runMCP) names each tool exactly once, so this never triggers
// outside of a deliberately adversarial test.
func NewRegistry(entries ...ToolEntry) Registry {
	byName := make(map[string]toolHandler, len(entries))
	for _, e := range entries {
		byName[e.definition.Name] = e.handler
	}
	return Registry{entries: entries, byName: byName}
}

// names returns every registered tool name, in tools/list order — used only
// to compose an "unknown tool" error message a client can act on (e.g. "did
// you mean...").
func (t Registry) names() []string {
	names := make([]string, 0, len(t.entries))
	for _, e := range t.entries {
		names = append(names, e.definition.Name)
	}
	sort.Strings(names) // error-message cosmetics only; tools/list itself uses registration order below
	return names
}

// StatusToolEntry builds the registered omca_status ToolEntry from a
// StatusFunc — factored out of the old newToolRegistry so cmd/omca/mcp.go
// can compose it directly (Registry's own doc comment explains why).
func StatusToolEntry(status StatusFunc) ToolEntry {
	return ToolEntry{
		definition: toolDefinition{
			Name:        toolNameStatus,
			Description: statusToolDescription,
			InputSchema: noArgumentsInputSchema(),
		},
		handler: func(json.RawMessage) (any, error) { return status() },
	}
}

// QueryToolEntry builds the registered omca_query ToolEntry from an
// ArtifactFunc.
func QueryToolEntry(query ArtifactFunc) ToolEntry {
	return ToolEntry{
		definition: toolDefinition{
			Name:        toolNameQuery,
			Description: queryToolDescription,
			InputSchema: queryInputSchema(),
		},
		handler: queryToolHandler(query),
	}
}

// noArgumentsInputSchema is the shared inputSchema for a tool that takes no
// arguments at all — omca_status's shape, factored out so a future
// no-argument tool (PR-21) does not need to redeclare the same four-line
// literal.
func noArgumentsInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

// jsonrpcRequest is the subset of a JSON-RPC 2.0 request/notification this
// server reads. ID is nil for a notification (per the JSON-RPC 2.0 spec, a
// message with no "id" member) — encoding/json.RawMessage's own MarshalJSON
// already renders a nil value as the literal `null`, so a caller that
// forwards ID straight into a jsonrpcResponse never needs a separate
// "was there an id at all" branch on the write side.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonrpcResponse is the subset of a JSON-RPC 2.0 response this server
// writes: exactly one of Result/Error is set, matching the spec's mutual
// exclusivity. ID intentionally has no `omitempty`: a response with an
// unknown original ID (e.g. a parse error, where the request could not even
// be decoded enough to find its id) must still emit `"id":null` per the
// spec, and json.RawMessage(nil) already marshals to that literal.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcError is a JSON-RPC 2.0 error object. Code follows the spec's
// reserved ranges (-32700 parse error, -32601 method not found, -32602
// invalid params) — this server never invents a custom application-level
// code, since it has no application-level error condition beyond "which
// well-known JSON-RPC problem is this."
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Serve runs the stdio MCP server: reads newline-delimited JSON-RPC 2.0
// messages from r, dispatches them, and writes newline-delimited JSON-RPC
// 2.0 responses to w — exactly the stdio transport's documented contract
// ("Messages are individual JSON-RPC requests, notifications, or responses,
// delimited by newlines, and MUST NOT contain embedded newlines... the
// server MUST NOT write anything to its stdout that is not a valid MCP
// message," per the official specification's transports.mdx). Every
// response is written and flushed before the next request is read — this
// server processes one message at a time, matching the "single fixed-schema
// tool, no concurrency machinery" scope this package's doc.go documents.
//
// Serve returns nil when r reaches EOF (the client, normally the host
// process holding this server's stdin open, closed its side — e.g. the host
// session itself exiting), or a non-nil error only for a genuine I/O
// failure reading r or writing w. A malformed or unrecognized JSON-RPC
// message is never such a failure: it produces a JSON-RPC error response
// (or, for a notification, no response at all) and Serve keeps running,
// exactly like a long-lived server must.
//
// registry is the complete, already-built tool table (Registry's own doc
// comment explains why Serve takes this rather than a per-tool callback
// parameter list) — the caller (runMCP) composes it once, before calling
// Serve, from whichever StatusFunc/ArtifactFunc/CapabilityFunc/CompileFunc
// callbacks this process's own environment resolved.
func Serve(r io.Reader, w io.Writer, registry Registry) error {
	scanner := bufio.NewScanner(r)
	// The default 64KiB token limit is already generous for this server's
	// entire message vocabulary (a no-argument tools/call request, or an
	// omca_status response naming at most a couple of hosts); this raises it
	// only defensively, to 4MiB, so an oversized or malformed line from a
	// misbehaving client fails as a clear scanner error rather than being
	// silently truncated.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	bw := bufio.NewWriter(w)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		resp := handleLine(line, registry)
		if resp == nil {
			continue // notification: JSON-RPC 2.0 defines no response at all
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("mcp: Serve: marshaling response: %w", err)
		}
		if _, err := bw.Write(data); err != nil {
			return fmt.Errorf("mcp: Serve: writing response: %w", err)
		}
		if err := bw.WriteByte('\n'); err != nil {
			return fmt.Errorf("mcp: Serve: writing response: %w", err)
		}
		if err := bw.Flush(); err != nil {
			return fmt.Errorf("mcp: Serve: flushing response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("mcp: Serve: reading request: %w", err)
	}
	return nil
}

// handleLine decodes and dispatches one newline-delimited message, returning
// the *jsonrpcResponse to write, or nil when line was a notification (no
// "id") and therefore gets no response under any circumstance, including an
// error — a notification's sender has, by construction, declared it is not
// waiting for one.
func handleLine(line []byte, registry Registry) *jsonrpcResponse {
	var req jsonrpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		// encoding/json.Unmarshal populates every struct field it CAN parse
		// before returning the first error it hit (e.g. a type-mismatched
		// "method" value does not stop it from having already set req.ID) --
		// so req.ID may be genuinely known even on this error path, and must
		// be echoed back rather than forced to null: a strict client
		// correlating responses by id would otherwise never match this
		// error to its pending request and could hang forever. Only when
		// the id itself could not be determined (a *json.SyntaxError, or any
		// failure severe enough that no field was populated) does the
		// JSON-RPC 2.0 spec's id:null apply.
		//
		// The error code also distinguishes the two cases: a *json.SyntaxError
		// means the bytes are not valid JSON at all (Parse error, -32700);
		// anything else (e.g. *json.UnmarshalTypeError) means the JSON parsed
		// fine but does not shape a valid JSON-RPC request (Invalid Request,
		// -32600).
		code := codeInvalidRequest
		message := "invalid request: " + err.Error()
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			code = codeParseError
			message = "parse error: " + err.Error()
		}
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &jsonrpcError{Code: code, Message: message}}
	}
	isNotification := len(req.ID) == 0

	var result any
	var rpcErr *jsonrpcError
	switch req.Method {
	case "initialize":
		result = initializeResult()
	case "notifications/initialized", "initialized":
		// The handshake notification. "initialized" (no "notifications/"
		// prefix) is this server's own lenient alias, not part of the MCP
		// spec -- accepted defensively in case a client sends the bare
		// method name. Handling both through the SAME isNotification-gated
		// path below (rather than an unconditional `return nil` here, this
		// package's own earlier bug) matters because a well-behaved client
		// only ever sends this as a notification (no "id"), but a
		// malformed or unusual client that attaches an id to it is a
		// REQUEST per the JSON-RPC 2.0 spec's own id-presence rule and
		// still deserves a response, or it would hang forever waiting for
		// one that never comes.
		result = struct{}{}
	case "ping":
		result = struct{}{}
	case "tools/list":
		result = toolsListResult(registry)
	case "tools/call":
		result, rpcErr = handleToolsCall(req.Params, registry)
	default:
		rpcErr = &jsonrpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}

	if isNotification {
		return nil // JSON-RPC 2.0: notifications never get a response, not even an error
	}
	if rpcErr != nil {
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	}
	return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// implementation is the MCP spec's "Implementation" shape (name + version),
// used for both this server's own serverInfo and, structurally, whatever a
// client sends as clientInfo (which this server reads but does not
// otherwise use, matching an M1 stub's minimal-behavior scope).
type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeResultPayload is the "initialize" method's result shape.
type initializeResultPayload struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      implementation `json:"serverInfo"`
	Instructions    string         `json:"instructions,omitempty"`
}

// initializeResult is this server's fixed "initialize" response:
// capabilities.tools (present, listChanged:false — this server's tool list
// never changes at runtime, even though it now has more than one entry) and
// nothing else, matching this server's actual, narrow capability surface.
func initializeResult() initializeResultPayload {
	return initializeResultPayload{
		ProtocolVersion: protocolVersion,
		Capabilities: map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		ServerInfo:   implementation{Name: "omca", Version: version.Version},
		Instructions: "This is the OMCA MCP control surface: omca_status (worktree/context/generation summary), omca_query (logical entities, drift cards, Knowledge evidence, generation sources, and the report artifact overview), omca_propose (validate a RepairProposal document against report fingerprint, schema, capability gates, ownership, and policy, and classify its risk), and omca_stage (fully re-validate and, only for an AUTO_STAGE proposal, compile it into a fresh pending generation) -- all scoped to this process's own bound worktree/generation, never caller-selectable, and never able to bypass a confirmation class. See docs/architecture/reporting.md §11 and docs/product/requirements.md §7-8 for the full control surface.",
	}
}

// toolDefinition is one entry in tools/list's result.
type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// toolsListResult projects registry's entries into the tools/list result, in
// registration order — see Registry's doc comment for why this and
// tools/call's dispatch (handleToolsCall) always agree on the exact same
// tool set.
func toolsListResult(registry Registry) map[string]any {
	defs := make([]toolDefinition, 0, len(registry.entries))
	for _, e := range registry.entries {
		defs = append(defs, e.definition)
	}
	return map[string]any{"tools": defs}
}

// toolsCallParams is "tools/call"'s params shape: which tool, and its
// arguments (a client-supplied arguments object is always valid JSON-RPC —
// even for omca_status, which accepts none — and must never itself cause a
// parse failure; each toolHandler decides what, if anything, to do with a
// non-empty Arguments).
type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// contentBlock is one entry in a CallToolResult's content array — this
// server only ever emits the "text" block type.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// callToolResult is the "tools/call" method's result shape (MCP spec's
// CallToolResult): Content is the mandatory, unstructured rendering;
// StructuredContent is the optional, machine-readable twin carrying the
// exact same information as a typed JSON object rather than serialized text
// — both are populated here so a text-only client and a structured-content-
// aware client each get a complete answer from one response.
type callToolResult struct {
	Content           []contentBlock `json:"content"`
	StructuredContent any            `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
}

// handleToolsCall implements "tools/call". Per the MCP specification's own
// guidance (CallToolResult's doc comment: "Errors originating from the tool
// itself should be reported within the result object with isError set to
// true... Protocol-level errors, such as issues finding the tool... should
// be handled differently"), an unknown tool name is a protocol-level error
// (returned as *jsonrpcError, this method's second return value), while the
// matched handler itself failing is a tool-level error (returned as a
// normal result with IsError: true, first return value) — a client can
// recover from the latter without treating the whole JSON-RPC exchange as
// broken. This dispatch logic itself is now tool-agnostic (issue #24's
// round-4 audit): it looks the handler up in registry.byName by name and
// renders whatever it returns, the same way regardless of which tool was
// called — the PR-11 stub's single hardcoded `p.Name != toolNameStatus`
// check is gone.
func handleToolsCall(params json.RawMessage, registry Registry) (any, *jsonrpcError) {
	var p toolsCallParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &jsonrpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
		}
	}
	handler, ok := registry.byName[p.Name]
	if !ok {
		return nil, &jsonrpcError{Code: codeInvalidParams, Message: fmt.Sprintf("unknown tool %q (this server exposes: %s)", p.Name, strings.Join(registry.names(), ", "))}
	}

	result, err := handler(p.Arguments)
	if err != nil {
		return callToolResult{
			Content: []contentBlock{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}
	return renderCallToolResult(p.Name, result), nil
}

// renderCallToolResult wraps a successful handler result into the shared
// CallToolResult shape every tool uses: a pretty-printed JSON text block
// (for a text-only client) plus the identical value as StructuredContent
// (for a structured-content-aware client) — the exact rendering the PR-11
// stub's handleToolsCall originally did only for omca_status, factored out
// so every tool in the registry renders identically rather than each
// reimplementing it.
func renderCallToolResult(toolName string, result any) callToolResult {
	pretty, err := json.MarshalIndent(result, "", "  ")
	text := string(pretty)
	if err != nil {
		// Every result type this package's handlers return is a fixed,
		// entirely JSON-marshalable struct (no channels, funcs, or cyclic
		// pointers) — this should never actually happen, but a text
		// fallback is cheap insurance against ever returning a response
		// with an empty content array, which the MCP spec requires to be
		// non-empty.
		text = fmt.Sprintf("%s: %+v", toolName, result)
	}
	return callToolResult{
		Content:           []contentBlock{{Type: "text", Text: text}},
		StructuredContent: result,
	}
}
