package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/conformance"
)

// scriptedServer is a fake adapter subprocess: it reads one JSON-RPC request
// line at a time from its own stdin-side pipe and answers with whatever
// respond returns, verbatim — including deliberately malformed, truncated,
// or wrong-shaped bytes no real conformant adapter would ever emit. This is
// exactly the "fake subprocess or a piped fake stream, not necessarily a
// real adapter binary" issue #29 calls for to prove RemoteAdapter's own
// clean-error-not-panic property, independent of any real external binary.
type scriptedServer struct {
	client *RemoteAdapter
	done   chan struct{}
}

// respondFunc decides what raw response line(s) (each already a complete,
// newline-free JSON-RPC message) to write back for one received request
// line, and whether to close the connection afterward instead of waiting
// for a further request.
type respondFunc func(reqLine []byte) (lines []string, closeAfter bool)

func newScriptedServer(t *testing.T, respond respondFunc) *scriptedServer {
	t.Helper()
	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(reqR)
		scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
		for scanner.Scan() {
			lines, closeAfter := respond(append([]byte(nil), scanner.Bytes()...))
			for _, l := range lines {
				if _, err := io.WriteString(respW, l+"\n"); err != nil {
					_ = respW.Close()
					return
				}
			}
			if closeAfter {
				_ = respW.Close()
				return
			}
		}
		_ = respW.Close()
	}()

	client := newRemoteAdapter(reqW, respR, func() error {
		_ = reqW.Close()
		_ = respR.Close()
		return nil
	})
	return &scriptedServer{client: client, done: done}
}

func (s *scriptedServer) Close() {
	_ = s.client.Close()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
}

// echoID extracts the "id" field from a raw request line so a scripted
// response can echo it back correctly (RemoteAdapter.call rejects a
// mismatched id) except in the tests that deliberately want to prove a
// mismatched-id response is itself caught.
func echoID(t *testing.T, reqLine []byte) string {
	t.Helper()
	var req struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(reqLine, &req); err != nil {
		t.Fatalf("test bug: could not parse the request line to echo its id: %v", err)
	}
	return string(req.ID)
}

func TestRemoteAdapter_MalformedJSONResponse_CleanErrorNoPanic(t *testing.T) {
	srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
		return []string{`{{{ not json`}, true
	})
	defer srv.Close()

	_, err := srv.client.Detect(context.Background(), plugin.DetectRequest{})
	if err == nil {
		t.Fatal("Detect against a malformed JSON response: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "malformed response") {
		t.Errorf("error = %q, want it to name the response as malformed", err)
	}
}

func TestRemoteAdapter_TruncatedResponse_CleanErrorNoPanic(t *testing.T) {
	srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
		// Never writes a newline-terminated response at all -- closing
		// immediately stands in for a subprocess that crashed mid-response
		// or was killed before it could finish writing.
		return nil, true
	})
	defer srv.Close()

	_, err := srv.client.Detect(context.Background(), plugin.DetectRequest{})
	if err == nil {
		t.Fatal("Detect against a subprocess that closed without responding: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "closed its output") {
		t.Errorf("error = %q, want it to describe the subprocess closing without responding", err)
	}
}

func TestRemoteAdapter_MismatchedResponseID_CleanErrorNoPanic(t *testing.T) {
	srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
		return []string{`{"jsonrpc":"2.0","id":999999,"result":{"instances":[]}}`}, false
	})
	defer srv.Close()

	_, err := srv.client.Detect(context.Background(), plugin.DetectRequest{})
	if err == nil {
		t.Fatal("Detect against a response with a mismatched id: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "does not match request id") {
		t.Errorf("error = %q, want it to describe the id mismatch", err)
	}
}

func TestRemoteAdapter_ResponseMissingResultAndError_CleanErrorNoPanic(t *testing.T) {
	srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
		id := echoID(t, reqLine)
		return []string{`{"jsonrpc":"2.0","id":` + id + `}`}, false
	})
	defer srv.Close()

	_, err := srv.client.Detect(context.Background(), plugin.DetectRequest{})
	if err == nil {
		t.Fatal("Detect against a response with neither result nor error: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "neither result nor error") {
		t.Errorf("error = %q, want it to describe the missing result/error", err)
	}
}

