package transport

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/conformance"
)

func testManifest() plugin.PluginManifest {
	return plugin.PluginManifest{
		AdapterID:       "conformance-fake",
		AdapterVersion:  "0.0.0-test",
		ContractVersion: plugin.ContractVersion,
		Hosts: []plugin.HostSelector{
			{HostID: "codex", Surfaces: []string{"cli"}, VersionRange: "0.144.0"},
		},
	}
}

// decodeResponse unmarshals resp (which must be non-nil — the caller sent a
// request with an "id", so handleLine always answers) and fails the test if
// it is nil.
func decodeResponse(t *testing.T, resp *jsonrpcResponse) jsonrpcResponse {
	t.Helper()
	if resp == nil {
		t.Fatal("handleLine returned nil for a request with an id; want a response")
	}
	return *resp
}

func TestHandleLine_MalformedJSON_ReturnsParseError(t *testing.T) {
	resp := decodeResponse(t, handleLine([]byte(`not json at all {{{`), testManifest(), conformance.NewFakeAdapter()))
	if resp.Error == nil {
		t.Fatal("want a JSON-RPC error for malformed JSON, got none")
	}
	if resp.Error.Code != codeParseError {
		t.Errorf("Error.Code = %d, want %d (parse error)", resp.Error.Code, codeParseError)
	}
}

func TestHandleLine_UnknownMethod_ReturnsMethodNotFound(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":1,"method":"does-not-exist"}`
	resp := decodeResponse(t, handleLine([]byte(line), testManifest(), conformance.NewFakeAdapter()))
	if resp.Error == nil || resp.Error.Code != codeMethodNotFound {
		t.Fatalf("Error = %+v, want code %d (method not found)", resp.Error, codeMethodNotFound)
	}
}

func TestHandleLine_Notification_NoID_GetsNoResponse(t *testing.T) {
	line := `{"jsonrpc":"2.0","method":"detect","params":{}}`
	resp := handleLine([]byte(line), testManifest(), conformance.NewFakeAdapter())
	if resp != nil {
		t.Fatalf("handleLine(notification) = %+v, want nil (JSON-RPC 2.0 notifications get no response)", resp)
	}
}

func TestHandleLine_Handshake_ReturnsManifest(t *testing.T) {
	manifest := testManifest()
	line := `{"jsonrpc":"2.0","id":1,"method":"handshake","params":{}}`
	resp := decodeResponse(t, handleLine([]byte(line), manifest, conformance.NewFakeAdapter()))
	if resp.Error != nil {
		t.Fatalf("handshake: unexpected error: %+v", resp.Error)
	}
	var got plugin.PluginManifest
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("decoding handshake result: %v", err)
	}
	if got.AdapterID != manifest.AdapterID || got.ContractVersion != manifest.ContractVersion {
		t.Errorf("handshake result = %+v, want %+v", got, manifest)
	}
}

func TestHandleLine_Detect_HappyPath(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":7,"method":"detect","params":{"Invocation":{"WorktreeID":"wt","Cwd":"/wt","Trust":"trusted","GenerationID":"gen"}}}`
	resp := decodeResponse(t, handleLine([]byte(line), testManifest(), conformance.NewFakeAdapter()))
	if resp.Error != nil {
		t.Fatalf("detect: unexpected error: %+v", resp.Error)
	}
	if string(resp.ID) != "7" {
		t.Errorf("response id = %s, want 7 (echoed from the request)", resp.ID)
	}
	var result detectResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decoding detect result: %v", err)
	}
	if len(result.Instances) != 1 || result.Instances[0].HostID != "codex" {
		t.Errorf("detect result = %+v, want one HostInstance with HostID codex", result)
	}
}

func TestHandleLine_Detect_BadParams_ReturnsInvalidParams(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":1,"method":"detect","params":"not-an-object"}`
	resp := decodeResponse(t, handleLine([]byte(line), testManifest(), conformance.NewFakeAdapter()))
	if resp.Error == nil || resp.Error.Code != codeInvalidParams {
		t.Fatalf("Error = %+v, want code %d (invalid params)", resp.Error, codeInvalidParams)
	}
}

// TestHandleLine_ErrorTaxonomy_ClassifiesEachSentinel proves classifyErr maps
// every one of internal/plugin's three error-taxonomy sentinels onto this
// wire protocol's matching reserved code — the encode half of the property
// that keeps errors.Is(err, plugin.ErrNotDetected) (etc.) true across the
// process boundary (see client.go's errorFromRPC, the decode half).
func TestHandleLine_ErrorTaxonomy_ClassifiesEachSentinel(t *testing.T) {
	adapter := conformance.NewFakeAdapter()
	notDetected := `{"HostID":"never-detected"}`

	cases := []struct {
		name     string
		line     string
		wantCode int
	}{
		{
			name:     "capabilities against an undetected host -> ErrNotDetected",
			line:     `{"jsonrpc":"2.0","id":1,"method":"capabilities","params":` + notDetected + `}`,
			wantCode: codeNotDetected,
		},
		{
			name:     "resolve of the unsupported concept -> ErrUnsupportedOperation",
			line:     `{"jsonrpc":"2.0","id":1,"method":"resolve","params":{"Invocation":{},"Host":{"HostID":"codex","Surface":"cli","Version":"0.144.0","Platform":"darwin-arm64"},"Concept":"` + conformance.ConceptUnsupported + `"}}`,
			wantCode: codeUnsupportedOperation,
		},
		{
			name:     "compile of the blocked concept -> ErrCapabilityDenied",
			line:     `{"jsonrpc":"2.0","id":1,"method":"compile","params":{"Invocation":{},"Host":{"HostID":"codex","Surface":"cli","Version":"0.144.0","Platform":"darwin-arm64"},"Concept":"` + conformance.ConceptBlocked + `"}}`,
			wantCode: codeCapabilityDenied,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := decodeResponse(t, handleLine([]byte(c.line), testManifest(), adapter))
			if resp.Error == nil {
				t.Fatalf("want a JSON-RPC error, got a success result")
			}
			if resp.Error.Code != c.wantCode {
				t.Errorf("Error.Code = %d, want %d\nmessage: %s", resp.Error.Code, c.wantCode, resp.Error.Message)
			}
		})
	}
}

func TestServe_NilAdapter_ReturnsError(t *testing.T) {
	if err := Serve(strings.NewReader(""), new(strings.Builder), testManifest(), nil); err == nil {
		t.Fatal("Serve with a nil adapter: want an error, got nil")
	}
}
