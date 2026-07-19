package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
)

// Compile-time proof that RemoteAdapter implements the frozen contract —
// exactly the same assurance internal/plugin/conformance/fake.go's own
// `var _ plugin.HostAdapter = (*FakeAdapter)(nil)` gives for the in-process
// reference implementation.
var _ plugin.HostAdapter = (*RemoteAdapter)(nil)

// RemoteAdapter is the client side of the M6 plugin transport: it implements
// plugin.HostAdapter by speaking this package's stdio JSON-RPC 2.0 wire
// protocol to an already-started external adapter subprocess. Because it
// implements the same interface an in-process adapter does, it can be
// registered into a plugin.Registry, driven through
// internal/plugin/conformance.Run, or used anywhere else a plugin.HostAdapter
// value is expected — the core never needs to know a given HostAdapter is
// remote.
//
// One RemoteAdapter serializes every call through a single mutex: the
// subprocess sees at most one in-flight request at a time, mirroring
// internal/mcp/server.go's own "one message at a time" stdio discipline
// (this package's doc.go) rather than adding pipelining machinery the v1
// contract's request/response shape does not need.
type RemoteAdapter struct {
	mu      sync.Mutex
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	closeFn func() error
	nextID  int64

	manifest plugin.PluginManifest
}

// newRemoteAdapter builds a RemoteAdapter directly from an already-open
// stdin writer and stdout reader, without performing a handshake — the
// building block NewRemoteAdapter (the real-subprocess constructor) and this
// package's own tests (a scripted fake stream standing in for a subprocess,
// no real binary needed) both use. closeFn releases whatever underlying
// resource stdin/stdout are backed by (a real subprocess's pipes plus
// cmd.Wait, or a test's io.Pipe ends); it may be nil.
func newRemoteAdapter(stdin io.WriteCloser, stdout io.Reader, closeFn func() error) *RemoteAdapter {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	return &RemoteAdapter{stdin: stdin, scanner: scanner, closeFn: closeFn}
}

// NewRemoteAdapter starts cmd (its Path/Args/Dir/Env must already be fully
// configured by the caller — RemoteAdapter never modifies them, see this
// package's sandbox test for why the caller, not this constructor, owns
// keeping cmd's environment minimal) wired to a fresh stdin/stdout pipe
// pair, and performs the "handshake" call to fetch the remote adapter's own
// plugin.PluginManifest.
//
// A malformed handshake response, a contract-shaped-but-wrong-typed
// response, or the subprocess exiting/crashing before answering all produce
// a plain, clearly worded Go error here — never a panic and never a process
// that is left half-started: on any failure this function kills/closes what
// it already started before returning. This is the concrete mechanism
// behind issue #29's "a contract violation produces a clear diagnostic, not
// a crash" acceptance criterion for the loading path; Registry.Register's
// own pre-existing CompatibleContractVersion check (internal/plugin/
// manifest.go) is what then rejects a handshake that succeeded but declared
// an incompatible ContractVersion — this constructor does not duplicate
// that check, it only makes the manifest available for it.
func NewRemoteAdapter(cmd *exec.Cmd) (*RemoteAdapter, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("transport: NewRemoteAdapter: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("transport: NewRemoteAdapter: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("transport: NewRemoteAdapter: starting subprocess: %w", err)
	}

	ra := newRemoteAdapter(stdin, stdout, func() error {
		_ = stdin.Close()
		return cmd.Wait()
	})

	manifest, err := ra.Handshake(context.Background())
	if err != nil {
		_ = ra.Close()
		return nil, fmt.Errorf("transport: NewRemoteAdapter: %w", err)
	}
	ra.manifest = manifest
	return ra, nil
}

// Handshake calls the wire protocol's "handshake" method and returns the
// remote adapter's self-declared plugin.PluginManifest. NewRemoteAdapter
// calls this once at construction and caches the result (Manifest, ID); it
// is exported mainly so a caller (or a test) can re-probe a live
// RemoteAdapter's manifest, or drive the handshake step directly against a
// scripted fake stream without going through the real-subprocess
// constructor.
func (r *RemoteAdapter) Handshake(ctx context.Context) (plugin.PluginManifest, error) {
	var manifest plugin.PluginManifest
	if err := r.call(ctx, methodHandshake, struct{}{}, &manifest); err != nil {
		return plugin.PluginManifest{}, err
	}
	return manifest, nil
}

