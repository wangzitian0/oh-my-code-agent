package observe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func TestObserve_UnknownHostID(t *testing.T) {
	_, err := Observe(Request{Detection: hostcontext.HostDetection{Host: "not-a-real-host"}})
	if err == nil {
		t.Fatal("Observe(not-a-real-host): want error, got nil")
	}
}

func TestObserve_KnownButUnimplementedHostID(t *testing.T) {
	// "opencode" is a canonical host ID (domain.KnownHostIDs) but this
	// package only implements physical-mapping knowledge for codex and
	// claude-code — the same distinct failure mode
	// internal/context/host.go's DetectHost draws between "not a host ID at
	// all" and "a known host ID we don't implement."
	_, err := Observe(Request{Detection: hostcontext.HostDetection{Host: "opencode"}})
	if err == nil {
		t.Fatal("Observe(opencode): want error, got nil")
	}
	if !strings.Contains(err.Error(), "does not implement observation") {
		t.Errorf("error = %q, want it to explain observation is unimplemented for this known host", err.Error())
	}
}

func TestObserve_EmptyRequest_NoHomesNoWorktree(t *testing.T) {
	obs, err := Observe(Request{Detection: hostcontext.HostDetection{Host: "codex"}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(obs) != 0 {
		t.Fatalf("Observe with no native homes and no worktree root: got %d observations, want 0", len(obs))
	}
}

func TestObserve_NativeHomeDoesNotExist_SilentlySkipped(t *testing.T) {
	tr := newCodexTree(t) // never created on disk
	obs, err := Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(obs) != 0 {
		t.Fatalf("Observe over nonexistent native homes and worktree root: got %d observations, want 0", len(obs))
	}
}

func TestObserve_NonAbsoluteNativeHomePath_Errors(t *testing.T) {
	req := Request{
		Detection: hostcontext.HostDetection{
			Host: "codex",
			NativeHomes: []hostcontext.NativeHome{
				{Name: "CODEX_HOME", Path: "relative/codex-home"},
			},
		},
	}
	if _, err := Observe(req); err == nil {
		t.Fatal("Observe with a non-absolute native home path: want error, got nil")
	}
}

func TestObserve_NonAbsoluteWorktreeRoot_Errors(t *testing.T) {
	req := Request{
		Detection:    hostcontext.HostDetection{Host: "codex"},
		WorktreeRoot: "relative/project",
	}
	if _, err := Observe(req); err == nil {
		t.Fatal("Observe with a non-absolute WorktreeRoot: want error, got nil")
	}
}

// TestObserve_Codex_FullLayout builds a synthetic Codex host layout
// exercising every source rule this package implements for Codex
// (codexUserRules, codexWorkspaceRules) and asserts every expected
// Observation is present, with the right concept/scope/content, and that
// nothing unexpected was reported.
func TestObserve_Codex_FullLayout(t *testing.T) {
	tr := newCodexTree(t)

	// CODEX_HOME (user scope): both AGENTS.override.md and AGENTS.md exist
	// -- this package reports both (no precedence filtering; see rules.go).
	mustWriteFile(t, filepath.Join(tr.CodexHome, "AGENTS.override.md"), "# override instructions\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "AGENTS.md"), "# base instructions\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "config.toml"), "[mcp_servers.demo]\ncommand = \"npx\"\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", "deploy", "SKILL.md"), "---\nname: deploy\n---\nbody\n")
	// A non-SKILL.md file sitting alongside a skill package must not be
	// picked up as a skill (marker-restricted walk).
	mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", "deploy", "README.md"), "not a skill marker\n")

	// $HOME/.agents/skills (user scope, shared root).
	mustWriteFile(t, filepath.Join(tr.HomeAgentsDir, "shared", "SKILL.md"), "---\nname: shared\n---\nbody\n")

	// Worktree root (workspace scope): only AGENTS.md this time (override
	// absent at this root) to prove candidateFiles does not fabricate a
	// record for a name that is not present.
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# project instructions\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".codex", "config.toml"), "[mcp_servers.proj]\ncommand = \"./run.sh\"\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".agents", "skills", "proj-skill", "SKILL.md"), "---\nname: proj-skill\n---\nbody\n")

	obs, err := Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	type want struct {
		concept string
		path    string
		scope   string
		root    string
	}
	wants := []want{
		{conceptInstruction, filepath.Join(tr.CodexHome, "AGENTS.override.md"), "user", tr.CodexHome},
		{conceptInstruction, filepath.Join(tr.CodexHome, "AGENTS.md"), "user", tr.CodexHome},
		{conceptMCPServer, filepath.Join(tr.CodexHome, "config.toml"), "user", tr.CodexHome},
		{conceptSkill, filepath.Join(tr.CodexHome, "skills", "deploy", "SKILL.md"), "user", tr.CodexHome},
		{conceptSkill, filepath.Join(tr.HomeAgentsDir, "shared", "SKILL.md"), "user", tr.HomeAgentsDir},
		{conceptInstruction, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "workspace", tr.WorktreeRoot},
		{conceptMCPServer, filepath.Join(tr.WorktreeRoot, ".codex", "config.toml"), "workspace", tr.WorktreeRoot},
		{conceptSkill, filepath.Join(tr.WorktreeRoot, ".agents", "skills", "proj-skill", "SKILL.md"), "workspace", tr.WorktreeRoot},
	}
	for _, w := range wants {
		o := findObservation(t, obs, w.concept, w.path)
		if o.Spec.Scope.Kind != w.scope {
			t.Errorf("%s: scope.kind = %q, want %q", w.path, o.Spec.Scope.Kind, w.scope)
		}
		if o.Spec.Scope.Root != w.root {
			t.Errorf("%s: scope.root = %q, want %q", w.path, o.Spec.Scope.Root, w.root)
		}
		if o.Spec.EvidenceLevel != domain.EvidenceLevelParsed {
			t.Errorf("%s: evidenceLevel = %s, want E1", w.path, o.Spec.EvidenceLevel)
		}
		if o.Spec.Host.ID != "codex" || o.Spec.Host.Version != "0.144.5" {
			t.Errorf("%s: host = %+v, want codex 0.144.5", w.path, o.Spec.Host)
		}
	}
	if hasObservation(obs, conceptSkill, filepath.Join(tr.CodexHome, "skills", "deploy", "README.md")) {
		t.Error("README.md next to a skill package must not be reported as a skill (marker-restricted)")
	}
	if len(obs) != len(wants) {
		t.Errorf("got %d observations, want exactly %d: %+v", len(obs), len(wants), obs)
	}

	// config.toml is never structurally parsed (see walk.go's parseContent
	// doc comment): OpaqueVendorFields["content"] must be the raw text,
	// not decoded into a map.
	mcp := findObservation(t, obs, conceptMCPServer, filepath.Join(tr.CodexHome, "config.toml"))
	content, ok := mcp.Spec.OpaqueVendorFields["content"].(string)
	if !ok {
		t.Fatalf("codex config.toml OpaqueVendorFields[content] type = %T, want string", mcp.Spec.OpaqueVendorFields["content"])
	}
	if !strings.Contains(content, "mcp_servers.demo") {
		t.Errorf("codex config.toml opaque content = %q, missing expected raw text", content)
	}
}

// TestObserve_ClaudeCode_FullLayout mirrors TestObserve_Codex_FullLayout for
// Claude Code, additionally proving JSON MCP sources are parsed losslessly
// into a generic structure (unlike Codex's opaque TOML).
func TestObserve_ClaudeCode_FullLayout(t *testing.T) {
	tr := newClaudeTree(t)

	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "CLAUDE.md"), "# user instructions\n")
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "rules", "style.md"), "# style rule\n")
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "rules", "nested", "deep.md"), "# nested rule\n")
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, ".claude.json"), `{"mcpServers":{"demo":{"command":"npx"}}}`)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "skills", "deploy", "SKILL.md"), "---\nname: deploy\n---\nbody\n")

	mustWriteFile(t, filepath.Join(tr.HomeAgentsDir, "shared", "SKILL.md"), "---\nname: shared\n---\nbody\n")

	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "CLAUDE.md"), "# project instructions (root)\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".claude", "CLAUDE.md"), "# project instructions (.claude)\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".claude", "rules", "x.md"), "# project rule\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".mcp.json"), `{"mcpServers":{"proj":{"command":"./run.sh"}}}`)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".claude", "skills", "proj-skill", "SKILL.md"), "---\nname: proj-skill\n---\nbody\n")

	obs, err := Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	type want struct{ concept, path string }
	wants := []want{
		{conceptInstruction, filepath.Join(tr.ClaudeConfigDir, "CLAUDE.md")},
		{conceptInstruction, filepath.Join(tr.ClaudeConfigDir, "rules", "style.md")},
		{conceptInstruction, filepath.Join(tr.ClaudeConfigDir, "rules", "nested", "deep.md")},
		{conceptMCPServer, filepath.Join(tr.ClaudeConfigDir, ".claude.json")},
		{conceptSkill, filepath.Join(tr.ClaudeConfigDir, "skills", "deploy", "SKILL.md")},
		{conceptSkill, filepath.Join(tr.HomeAgentsDir, "shared", "SKILL.md")},
		{conceptInstruction, filepath.Join(tr.WorktreeRoot, "CLAUDE.md")},
		{conceptInstruction, filepath.Join(tr.WorktreeRoot, ".claude", "CLAUDE.md")},
		{conceptInstruction, filepath.Join(tr.WorktreeRoot, ".claude", "rules", "x.md")},
		{conceptMCPServer, filepath.Join(tr.WorktreeRoot, ".mcp.json")},
		{conceptSkill, filepath.Join(tr.WorktreeRoot, ".claude", "skills", "proj-skill", "SKILL.md")},
	}
	for _, w := range wants {
		findObservation(t, obs, w.concept, w.path)
	}
	if len(obs) != len(wants) {
		t.Errorf("got %d observations, want exactly %d: %+v", len(obs), len(wants), obs)
	}

	// .claude.json / .mcp.json ARE structurally parsed: content must be a
	// generic map carrying every field losslessly, not a raw string.
	mcp := findObservation(t, obs, conceptMCPServer, filepath.Join(tr.ClaudeConfigDir, ".claude.json"))
	content, ok := mcp.Spec.OpaqueVendorFields["content"].(map[string]any)
	if !ok {
		t.Fatalf(".claude.json OpaqueVendorFields[content] type = %T, want map[string]any", mcp.Spec.OpaqueVendorFields["content"])
	}
	servers, ok := content["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf(".claude.json parsed content missing mcpServers: %+v", content)
	}
	if _, ok := servers["demo"]; !ok {
		t.Errorf(".claude.json parsed content lost the 'demo' server entry: %+v", servers)
	}
}

