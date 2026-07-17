package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// TestRunEnv_RegistersOMCAMCPServer_InGeneratedConfig is issue #15's FR-7
// wiring check end to end through the CLI layer this project's own quality
// bar expects (env_test.go's own precedent: exercise runEnv, then inspect
// the real on-disk generation it produced): `omca env`'s compiled codex
// generation carries an `[mcp_servers.omca]` registration whose command is
// the worktree's own stable shimDir/omca entry (not a snapshot of the
// currently-running omca test binary's own resolved path — see
// internal/runtime/request.go's OMCABinaryPath doc comment for why), and
// that entry actually exists as a working symlink to the running binary —
// proving cmd/omca/env.go's shimEntryNames/omcaCommandPath wiring produces
// something a host could really launch, not just a plausible-looking path.
func TestRunEnv_RegistersOMCAMCPServer_InGeneratedConfig(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	var stdout, stderr bytes.Buffer
	if code := runEnv(&stdout, &stderr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, stderr.String())
	}

	wt, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)
	genDir, err := runtime.CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	configPath := filepath.Join(genDir, "hosts", "codex", "cli", "codex-home", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading generated config.toml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[mcp_servers.omca]") {
		t.Fatalf("generated config.toml has no [mcp_servers.omca] registration:\n%s", content)
	}

	wantCommand := omcaCommandPath(shimDir)
	if !strings.Contains(content, `command = "`+wantCommand+`"`) {
		t.Errorf("generated config.toml does not register the worktree's stable omca shim path %q:\n%s", wantCommand, content)
	}
	if info, statErr := os.Stat(wantCommand); statErr != nil {
		t.Errorf("the registered command %q does not resolve to an existing, executable file: %v", wantCommand, statErr)
	} else if info.IsDir() {
		t.Errorf("the registered command %q is a directory, not an executable file", wantCommand)
	}
	if !strings.Contains(content, `args = ["mcp", "serve"]`) {
		t.Errorf("generated config.toml does not register the `mcp serve` argv:\n%s", content)
	}
}