// Manifest returns the plugin.PluginManifest this RemoteAdapter learned at
// construction (or the last successful Handshake call) — the piece a loader
// needs to call plugin.Registry.Register(remote.Manifest(), remote) without
// ever having the external binary's own source compiled into the core.
func (r *RemoteAdapter) Manifest() plugin.PluginManifest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.manifest
}

// Close releases this RemoteAdapter's underlying subprocess/stream
// resources (closing stdin unblocks a well-behaved subprocess's own read
// loop, matching internal/mcp.Serve's EOF-on-stdin-close shutdown
// convention; for the real-subprocess constructor this also waits for the
// process to exit).
func (r *RemoteAdapter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closeFn == nil {
		return nil
	}
	return r.closeFn()
}

// ID returns the AdapterID learned at handshake time, matching
// plugin.HostAdapter.ID()'s own "bare accessor, no context, no error" shape
// — answered from the cached manifest, never a fresh round trip over the
// wire (a bare accessor that could block on subprocess I/O or fail would not
// honor that shape at all).
func (r *RemoteAdapter) ID() plugin.AdapterID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.manifest.AdapterID
}

// Detect implements plugin.HostAdapter.Detect over the wire.
func (r *RemoteAdapter) Detect(ctx context.Context, req plugin.DetectRequest) ([]plugin.HostInstance, error) {
	var result detectResult
	if err := r.call(ctx, methodDetect, req, &result); err != nil {
		return nil, err
	}
	return result.Instances, nil
}

// Capabilities implements plugin.HostAdapter.Capabilities over the wire.
func (r *RemoteAdapter) Capabilities(ctx context.Context, host plugin.HostInstance) (plugin.CapabilityManifest, error) {
	var result plugin.CapabilityManifest
	if err := r.call(ctx, methodCapabilities, host, &result); err != nil {
		return plugin.CapabilityManifest{}, err
	}
	return result, nil
}

// Observe implements plugin.HostAdapter.Observe over the wire.
func (r *RemoteAdapter) Observe(ctx context.Context, req plugin.ObserveRequest) (plugin.ObservationSet, error) {
	var result plugin.ObservationSet
	if err := r.call(ctx, methodObserve, req, &result); err != nil {
		return plugin.ObservationSet{}, err
	}
	return result, nil
}

// Resolve implements plugin.HostAdapter.Resolve over the wire.
func (r *RemoteAdapter) Resolve(ctx context.Context, req plugin.ResolveRequest) (plugin.HostEffectiveState, error) {
	var result plugin.HostEffectiveState
	if err := r.call(ctx, methodResolve, req, &result); err != nil {
		return plugin.HostEffectiveState{}, err
	}
	return result, nil
}

// Compile implements plugin.HostAdapter.Compile over the wire.
func (r *RemoteAdapter) Compile(ctx context.Context, req plugin.CompileRequest) (plugin.ArtifactSet, error) {
	var result plugin.ArtifactSet
	if err := r.call(ctx, methodCompile, req, &result); err != nil {
		return plugin.ArtifactSet{}, err
	}
	return result, nil
}

// Verify implements plugin.HostAdapter.Verify over the wire.
func (r *RemoteAdapter) Verify(ctx context.Context, req plugin.VerifyRequest) (plugin.EvidenceSet, error) {
	var result plugin.EvidenceSet
	if err := r.call(ctx, methodVerify, req, &result); err != nil {
		return plugin.EvidenceSet{}, err
	}
	return result, nil
}

// Launch implements plugin.HostAdapter.Launch over the wire. Launch's own
// interface signature returns only an error, so call is given a nil result
// pointer: no response body is ever decoded, matching the wire's own
// `struct{}{}` result for this method (server.go).
func (r *RemoteAdapter) Launch(ctx context.Context, req plugin.LaunchRequest) error {
	return r.call(ctx, methodLaunch, req, nil)
}

