package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// TestRunMCP_RejectsMissingOrWrongSubcommand proves `omca mcp` requires
// exactly the literal "serve" subcommand — issue #15's own synopsis, `omca
// mcp serve` — rather than silently doing something else for `omca mcp` or
// `omca mcp bogus`.
func TestRunMCP_RejectsMissingOrWrongSubcommand(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"bogus"}, {"serve", "extra"}} {
		var stdout, stderr bytes.Buffer
		code := runMCP(strings.NewReader(""), &stdout, &stderr, args)
		if code != 2 {
			t.Errorf("runMCP(%v) = %d, want 2", args, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("runMCP(%v) wrote to stdout, want none on a usage error: %q", args, stdout.String())
		}
	}
}

// TestRunMCP_Serve_RespondsToToolsCall_ReadingRealAmbientEnvironment proves
// `omca mcp serve` actually wires internal/mcp.Serve up to this process's
// real environment (via hostcontext.RealEnvironment(), matching how
// checkSessionManaged/checkPathBypass in doctor.go already read managed-
// session state): with OMCA_WORKTREE_ID/OMCA_STATE_DIR set via t.Setenv,
// a tools/call for omca_status over stdin returns a StatusResult naming
// that exact worktree ID, read out of the real process environment, not a
// hardcoded or passed-as-argument value.
func TestRunMCP_Serve_RespondsToToolsCall_ReadingRealAmbientEnvironment(t *testing.T) {
	t.Setenv("OMCA_WORKTREE_ID", "worktree:sha256:test-value")
	t.Setenv("OMCA_CONTEXT_ID", "")
	t.Setenv("OMCA_STATE_DIR", t.TempDir())

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"omca_status","arguments":{}}}` + "\n"
	var stdout, stderr bytes.Buffer
	code := runMCP(strings.NewReader(input), &stdout, &stderr, []string{"serve"})
	if code != 0 {
		t.Fatalf("runMCP([serve]) = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	line := strings.TrimSpace(stdout.String())
	if line == "" {
		t.Fatal("runMCP wrote nothing to stdout")
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("stdout line is not valid JSON: %v\nline: %s", err, line)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result object: %v", resp)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("result has no structuredContent object: %v", result)
	}
	if structured["worktreeId"] != "worktree:sha256:test-value" {
		t.Errorf("structuredContent.worktreeId = %v, want the value read from OMCA_WORKTREE_ID", structured["worktreeId"])
	}
}

// TestRunMCP_Serve_RespondsToOmcaQuery_ThroughTheRealBuildPipeline proves
// `omca mcp serve` wires omca_query to the real buildArtifactForCLI
// pipeline (issue #24/PR-20) — the same detect-observe-compose-Build
// sequence every `omca report`/`omca drift`/... CLI command already runs —
// rather than some second, parallel implementation: a tools/call for
// omca_query with kind=artifact over stdin succeeds and returns an
// ArtifactSummary carrying a real, non-empty worktree ID (this repository's
// own, computed by hostcontext.DetectWorktree(cwd) exactly like every other
// CLI command run from this directory) — proof the omca_query path in
// cmd/omca/mcp.go actually reaches real on-disk state, not a stub.
func TestRunMCP_Serve_RespondsToOmcaQuery_ThroughTheRealBuildPipeline(t *testing.T) {
	t.Setenv("OMCA_STATE_DIR", t.TempDir())

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"omca_query","arguments":{"kind":"artifact"}}}` + "\n"
	var stdout, stderr bytes.Buffer
	code := runMCP(strings.NewReader(input), &stdout, &stderr, []string{"serve"})
	if code != 0 {
		t.Fatalf("runMCP([serve]) = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	line := strings.TrimSpace(stdout.String())
	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("stdout line is not valid JSON: %v\nline: %s", err, line)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result object: %v", resp)
	}
	if result["isError"] == true {
		t.Fatalf("isError = true: %v", result)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("result has no structuredContent object: %v", result)
	}
	if structured["kind"] != "artifact" {
		t.Errorf("structuredContent.kind = %v, want %q", structured["kind"], "artifact")
	}
	artifact, ok := structured["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent has no artifact object: %v", structured)
	}
	worktree, _ := artifact["worktree"].(string)
	if worktree == "" {
		t.Error("artifact.worktree is empty, want the real worktree ID this test's own cwd resolves to")
	}
}

