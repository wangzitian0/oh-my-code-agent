package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
)

// maxLineBytes bounds one JSON-RPC message. internal/mcp/server.go's own
// stub server tops out at 4MiB because its whole vocabulary is small
// (no-argument tool calls and a status summary); this transport's Compile/
// Verify traffic carries whole native config file bodies as base64-encoded
// Artifact.Content, so the ceiling is raised further, defensively, to 64MiB
// — comfortably above any single generated config file this project's own
// adapters produce, while still failing a genuinely runaway or malformed
// line with a clear scanner error instead of an unbounded read.
const maxLineBytes = 64 * 1024 * 1024

// Serve runs the stdio JSON-RPC 2.0 server side of the M6 plugin transport:
// it reads newline-delimited JSON-RPC 2.0 requests from r, dispatches each
// one to adapter (or answers "handshake" from manifest directly), and writes
// newline-delimited JSON-RPC 2.0 responses to w — the same framing
// convention internal/mcp/server.go's own Serve already established for this
// codebase's other stdio JSON-RPC server (see doc.go).
//
// manifest is what "handshake" reports: the caller (an external adapter
// binary's own main()) supplies its own, self-declared plugin.PluginManifest
// — Serve never invents one. adapter is driven through the exact
// plugin.HostAdapter interface; Serve has no knowledge of any concrete
// adapter type, so it works unmodified for a first-party adapter wrapped for
// testing (this package's own conformance parity proof) or a genuine
// third-party one.
//
// Serve returns nil when r reaches EOF (the client — normally RemoteAdapter,
// holding this process's stdin open — closed its side, e.g. Close()ing the
// subprocess), or a non-nil error only for a genuine I/O failure reading r or
// writing w. A malformed or unrecognized request is never such a failure: it
// produces a JSON-RPC error response and Serve keeps running.
func Serve(r io.Reader, w io.Writer, manifest plugin.PluginManifest, adapter plugin.HostAdapter) error {
	if adapter == nil {
		return fmt.Errorf("transport: Serve: adapter must not be nil")
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	bw := bufio.NewWriter(w)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		resp := handleLine(line, manifest, adapter)
		if resp == nil {
			continue // notification (no "id"): JSON-RPC 2.0 defines no response at all
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("transport: Serve: marshaling response: %w", err)
		}
		if _, err := bw.Write(data); err != nil {
			return fmt.Errorf("transport: Serve: writing response: %w", err)
		}
		if err := bw.WriteByte('\n'); err != nil {
			return fmt.Errorf("transport: Serve: writing response: %w", err)
		}
		if err := bw.Flush(); err != nil {
			return fmt.Errorf("transport: Serve: flushing response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("transport: Serve: reading request: %w", err)
	}
	return nil
}

// handleLine decodes and dispatches one newline-delimited message, returning
// the *jsonrpcResponse to write, or nil for a notification (no "id").
func handleLine(line []byte, manifest plugin.PluginManifest, adapter plugin.HostAdapter) *jsonrpcResponse {
	var req jsonrpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
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
	ctx := context.Background()

	var result any
	var rpcErr *jsonrpcError
	switch req.Method {
	case methodHandshake:
		result = manifest
	case methodDetect:
		var p plugin.DetectRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			rpcErr = invalidParamsErr(err)
			break
		}
		instances, err := adapter.Detect(ctx, p)
		if err != nil {
			rpcErr = classifyErr(err)
			break
		}
		result = detectResult{Instances: instances}
	case methodCapabilities:
		var p plugin.HostInstance
		if err := json.Unmarshal(req.Params, &p); err != nil {
			rpcErr = invalidParamsErr(err)
			break
		}
		cm, err := adapter.Capabilities(ctx, p)
		if err != nil {
			rpcErr = classifyErr(err)
			break
		}
		result = cm
	case methodObserve:
		var p plugin.ObserveRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			rpcErr = invalidParamsErr(err)
			break
		}
		obs, err := adapter.Observe(ctx, p)
		if err != nil {
			rpcErr = classifyErr(err)
			break
		}
		result = obs
	case methodResolve:
		var p plugin.ResolveRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			rpcErr = invalidParamsErr(err)
			break
		}
		effective, err := adapter.Resolve(ctx, p)
		if err != nil {
			rpcErr = classifyErr(err)
			break
		}
		result = effective
	case methodCompile:
		var p plugin.CompileRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			rpcErr = invalidParamsErr(err)
			break
		}
		artifacts, err := adapter.Compile(ctx, p)
		if err != nil {
			rpcErr = classifyErr(err)
			break
		}
		result = artifacts
	case methodVerify:
		var p plugin.VerifyRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			rpcErr = invalidParamsErr(err)
			break
		}
		evidence, err := adapter.Verify(ctx, p)
		if err != nil {
			rpcErr = classifyErr(err)
			break
		}
		result = evidence
	case methodLaunch:
		var p plugin.LaunchRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			rpcErr = invalidParamsErr(err)
			break
		}
		if err := adapter.Launch(ctx, p); err != nil {
			rpcErr = classifyErr(err)
			break
		}
		result = struct{}{}
	default:
		rpcErr = &jsonrpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}

	if isNotification {
		return nil
	}
	if rpcErr != nil {
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		// Every result type a case above produces is a plain, fully
		// JSON-marshalable struct from internal/plugin/contract.go (no
		// funcs/channels/unexported fields, by that package's own doc
		// comment) — this should never actually happen, but failing the
		// individual call with a clear error is still strictly better than
		// a response line that Marshal itself cannot even produce.
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &jsonrpcError{Code: codeAdapterError, Message: "marshaling result: " + err.Error()}}
	}
	return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultJSON}
}

func invalidParamsErr(err error) *jsonrpcError {
	return &jsonrpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
}

// classifyErr maps an error plugin.HostAdapter method returned onto this
// wire protocol's small, closed set of application error codes, so the
// client side (errorFromRPC, client.go) can reconstruct the exact sentinel
// with errors.Is(err, plugin.ErrNotDetected) (etc.) still true after
// crossing the process boundary — the property that makes running
// internal/plugin/conformance.Run against a RemoteAdapter a faithful parity
// proof rather than a weaker approximation of one.
func classifyErr(err error) *jsonrpcError {
	switch {
	case errors.Is(err, plugin.ErrNotDetected):
		return &jsonrpcError{Code: codeNotDetected, Message: err.Error()}
	case errors.Is(err, plugin.ErrUnsupportedOperation):
		return &jsonrpcError{Code: codeUnsupportedOperation, Message: err.Error()}
	case errors.Is(err, plugin.ErrCapabilityDenied):
		return &jsonrpcError{Code: codeCapabilityDenied, Message: err.Error()}
	default:
		return &jsonrpcError{Code: codeAdapterError, Message: err.Error()}
	}
}