func TestObserve_MalformedJSON_FallsBackToRawText(t *testing.T) {
	tr := newClaudeTree(t)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, ".claude.json"), `{not valid json`)

	obs, err := Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	mcp := findObservation(t, obs, conceptMCPServer, filepath.Join(tr.ClaudeConfigDir, ".claude.json"))
	content, ok := mcp.Spec.OpaqueVendorFields["content"].(string)
	if !ok {
		t.Fatalf("malformed .claude.json OpaqueVendorFields[content] type = %T, want string fallback", mcp.Spec.OpaqueVendorFields["content"])
	}
	if content != `{not valid json` {
		t.Errorf("malformed .claude.json opaque content = %q, want the raw text preserved verbatim", content)
	}
	if mcp.Spec.EvidenceLevel != domain.EvidenceLevelParsed {
		t.Errorf("evidenceLevel = %s, want E1 (still read and retained, just not structurally parsed)", mcp.Spec.EvidenceLevel)
	}
}

// TestObserve_UnreadableFile_EmitsE0 proves the E0 (EvidenceLevelDiscovered)
// path: a file that exists but cannot be read still produces a record
// (source path, scope, evidence level E0, and non-empty raw/parsed digests
// derived from a fixed placeholder rather than any actual content) instead
// of being silently dropped. Skipped when running as root, since root
// bypasses Unix permission bits and the test's premise would not hold.
func TestObserve_UnreadableFile_EmitsE0(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits only")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not block reads")
	}

	tr := newCodexTree(t)
	path := filepath.Join(tr.CodexHome, "AGENTS.md")
	mustWriteFile(t, path, "# unreadable\n")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) }) // let t.TempDir() clean up successfully

	obs, err := Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	o := findObservation(t, obs, conceptInstruction, path)
	if o.Spec.EvidenceLevel != domain.EvidenceLevelDiscovered {
		t.Errorf("evidenceLevel = %s, want E0 (discovered but unreadable)", o.Spec.EvidenceLevel)
	}
	if o.Spec.Disposition != domain.DispositionDiscovered {
		t.Errorf("disposition = %s, want DISCOVERED", o.Spec.Disposition)
	}
	if readable, ok := o.Spec.OpaqueVendorFields["readable"].(bool); !ok || readable {
		t.Errorf("OpaqueVendorFields[readable] = %v (ok=%v), want false", o.Spec.OpaqueVendorFields["readable"], ok)
	}
}

