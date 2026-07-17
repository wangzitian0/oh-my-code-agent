package observe

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain/redact"
)

// realSecretPlaceholder is the exact value already committed in
// fixtures/codex/0.144.5/mcp-merge/input/codex-home/config.toml and
// fixtures/claude-code/2.1.211/mcp-merge/input/claude-config/.claude.json,
// under an EXAMPLE_TOKEN/env key — this test reads it from those existing,
// already-merged PR-06 fixtures rather than inventing a new secret-shaped
// literal of its own, so there is nothing new here for a secret scanner to
// flag.
const realSecretPlaceholder = "user-level-placeholder-not-a-real-secret"

// TestObserve_RedactionSafe_OpaqueTOMLEnvBlock is this PR's acceptance
// criterion "the PR-04 redaction tests pass over real observation output"
// exercised against the opaque-text content branch (Codex's config.toml is
// never structurally parsed, see walk.go's parseContent doc comment): an
// MCP server's env block carries an API-token-shaped value, and
// internal/domain/redact's shape-based scan must catch it even though this
// package retained the whole file as one opaque string.
func TestObserve_RedactionSafe_OpaqueTOMLEnvBlock(t *testing.T) {
	fixtureConfig := filepath.Join(repoFixturesDir(), "codex", "0.144.5", "mcp-merge", "input", "codex-home", "config.toml")

	tr := newCodexTree(t)
	copyFixtureFile(t, fixtureConfig, filepath.Join(tr.CodexHome, "config.toml"))

	obs, err := Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(obs) == 0 {
		t.Fatal("Observe returned no observations; this test would be vacuous")
	}

	rawJSON, err := json.Marshal(obs)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(rawJSON), realSecretPlaceholder) {
		t.Fatalf("raw (unredacted) Observe() output does not contain the fixture's secret-shaped placeholder; this test would be vacuous:\n%s", rawJSON)
	}

	redacted, err := redact.JSON(obs)
	if err != nil {
		t.Fatalf("redact.JSON: %v", err)
	}
	if strings.Contains(string(redacted), realSecretPlaceholder) {
		t.Fatalf("redact.JSON(Observe() output) leaked the secret-shaped placeholder:\n%s", redacted)
	}
	if !strings.Contains(string(redacted), "REDACTED:sha256:") {
		t.Error("expected the redacted output to contain at least one REDACTED marker")
	}
	// Sanity: non-secret content must survive redaction (this must not be a
	// redact-everything result that would trivially satisfy the assertions
	// above).
	if !strings.Contains(string(redacted), "shared-tools") {
		t.Error("expected non-secret content (the 'shared-tools' server id) to survive redaction")
	}
}

// TestObserve_RedactionSafe_ParsedJSONEnvBlock is the same proof over the
// other content branch: Claude Code's .claude.json IS structurally parsed
// into a generic map (see walk.go's parseContent), so this exercises
// internal/domain/redact's key-name-based redaction (the "env"/"apiKey"/
// "token"-shaped map key match), not just its string shape scan.
func TestObserve_RedactionSafe_ParsedJSONEnvBlock(t *testing.T) {
	fixtureConfig := filepath.Join(repoFixturesDir(), "claude-code", "2.1.211", "mcp-merge", "input", "claude-config", ".claude.json")

	tr := newClaudeTree(t)
	copyFixtureFile(t, fixtureConfig, filepath.Join(tr.ClaudeConfigDir, ".claude.json"))

	obs, err := Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(obs) == 0 {
		t.Fatal("Observe returned no observations; this test would be vacuous")
	}

	mcp := findObservation(t, obs, conceptMCPServer, filepath.Join(tr.ClaudeConfigDir, ".claude.json"))
	if _, ok := mcp.Spec.OpaqueVendorFields["content"].(map[string]any); !ok {
		t.Fatalf("expected .claude.json content to be parsed into a map, got %T", mcp.Spec.OpaqueVendorFields["content"])
	}

	rawJSON, err := json.Marshal(obs)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(rawJSON), realSecretPlaceholder) {
		t.Fatalf("raw (unredacted) Observe() output does not contain the fixture's secret-shaped placeholder; this test would be vacuous:\n%s", rawJSON)
	}

	redacted, err := redact.JSON(obs)
	if err != nil {
		t.Fatalf("redact.JSON: %v", err)
	}
	if strings.Contains(string(redacted), realSecretPlaceholder) {
		t.Fatalf("redact.JSON(Observe() output) leaked the secret-shaped placeholder:\n%s", redacted)
	}
}

// TestObserve_RedactionSafe_UnreadableFileHasNoContentToLeak proves the E0
// path can never leak anything: OpaqueVendorFields carries no "content" key
// at all when a file could not be read, so there is nothing for redaction
// to even need to catch. It calls buildObservation directly (rather than
// going through a real chmod'd file, which TestObserve_UnreadableFile_EmitsE0
// already covers end-to-end) to isolate this specific shape guarantee.
func TestObserve_RedactionSafe_UnreadableFileHasNoContentToLeak(t *testing.T) {
	fakeReadErr := errors.New("fake: permission denied")
	obs, err := buildObservation("codex", "0.144.5", "cli", conceptInstruction, "user", "/native/home", "/native/home/AGENTS.md", nil, fakeReadErr)
	if err != nil {
		t.Fatalf("buildObservation: %v", err)
	}
	if _, ok := obs.Spec.OpaqueVendorFields["content"]; ok {
		t.Error("an E0 (unreadable) observation must never carry a content field")
	}
}

// copyFixtureFile copies src (an existing, committed fixture file this repo
// already ships) to dst, failing the test on any error. Kept in this test
// file rather than helpers_test.go since only the redaction tests need to
// reuse committed fixture content byte-for-byte (every other test in this
// package builds its own minimal synthetic tree).
func copyFixtureFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("copyFixtureFile: read %s: %v", src, err)
	}
	mustWriteFile(t, dst, string(data))
}
