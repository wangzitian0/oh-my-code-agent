package observe

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestObserve_System_Codex_Golden is this PR's (issue #20) golden fixture
// for the `system`/`managed` scope, Codex side: a synthetic stand-in for
// /etc/codex (see system.go's SystemRoot doc comment for why this package
// never touches the real /etc/codex in a test) populated with every source
// codexSystemRules names, asserting the exact entry count and exact set of
// Metadata.IDs Observe produces — "entry count + logical IDs" per issue
// #20's round-3 acceptance criterion.
func TestObserve_System_Codex_Golden(t *testing.T) {
	tr := newCodexTree(t)
	etcCodex := filepath.Join(t.TempDir(), "etc-codex")

	mustWriteFile(t, filepath.Join(etcCodex, "config.toml"), "[mcp_servers.managed]\ncommand = \"npx\"\n")
	mustWriteFile(t, filepath.Join(etcCodex, "requirements.toml"), "approval_policy = \"never\"\n")
	mustWriteFile(t, filepath.Join(etcCodex, "skills", "audit", "SKILL.md"), "---\nname: audit\n---\nbody\n")
	mustWriteFile(t, filepath.Join(etcCodex, "plugins", "shipit", ".codex-plugin", "plugin.json"), `{"name":"shipit"}`)

	req := tr.request("0.144.5")
	req.SystemRoots = []SystemRoot{{Name: "ETC_CODEX", Path: etcCodex}}

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	wantIDs := []string{
		"codex:hook:" + filepath.Join(etcCodex, "config.toml"),
		"codex:mcp_server:" + filepath.Join(etcCodex, "config.toml"),
		"codex:plugin:" + filepath.Join(etcCodex, "plugins", "shipit", ".codex-plugin", "plugin.json"),
		"codex:policy:" + filepath.Join(etcCodex, "config.toml"),
		"codex:policy:" + filepath.Join(etcCodex, "requirements.toml"),
		"codex:skill:" + filepath.Join(etcCodex, "skills", "audit", "SKILL.md"),
	}
	assertExactIDs(t, obs, wantIDs)

	for _, o := range obs {
		if o.Spec.Scope.Kind != "managed" {
			t.Errorf("%s: scope.kind = %q, want %q", o.Metadata.ID, o.Spec.Scope.Kind, "managed")
		}
		if o.Spec.Scope.Root != etcCodex {
			t.Errorf("%s: scope.root = %q, want %q", o.Metadata.ID, o.Spec.Scope.Root, etcCodex)
		}
	}
}

// TestObserve_System_ClaudeCode_Golden mirrors the Codex system-scope golden
// test for Claude Code's managed root.
func TestObserve_System_ClaudeCode_Golden(t *testing.T) {
	tr := newClaudeTree(t)
	managed := filepath.Join(t.TempDir(), "claude-managed")

	mustWriteFile(t, filepath.Join(managed, "CLAUDE.md"), "# managed instructions\n")
	mustWriteFile(t, filepath.Join(managed, "managed-settings.json"), `{"permissions":{"deny":["Bash(rm -rf *)"]}}`)

	req := tr.request("2.1.211")
	req.SystemRoots = []SystemRoot{{Name: "CLAUDE_MANAGED", Path: managed}}

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	wantIDs := []string{
		"claude-code:hook:" + filepath.Join(managed, "managed-settings.json"),
		"claude-code:instruction:" + filepath.Join(managed, "CLAUDE.md"),
		"claude-code:plugin:" + filepath.Join(managed, "managed-settings.json"),
		"claude-code:policy:" + filepath.Join(managed, "managed-settings.json"),
	}
	assertExactIDs(t, obs, wantIDs)

	for _, o := range obs {
		if o.Spec.Scope.Kind != "managed" {
			t.Errorf("%s: scope.kind = %q, want %q", o.Metadata.ID, o.Spec.Scope.Kind, "managed")
		}
	}

	// managed-settings.json IS JSON-shaped: prove it is structurally parsed,
	// not retained as opaque text, mirroring the existing .claude.json/
	// .mcp.json coverage (observe_test.go's TestObserve_ClaudeCode_FullLayout).
	settings := findObservation(t, obs, conceptPolicy, filepath.Join(managed, "managed-settings.json"))
	if _, ok := settings.Spec.OpaqueVendorFields["content"].(map[string]any); !ok {
		t.Fatalf("managed-settings.json OpaqueVendorFields[content] type = %T, want map[string]any", settings.Spec.OpaqueVendorFields["content"])
	}
}

// TestObserve_System_MissingRoot_SilentlySkipped proves a SystemRoot whose
// path does not exist on disk is silently skipped, the same non-fatal
// "not found" stance every other root this package walks takes.
func TestObserve_System_MissingRoot_SilentlySkipped(t *testing.T) {
	tr := newCodexTree(t)
	req := tr.request("0.144.5")
	req.SystemRoots = []SystemRoot{{Name: "ETC_CODEX", Path: filepath.Join(t.TempDir(), "does-not-exist")}}

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(obs) != 0 {
		t.Fatalf("Observe over a nonexistent system root: got %d observations, want 0", len(obs))
	}
}

// TestObserve_System_NonAbsolutePath_Errors mirrors
// TestObserve_NonAbsoluteNativeHomePath_Errors for SystemRoots.
func TestObserve_System_NonAbsolutePath_Errors(t *testing.T) {
	tr := newCodexTree(t)
	req := tr.request("0.144.5")
	req.SystemRoots = []SystemRoot{{Name: "ETC_CODEX", Path: "relative/etc-codex"}}

	if _, err := Observe(req); err == nil {
		t.Fatal("Observe with a non-absolute SystemRoot path: want error, got nil")
	}
}

// assertExactIDs fails the test unless obs's sorted Metadata.IDs exactly
// equal wantIDs (also sorted) — the "entry count + logical IDs" golden
// assertion shape issue #20's round-3 acceptance criterion asks for.
func assertExactIDs(t *testing.T, obs []domain.Observation, wantIDs []string) {
	t.Helper()
	got := make([]string, len(obs))
	for i, o := range obs {
		got[i] = o.Metadata.ID
	}
	want := append([]string(nil), wantIDs...)
	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("got %d observations, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("observation IDs mismatch at index %d: got %q, want %q\ngot:  %v\nwant: %v", i, got[i], want[i], got, want)
		}
	}
}