// TestSessionHostFromEnv covers issue #19's restart_required wiring: which
// host this `omca mcp serve` process is answering for is inferred from
// whichever native-home environment variable (CODEX_HOME/CLAUDE_CONFIG_DIR)
// is actually set in this process's own environment -- exactly what a real
// managed launch path (cmd/omca/run.go's runIsolated, internal/shim.Plan.Exec)
// sets before exec'ing the host binary that in turn spawns this subprocess.
func TestSessionHostFromEnv(t *testing.T) {
	cases := []struct {
		name       string
		codexHome  string
		claudeHome string
		want       string
	}{
		{name: "neither set", want: ""},
		{name: "codex only", codexHome: "/gen/codex-home", want: "codex"},
		{name: "claude only", claudeHome: "/gen/claude-config", want: "claude-code"},
		{name: "both set is ambiguous", codexHome: "/gen/codex-home", claudeHome: "/gen/claude-config", want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var vars []string
			if c.codexHome != "" {
				vars = append(vars, "CODEX_HOME="+c.codexHome)
			}
			if c.claudeHome != "" {
				vars = append(vars, "CLAUDE_CONFIG_DIR="+c.claudeHome)
			}
			env := hostcontext.Environment{Vars: vars}
			got := sessionHostFromEnv(env)
			if got != c.want {
				t.Errorf("sessionHostFromEnv() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestCompileFuncForMCP_CorruptCachedManifest_FailsClosed proves the
// Copilot-review fix: compileFuncForMCP (the CompileFunc backing omca_stage)
// previously recompiled into outputDir on ANY ReadGenerationManifest error,
// not just a genuine cache miss (os.IsNotExist) -- silently overwriting a
// content-addressed generation directory whose manifest exists but failed
// validation, the same "refuse to overwrite a broken content-addressed
// path" invariant runtime.EnsureGeneration already enforces elsewhere.
func TestCompileFuncForMCP_CorruptCachedManifest_FailsClosed(t *testing.T) {
	if testFixtureBinaries.fakeHost == "" {
		t.Skip("fixture binaries not built (TestMain)")
	}
	xdgStateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdgStateHome)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	restoreWritableTree(t, xdgStateHome) // ensure t.TempDir()'s own cleanup can remove the read-only compiled tree
	binDir := t.TempDir()
	if err := os.Symlink(testFixtureBinaries.fakeHost, filepath.Join(binDir, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	compileFn := compileFuncForMCP(&bytes.Buffer{})
	activations := map[string]domain.HostActivation{"claude-code": {}}

	gen, _, err := compileFn(activations)
	if err != nil {
		t.Fatalf("first compileFn call: %v", err)
	}

	stateRoot := filepath.Join(os.Getenv("XDG_STATE_HOME"), "omca")
	// Find the generation's own manifest.json under the state root (its
	// exact worktree-ID-derived subdirectory is not this test's concern)
	// and corrupt it in place: still present, but no longer valid JSON.
	var manifestPath string
	_ = filepath.WalkDir(stateRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() == "manifest.json" && strings.Contains(path, runtime.DirSafeID(gen.Metadata.ID)) {
			manifestPath = path
		}
		return nil
	})
	if manifestPath == "" {
		t.Fatalf("could not locate the compiled generation's manifest.json under %s", stateRoot)
	}
	// The compiled generation tree lands read-only (internal/runtime/
	// readonly.go); restore write permission (immediately, not just via
	// t.Cleanup) before tampering with it.
	restoreWritableSkippingSymlinks(stateRoot)
	if err := os.WriteFile(manifestPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("corrupting manifest: %v", err)
	}

	if _, _, err := compileFn(activations); err == nil {
		t.Fatal("second compileFn call with a corrupted cached manifest: want an error, got nil -- it silently recompiled over (or ignored) the corruption")
	} else if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("error = %q, want it to explain the refuse-to-overwrite-a-broken-content-addressed-path reasoning", err.Error())
	}
}

// TestRun_MCPServe_DispatchesThroughMain proves `run(["mcp", "serve"], ...)`
// — the same entry point main() uses — reaches runMCP rather than falling
// through to the "unknown command" default case.
func TestRun_MCPServe_DispatchesThroughMain(t *testing.T) {
	t.Setenv("OMCA_STATE_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"mcp", "bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run([mcp, bogus]) = %d, want 2 (dispatched into runMCP's own usage error, not the top-level unknown-command error)", code)
	}
	if strings.Contains(stderr.String(), usage) {
		t.Errorf("stderr contains the top-level usage string, meaning `mcp` fell through to the unknown-command branch: %q", stderr.String())
	}
}