// call performs one full request/response round trip: marshal params,
// write one JSON-RPC request line, read and decode exactly one JSON-RPC
// response line, and either decode its result into result (if non-nil) or
// translate its error into a plain Go error. Every failure mode — a
// marshal/write error, the subprocess closing its stdout (crashed or exited)
// before answering, a response line that is not valid JSON, a response
// whose "id" does not match this call's request, or a response missing both
// "result" and "error" — returns a clearly worded error naming what went
// wrong; none of them panics. The recover below is deliberate, cheap
// insurance on top of that (mirroring internal/mcp/server.go's own
// "should never actually happen, but..." belt-and-suspenders comments): it
// guarantees this method can never bring down the calling omca process even
// if some future change to this function introduces a genuine bug, which is
// the actual, literal wording of issue #29's second acceptance criterion.
func (r *RemoteAdapter) call(ctx context.Context, method string, params, result any) (callErr error) {
	defer func() {
		if p := recover(); p != nil {
			callErr = fmt.Errorf("transport: %s: recovered from an internal panic: %v", method, p)
		}
	}()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("transport: %s: %w", method, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	id := r.nextID

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("transport: %s: marshaling params: %w", method, err)
	}
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(strconv.FormatInt(id, 10)),
		Method:  method,
		Params:  paramsJSON,
	}
	line, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("transport: %s: marshaling request: %w", method, err)
	}
	line = append(line, '\n')
	if _, err := r.stdin.Write(line); err != nil {
		return fmt.Errorf("transport: %s: writing request (subprocess may have exited): %w", method, err)
	}

	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return fmt.Errorf("transport: %s: reading response: %w", method, err)
		}
		return fmt.Errorf("transport: %s: subprocess closed its output before responding (crashed, exited, or wrote no response)", method)
	}
	respLine := r.scanner.Bytes()

	var resp jsonrpcResponse
	if err := json.Unmarshal(respLine, &resp); err != nil {
		return fmt.Errorf("transport: %s: malformed response (not valid JSON-RPC): %w", method, err)
	}
	if resp.JSONRPC != "2.0" {
		return fmt.Errorf("transport: %s: malformed response: jsonrpc field = %q, want \"2.0\"", method, resp.JSONRPC)
	}
	wantID := strconv.FormatInt(id, 10)
	if !bytes.Equal(bytes.TrimSpace(resp.ID), []byte(wantID)) {
		return fmt.Errorf("transport: %s: response id %s does not match request id %s", method, string(resp.ID), wantID)
	}
	if resp.Error != nil {
		return errorFromRPC(method, resp.Error)
	}
	if result == nil {
		return nil
	}
	if len(resp.Result) == 0 {
		return fmt.Errorf("transport: %s: malformed response: neither result nor error is present", method)
	}
	if err := json.Unmarshal(resp.Result, result); err != nil {
		return fmt.Errorf("transport: %s: malformed response: could not decode result into the expected shape: %w", method, err)
	}
	return nil
}

// errorFromRPC is classifyErr's (server.go) inverse: it reconstructs a
// plain Go error from a wire jsonrpcError, wrapping the matching
// internal/plugin sentinel (via fmt.Errorf's %w) whenever the server side
// classified the original adapter error as one of the three contract-
// taxonomy outcomes, so errors.Is(err, plugin.ErrNotDetected) (etc.) still
// holds true on the client side of the process boundary. Any other code —
// including an adapter's own non-taxonomy error and any JSON-RPC
// protocol-level code this client did not expect — becomes a plain,
// clearly worded error naming the numeric code, never a silently
// misclassified sentinel.
func errorFromRPC(method string, e *jsonrpcError) error {
	switch e.Code {
	case codeNotDetected:
		return fmt.Errorf("transport: %s: %s: %w", method, e.Message, plugin.ErrNotDetected)
	case codeUnsupportedOperation:
		return fmt.Errorf("transport: %s: %s: %w", method, e.Message, plugin.ErrUnsupportedOperation)
	case codeCapabilityDenied:
		return fmt.Errorf("transport: %s: %s: %w", method, e.Message, plugin.ErrCapabilityDenied)
	default:
		return fmt.Errorf("transport: %s: remote error (code %d): %s", method, e.Code, e.Message)
	}
}