// TestObserve_TwoCallsAreIndependent proves Observe does not accidentally
// share or mutate state across calls: two Requests built from two entirely
// separate synthetic trees produce disjoint, correctly-scoped results.
func TestObserve_TwoCallsAreIndependent(t *testing.T) {
	tr1 := newCodexTree(t)
	mustWriteFile(t, filepath.Join(tr1.CodexHome, "AGENTS.md"), "# tree one\n")

	tr2 := newCodexTree(t)
	mustWriteFile(t, filepath.Join(tr2.CodexHome, "AGENTS.md"), "# tree two\n")

	obs1, err := Observe(tr1.request("0.144.5"))
	if err != nil {
		t.Fatal(err)
	}
	obs2, err := Observe(tr2.request("0.144.5"))
	if err != nil {
		t.Fatal(err)
	}
	if len(obs1) != 1 || len(obs2) != 1 {
		t.Fatalf("got %d and %d observations, want 1 and 1", len(obs1), len(obs2))
	}
	if obs1[0].Spec.Source.Path == obs2[0].Spec.Source.Path {
		t.Fatal("two independent trees produced the same source path")
	}
	if obs1[0].Spec.RawDigest == obs2[0].Spec.RawDigest {
		t.Fatal("two files with different content produced the same rawDigest")
	}
}

// jsonRoundTrip is a small determinism/inspection helper: marshal obs and
// unmarshal it back into a generic value, failing the test on any error.
func jsonRoundTrip(t *testing.T, obs []domain.Observation) string {
	t.Helper()
	raw, err := json.Marshal(obs)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(raw)
}