// TestRunEnv_ThenRunMCPServe_EndToEnd is this PR's fullest integration
// proof: `omca env` compiles and records a real generation for a worktree,
// then (simulating what a managed host's spawned `omca mcp serve`
// subprocess would see: OMCA_WORKTREE_ID/OMCA_STATE_DIR inherited in its
// environment, exactly as env.go's own exports document) `omca mcp serve`
// answers a tools/call for omca_status with that generation's real
// generation ID and exclusion counts — proving the whole pipeline this PR
// built (Bootstrap -> MCP registration -> omca_status) is actually
// connected end to end, not just unit-correct in isolation.
func TestRunEnv_ThenRunMCPServe_EndToEnd(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	// Plant one native user-global MCP server and one Skill so the
	// generation's exclusion counts are non-trivial.
	if err := os.MkdirAll(filepath.Join(env.HomeDir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.HomeDir, ".codex", "config.toml"), []byte("[mcp_servers.native]\ncommand = \"npx\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(env.HomeDir, ".codex", "skills", "native-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.HomeDir, ".codex", "skills", "native-skill", "SKILL.md"), []byte("---\nname: native-skill\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var envOut, envErr bytes.Buffer
	if code := runEnv(&envOut, &envErr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, envErr.String())
	}
	exports := parseExports(t, envOut.String())

	// Simulate the environment a managed host's spawned `omca mcp serve`
	// subprocess would actually see: the exported OMCA_WORKTREE_ID/
	// OMCA_STATE_DIR/OMCA_CONTEXT_ID values, inherited exactly as a real
	// launch would pass them through.
	t.Setenv("OMCA_WORKTREE_ID", exports["OMCA_WORKTREE_ID"])
	t.Setenv("OMCA_CONTEXT_ID", exports["OMCA_CONTEXT_ID"])
	t.Setenv("OMCA_STATE_DIR", exports["OMCA_STATE_DIR"])

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"omca_status","arguments":{}}}` + "\n"
	var mcpOut, mcpErr bytes.Buffer
	if code := runMCP(strings.NewReader(input), &mcpOut, &mcpErr, []string{"serve"}); code != 0 {
		t.Fatalf("runMCP = %d; stderr:\n%s", code, mcpErr.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(mcpOut.Bytes()), &resp); err != nil {
		t.Fatalf("decoding omca_status response: %v\noutput: %s", err, mcpOut.String())
	}
	result := resp["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["worktreeId"] != exports["OMCA_WORKTREE_ID"] {
		t.Errorf("worktreeId = %v, want %q", structured["worktreeId"], exports["OMCA_WORKTREE_ID"])
	}
	hosts, ok := structured["hosts"].([]any)
	if !ok || len(hosts) == 0 {
		t.Fatalf("structuredContent.hosts = %v, want at least one entry", structured["hosts"])
	}
	var codexStatus map[string]any
	for _, h := range hosts {
		hm := h.(map[string]any)
		if hm["host"] == "codex" {
			codexStatus = hm
		}
	}
	if codexStatus == nil {
		t.Fatalf("no codex entry in hosts: %v", hosts)
	}
	if codexStatus["managed"] != true {
		t.Fatalf("codex managed = %v, want true", codexStatus["managed"])
	}
	if codexStatus["excludedMcpServers"] != float64(1) {
		t.Errorf("codex excludedMcpServers = %v, want 1", codexStatus["excludedMcpServers"])
	}
	if codexStatus["excludedSkills"] != float64(1) {
		t.Errorf("codex excludedSkills = %v, want 1", codexStatus["excludedSkills"])
	}
	contextCost, ok := codexStatus["contextCost"].(map[string]any)
	if !ok {
		t.Fatalf("codex contextCost missing or not an object: %v", codexStatus)
	}
	if contextCost["confidence"] == "" {
		t.Error("contextCost.confidence is empty")
	}
}

// TestRunEnv_PrintsContextCostSummary_OnStderr is issue #15's own
// instruction to surface the exclusion counts/context-cost estimate
// "wherever omca doctor/omca env/a report-producing command already prints
// diagnostic output" — not only through the omca_status MCP tool. Plants
// one native MCP server and one native Skill (like
// TestRunEnv_ThenRunMCPServe_EndToEnd) and asserts `omca env`'s stderr
// names the same counts.
func TestRunEnv_PrintsContextCostSummary_OnStderr(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	if err := os.MkdirAll(filepath.Join(env.HomeDir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.HomeDir, ".codex", "config.toml"), []byte("[mcp_servers.native]\ncommand = \"npx\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(env.HomeDir, ".codex", "skills", "native-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.HomeDir, ".codex", "skills", "native-skill", "SKILL.md"), []byte("---\nname: native-skill\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runEnv(&stdout, &stderr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, stderr.String())
	}
	out := stderr.String()
	if !strings.Contains(out, "excluded 1 native MCP configuration source(s), 1 native Skill(s)") {
		t.Errorf("stderr does not contain the expected context-cost summary line:\n%s", out)
	}
	if !strings.Contains(out, "estimated context-cost delta") {
		t.Errorf("stderr does not mention the estimated context-cost delta:\n%s", out)
	}
}

// TestRunDoctor_ReportsContextCost proves `omca doctor` carries the same
// "context-cost:<host>" finding, always statusOK (a report, never itself a
// pass/fail condition), reusing the identical internal/mcp computation
// `omca env`'s stderr line and omca_status both use.
func TestRunDoctor_ReportsContextCost(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	if err := os.MkdirAll(filepath.Join(env.HomeDir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.HomeDir, ".codex", "config.toml"), []byte("[mcp_servers.native]\ncommand = \"npx\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var envOut, envErr bytes.Buffer
	if code := runEnv(&envOut, &envErr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, envErr.String())
	}

	var stdout, stderr bytes.Buffer
	_ = runDoctor(&stdout, &stderr) // exit code is irrelevant here — this fixture's PATH bypass FAIL (no shim on PATH) is covered elsewhere
	out := stdout.String()
	if !strings.Contains(out, "[OK  ] context-cost:codex:") {
		t.Errorf("stdout does not contain an OK context-cost:codex finding:\n%s", out)
	}
	if !strings.Contains(out, "excluded 1 native MCP configuration source(s)") {
		t.Errorf("stdout does not report the excluded native MCP source count:\n%s", out)
	}
}

// parseExports parses `omca env`'s own stdout (export KEY='VALUE' lines)
// into a map, the same minimal shell-export parse doctor_test.go's
// applyExportsToEnv performs, generalized here to return every exported
// value rather than applying a fixed subset via t.Setenv immediately.
func parseExports(t *testing.T, exports string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(exports, "\n") {
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := line[:eq]
		val := strings.Trim(line[eq+1:], "'")
		out[key] = val
	}
	return out
}
