package transport

import (
	"encoding/json"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
)

// Wire method names: one per plugin.HostAdapter method, plus "handshake"
// (not part of the HostAdapter interface itself — see doc.go). HostAdapter's
// bare ID() accessor has no wire method at all: RemoteAdapter answers ID()
// from the AdapterID handshake already returned, never a fresh round trip,
// matching the interface's own "no context, no error" shape for that one
// method.
const (
	methodHandshake    = "handshake"
	methodDetect       = "detect"
	methodCapabilities = "capabilities"
	methodObserve      = "observe"
	methodResolve      = "resolve"
	methodCompile      = "compile"
	methodVerify       = "verify"
	methodLaunch       = "launch"
)

// jsonrpcRequest / jsonrpcResponse / jsonrpcError mirror internal/mcp/
// server.go's identical shapes: this package deliberately reuses the same
// JSON-RPC 2.0 envelope convention rather than inventing a second one in the
// same codebase. ID is a bare integer request counter encoded as
// json.RawMessage so the response's echoed id can be compared byte-for-byte
// without an intermediate allocation.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error codes. The four standard JSON-RPC 2.0 codes match internal/mcp/
// server.go's own constants exactly. The four below them are this package's
// own reserved-range (-32000 to -32099, "Server error", per the JSON-RPC 2.0
// spec) application codes: how a contract-taxonomy error (internal/plugin's
// ErrNotDetected/ErrUnsupportedOperation/ErrCapabilityDenied) crosses the
// wire as an identifiable value rather than an opaque string a client would
// have to pattern-match — see classifyErr (server.go) and errorFromRPC
// (client.go), the encode/decode pair that keeps errors.Is(err,
// plugin.ErrNotDetected) true across the process boundary.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602

	codeNotDetected          = -32001
	codeUnsupportedOperation = -32002
	codeCapabilityDenied     = -32003
	codeAdapterError         = -32000 // any other adapter-returned error
)

// detectResult is "detect"'s result shape: plugin.HostAdapter.Detect returns
// a bare []plugin.HostInstance, wrapped in a named field (rather than
// transmitted as a bare JSON array) so a future additive field can be added
// to this result without an incompatible wire-shape change.
type detectResult struct {
	Instances []plugin.HostInstance `json:"instances"`
}
