package runtime

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestBootstrap_OMCABinaryPathEmpty_NoMCPRegistration is the backward-
// compatibility half of issue #15's MCP-registration wiring: every caller
// that leaves BootstrapRequest.OMCABinaryPath unset (every test in this
// package predating this PR) must keep getting a generation with no MCP
// registration at all -- PR-09's original, documented behavior.
func TestBootstrap_OMCABinaryPathEmpty_NoMCPRegistration(t *testing.T) {
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req := BootstrapRequest{
		Detection:    tr.detection("0.144.5"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		// OMCABinaryPath intentionally left empty.
	}
	outputDir := filepath.Join(t.TempDir(), "generation")
	if _, err := Bootstrap(req, outputDir); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, outputDir)

	tree := walkGeneratedTree(t, outputDir)
	configTOML := string(tree[filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")])
	if strings.Contains(configTOML, "mcp_servers.omca") {
		t.Errorf("config.toml contains an MCP registration despite an empty OMCABinaryPath:\n%s", configTOML)
	}
	if _, ok := tree[filepath.Join("hosts", "codex", "cli", "codex-home", ".claude.json")]; ok {
		t.Error("a .claude.json was generated for a codex generation, which never uses that filename")
	}
}

// TestBootstrap_Codex_OMCABinaryPath_RegistersMCPServer proves issue #15's
// core MCP-wiring requirement for Codex: when OMCABinaryPath is supplied,
// the generated config.toml gains an `[mcp_servers.omca]` table naming that
// exact path and the fixed `omca mcp serve` argv, alongside (not instead
// of) the existing conservative permission defaults.
func TestBootstrap_Codex_OMCABinaryPath_RegistersMCPServer(t *testing.T) {
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	const omcaBinaryPath = "/opt/homebrew/bin/omca"
	req := BootstrapRequest{
		Detection:      tr.detection("0.144.5"),
		Worktree:       tr.worktree(t),
		Observations:   obs,
		Now:            time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		OMCABinaryPath: omcaBinaryPath,
	}
	outputDir := filepath.Join(t.TempDir(), "generation")
	if _, err := Bootstrap(req, outputDir); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, outputDir)

	tree := walkGeneratedTree(t, outputDir)
	configTOML := string(tree[filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")])
	if !strings.Contains(configTOML, "[mcp_servers.omca]") {
		t.Fatalf("config.toml does not contain [mcp_servers.omca]:\n%s", configTOML)
	}
	if !strings.Contains(configTOML, `command = "/opt/homebrew/bin/omca"`) {
		t.Errorf("config.toml does not name the supplied OMCA binary path as command:\n%s", configTOML)
	}
	if !strings.Contains(configTOML, `args = ["mcp", "serve"]`) {
		t.Errorf("config.toml does not carry the fixed `mcp serve` argv:\n%s", configTOML)
	}
	// The conservative permission defaults must still be present -- the MCP
	// registration is additive, not a replacement.
	if !strings.Contains(configTOML, "approval_policy") {
		t.Errorf("config.toml lost its conservative permission defaults:\n%s", configTOML)
	}
}

// TestBootstrap_ClaudeCode_OMCABinaryPath_RegistersMCPServer is the Claude
// Code half: the generated .claude.json (a separate file from
// settings.json, matching claudeUserRules' own real physical split) carries
// a valid `mcpServers.omca` JSON entry with the supplied command/args.
func TestBootstrap_ClaudeCode_OMCABinaryPath_RegistersMCPServer(t *testing.T) {
	tr := newClaudeFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "CLAUDE.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	const omcaBinaryPath = "/opt/homebrew/bin/omca"
	req := BootstrapRequest{
		Detection:      tr.detection("2.1.211"),
		Worktree:       tr.worktree(t),
		Observations:   obs,
		Now:            time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		OMCABinaryPath: omcaBinaryPath,
	}
	outputDir := filepath.Join(t.TempDir(), "generation")
	if _, err := Bootstrap(req, outputDir); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, outputDir)

	tree := walkGeneratedTree(t, outputDir)
	claudeJSONPath := filepath.Join("hosts", "claude-code", "cli", "claude-config", ".claude.json")
	raw, ok := tree[claudeJSONPath]
	if !ok {
		t.Fatalf("no .claude.json was generated at %s; got %v", claudeJSONPath, keysOf(tree))
	}
	var doc struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("generated .claude.json does not parse as JSON: %v\ncontent: %s", err, raw)
	}
	entry, ok := doc.MCPServers["omca"]
	if !ok {
		t.Fatalf("mcpServers has no \"omca\" entry: %+v", doc.MCPServers)
	}
	if entry.Command != omcaBinaryPath {
		t.Errorf("mcpServers.omca.command = %q, want %q", entry.Command, omcaBinaryPath)
	}
	if len(entry.Args) != 2 || entry.Args[0] != "mcp" || entry.Args[1] != "serve" {
		t.Errorf("mcpServers.omca.args = %v, want [mcp serve]", entry.Args)
	}

	// settings.json (the permission defaults file) must still exist and be
	// unaffected -- the MCP registration lands in a separate file.
	settings := string(tree[filepath.Join("hosts", "claude-code", "cli", "claude-config", "settings.json")])
	if !strings.Contains(settings, "defaultMode") {
		t.Errorf("settings.json lost its conservative permission defaults:\n%s", settings)
	}
}

// TestBootstrapRequest_Validate_RejectsRelativeOMCABinaryPath proves the
// "fail closed on a relative path" discipline Bootstrap's own outputDir
// parameter already has extends to OMCABinaryPath: a relative value here
// would silently resolve against whatever directory Codex/Claude Code
// itself happens to be running from when it tries to launch the registered
// MCP server, not this compiler's notion of "absolute" -- a caller
// composition bug this package must reject rather than compile into a
// generation that will fail unpredictably at MCP-launch time.
func TestBootstrapRequest_Validate_RejectsRelativeOMCABinaryPath(t *testing.T) {
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req := BootstrapRequest{
		Detection:      tr.detection("0.144.5"),
		Worktree:       tr.worktree(t),
		Observations:   obs,
		Now:            time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		OMCABinaryPath: "relative/omca",
	}
	if err := req.validate(); err == nil {
		t.Fatal("validate() accepted a relative OMCABinaryPath; want an error")
	}
	if _, err := Bootstrap(req, filepath.Join(t.TempDir(), "generation")); err == nil {
		t.Fatal("Bootstrap with a relative OMCABinaryPath: want error, got nil")
	}
}

// TestGenerationID_StableAcrossOMCABinaryPathChange proves OMCABinaryPath
// deliberately does NOT participate in the content-addressed ID (request.go's
// OMCABinaryPath doc comment): two otherwise-identical requests differing
// only in this field must produce the SAME ID, exactly like differing only
// in Now/Parent already does (TestGenerationID_DeterministicAcrossCalls).
// This is the load-bearing property behind cmd/omca always passing the
// worktree's own stable PATH-shim path here rather than a snapshot of the
// currently-running omca binary's own resolved location: if this field DID
// participate in the ID, every `go run ./cmd/omca ...` invocation during
// development (verified to resolve a different os.Executable() path on every
// single run) would mint a brand-new generation and never hit
// EnsureGeneration's reuse fast path, defeating the whole steady-state story
// this PR's own internal/perf package measures.
func TestGenerationID_StableAcrossOMCABinaryPathChange(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reqEmpty := buildSimpleCodexRequest(t, "# instructions\n", now)

	reqA := reqEmpty
	reqA.OMCABinaryPath = "/opt/homebrew/bin/omca"
	reqB := reqEmpty
	reqB.OMCABinaryPath = "/usr/local/bin/omca"

	idEmpty, err := GenerationID(reqEmpty)
	if err != nil {
		t.Fatalf("GenerationID(empty): %v", err)
	}
	idA, err := GenerationID(reqA)
	if err != nil {
		t.Fatalf("GenerationID(A): %v", err)
	}
	idB, err := GenerationID(reqB)
	if err != nil {
		t.Fatalf("GenerationID(B): %v", err)
	}
	if idA != idEmpty || idB != idEmpty {
		t.Fatalf("GenerationID changed when only OMCABinaryPath changed: empty=%q, A=%q, B=%q (want all three identical)", idEmpty, idA, idB)
	}
}