func TestRemoteAdapter_WrongShapedResult_CleanErrorNoPanic(t *testing.T) {
	srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
		id := echoID(t, reqLine)
		// "detect"'s result must decode into detectResult{Instances: [...]}
		// -- a bare JSON string is a valid JSON value but the wrong shape.
		return []string{`{"jsonrpc":"2.0","id":` + id + `,"result":"totally-the-wrong-shape"}`}, false
	})
	defer srv.Close()

	_, err := srv.client.Detect(context.Background(), plugin.DetectRequest{})
	if err == nil {
		t.Fatal("Detect against a wrong-shaped result: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "could not decode result") {
		t.Errorf("error = %q, want it to describe the decode failure", err)
	}
}

func TestRemoteAdapter_UnexpectedJSONRPCVersion_CleanErrorNoPanic(t *testing.T) {
	srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
		id := echoID(t, reqLine)
		return []string{`{"jsonrpc":"1.0","id":` + id + `,"result":{}}`}, false
	})
	defer srv.Close()

	_, err := srv.client.Detect(context.Background(), plugin.DetectRequest{})
	if err == nil {
		t.Fatal("Detect against a response with the wrong jsonrpc version: want an error, got nil")
	}
	if !strings.Contains(err.Error(), `want "2.0"`) {
		t.Errorf("error = %q, want it to name the version mismatch", err)
	}
}

// TestRemoteAdapter_ErrorTaxonomy_SurvivesTheProcessBoundary is the decode
// half of the encode/decode pair (classifyErr/errorFromRPC) that keeps
// errors.Is(err, plugin.ErrNotDetected) true for a caller on the client side
// of a real process boundary, exercised here through a scripted server that
// emits exactly what Serve's own classifyErr would (server_test.go proves
// the encode half).
func TestRemoteAdapter_ErrorTaxonomy_SurvivesTheProcessBoundary(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		wantErr error
	}{
		{"not detected", codeNotDetected, plugin.ErrNotDetected},
		{"unsupported operation", codeUnsupportedOperation, plugin.ErrUnsupportedOperation},
		{"capability denied", codeCapabilityDenied, plugin.ErrCapabilityDenied},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
				id := echoID(t, reqLine)
				line, err := json.Marshal(jsonrpcResponse{
					JSONRPC: "2.0",
					ID:      json.RawMessage(id),
					Error:   &jsonrpcError{Code: c.code, Message: "synthetic taxonomy error"},
				})
				if err != nil {
					t.Fatalf("marshaling scripted response: %v", err)
				}
				return []string{string(line)}, false
			})
			defer srv.Close()

			_, err := srv.client.Detect(context.Background(), plugin.DetectRequest{})
			if !errors.Is(err, c.wantErr) {
				t.Errorf("Detect() error = %v, want it to wrap %v", err, c.wantErr)
			}
		})
	}
}

// TestRemoteAdapter_AlreadyCanceledContext proves call bails out before ever
// writing to the subprocess when the caller's context is already done.
func TestRemoteAdapter_AlreadyCanceledContext(t *testing.T) {
	srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
		t.Fatal("the scripted server received a request; the call should have been rejected before ever writing one")
		return nil, true
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := srv.client.Detect(ctx, plugin.DetectRequest{})
	if err == nil {
		t.Fatal("Detect with an already-canceled context: want an error, got nil")
	}
}

// TestRemoteAdapter_HungSubprocess_ContextDeadlineInterruptsRead is a
// regression test (Copilot review finding on this PR): before this fix,
// RemoteAdapter.call only checked ctx before writing the request -- once the
// blocking scanner.Scan() read started, a subprocess that accepted the
// request but never wrote a response (and never closed its stdout, e.g.
// hung or deadlocked rather than crashed) would block the call forever
// regardless of ctx's deadline or cancellation. The scripted server here
// deliberately does exactly that: it reads the request and returns no
// response lines at all, simulating a hung (not crashed) external adapter.
func TestRemoteAdapter_HungSubprocess_ContextDeadlineInterruptsRead(t *testing.T) {
	srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
		return nil, false // accept the request, never respond, never close
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := srv.client.Detect(ctx, plugin.DetectRequest{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Detect against a hung subprocess: want an error once ctx's deadline passes, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Detect against a hung subprocess: error = %v, want it to wrap context.DeadlineExceeded", err)
	}
	// Generous upper bound (well above the 200ms deadline) that would still
	// catch "this blocked forever" -- the actual bug this test guards
	// against -- without being a flaky tight timing assertion.
	if elapsed > 5*time.Second {
		t.Errorf("Detect against a hung subprocess took %s to return after a 200ms context deadline -- the read did not respect ctx cancellation", elapsed)
	}
}

// TestRemoteAdapter_ManyMalformedPayloads_NeverPanics is a small table of
// deliberately hostile response bytes run through every typed HostAdapter
// method at least once, asserting only that the call returns cleanly (no
// panic reaches the testing framework, which would fail the test process
// outright rather than reporting a normal failure).
func TestRemoteAdapter_ManyMalformedPayloads_NeverPanics(t *testing.T) {
	malformed := []string{
		``,
		`   `,
		`null`,
		`42`,
		`"just a string"`,
		`[1,2,3]`,
		`{"jsonrpc":"2.0"}`,
		`{"jsonrpc":"2.0","id":null}`,
		`{"jsonrpc":"2.0","id":9999,"result":{"unexpected":{"deeply":{"nested":true}}}}`,
	}

	for _, payload := range malformed {
		payload := payload
		t.Run(payload, func(t *testing.T) {
			srv := newScriptedServer(t, func(reqLine []byte) ([]string, bool) {
				return []string{payload}, true
			})
			defer srv.Close()

			ctx := context.Background()
			call := func() error { _, err := srv.client.Detect(ctx, plugin.DetectRequest{}); return err }

			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Fatalf("call panicked on malformed payload %q: %v", payload, p)
					}
				}()
				if err := call(); err == nil {
					t.Errorf("payload %q: want a non-nil error, got nil", payload)
				}
			}()
		})
	}
}

// TestRemoteAdapter_RoundTrip_InProcess wires Serve and RemoteAdapter
// directly to each other over an in-memory pipe (no real subprocess) as a
// fast sanity check that the two halves actually agree on the wire shape,
// independent of conformance_test.go's real-subprocess proof.
func TestRemoteAdapter_RoundTrip_InProcess(t *testing.T) {
	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()

	adapter := conformance.NewFakeAdapter()
	manifest := testManifest()
	serveErr := make(chan error, 1)
	go func() { serveErr <- Serve(reqR, respW, manifest, adapter) }()

	client := newRemoteAdapter(reqW, respR, func() error {
		_ = reqW.Close()
		return nil
	})

	got, err := client.Handshake(context.Background())
	if err != nil {
		t.Fatalf("Handshake: unexpected error: %v", err)
	}
	if got.AdapterID != manifest.AdapterID {
		t.Errorf("Handshake().AdapterID = %q, want %q", got.AdapterID, manifest.AdapterID)
	}

	instances, err := client.Detect(context.Background(), plugin.DetectRequest{})
	if err != nil {
		t.Fatalf("Detect: unexpected error: %v", err)
	}
	if len(instances) != 1 || instances[0].HostID != "codex" {
		t.Errorf("Detect() = %+v, want one codex HostInstance", instances)
	}

	if err := client.Close(); err != nil {
		t.Errorf("Close: unexpected error: %v", err)
	}
	select {
	case err := <-serveErr:
		if err != nil {
			t.Errorf("Serve returned an error after the client closed its stdin: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Serve did not return within 2s of the client closing its stdin")
	}
}

// TestRemoteAdapter_ConformanceParity_InProcess is a second, faster
// conformance-parity proof alongside conformance_test.go's real-subprocess
// one: it wires Serve and RemoteAdapter directly over an in-memory pipe
// (no compiled external binary needed) and runs the exact same
// conformance.Run suite against the resulting RemoteAdapter. Together with
// the real-subprocess version, this also exercises every one of
// handleLine's per-method branches (server.go) — including the happy paths
// for observe/resolve/compile/verify/launch that the real-subprocess test
// runs inside a separately compiled, uninstrumented binary and therefore
// never contributes to this package's own coverage numbers.
func TestRemoteAdapter_ConformanceParity_InProcess(t *testing.T) {
	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()

	adapter := conformance.NewFakeAdapter()
	manifest := testManifest()
	serveErr := make(chan error, 1)
	go func() { serveErr <- Serve(reqR, respW, manifest, adapter) }()

	client := newRemoteAdapter(reqW, respR, func() error {
		_ = reqW.Close()
		return nil
	})
	got, err := client.Handshake(context.Background())
	if err != nil {
		t.Fatalf("Handshake: unexpected error: %v", err)
	}
	client.manifest = got

	conformance.Run(t, client)

	if err := client.Close(); err != nil {
		t.Errorf("Close: unexpected error: %v", err)
	}
	select {
	case err := <-serveErr:
		if err != nil {
			t.Errorf("Serve returned an error after the client closed its stdin: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Serve did not return within 2s of the client closing its stdin")
	}
}
