package observe

import (
	"os"
	"path/filepath"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func TestDirectoryChain_SameDir_EmptyNoError(t *testing.T) {
	chain, err := directoryChain("/repo", "/repo")
	if err != nil {
		t.Fatalf("directoryChain: %v", err)
	}
	if len(chain) != 0 {
		t.Fatalf("directoryChain(root, root) = %v, want empty", chain)
	}
}

func TestDirectoryChain_NestedPath_RootToLeafOrder(t *testing.T) {
	chain, err := directoryChain(filepath.Join("/repo"), filepath.Join("/repo", "a", "b", "c"))
	if err != nil {
		t.Fatalf("directoryChain: %v", err)
	}
	want := []string{
		filepath.Join("/repo", "a"),
		filepath.Join("/repo", "a", "b"),
		filepath.Join("/repo", "a", "b", "c"),
	}
	if len(chain) != len(want) {
		t.Fatalf("directoryChain = %v, want %v", chain, want)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Errorf("chain[%d] = %q, want %q", i, chain[i], want[i])
		}
	}
}

func TestDirectoryChain_OutsideRoot_Errors(t *testing.T) {
	if _, err := directoryChain("/repo", "/somewhere/else"); err == nil {
		t.Fatal("directoryChain with workingDir outside worktreeRoot: want error, got nil")
	}
}

// TestObserve_Directory_Codex_Golden is this PR's (issue #20) golden fixture
// for the `directory` scope, Codex side: a nested "a/b" directory chain
// under the worktree root, each level carrying its own Instructions/MCP/
// Skills sources per docs/ontology/README.md §6.2's "root to cwd" wording.
func TestObserve_Directory_Codex_Golden(t *testing.T) {
	tr := newCodexTree(t)

	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# root\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "a", "AGENTS.md"), "# a\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "a", "b", "AGENTS.override.md"), "# a/b override\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "a", "b", ".codex", "config.toml"), "[mcp_servers.nested]\ncommand = \"./x\"\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "a", "b", ".agents", "skills", "nested-skill", "SKILL.md"), "---\nname: nested\n---\n")

	req := tr.request("0.144.5")
	req.WorkingDirectory = filepath.Join(tr.WorktreeRoot, "a", "b")

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	wantDirectoryScopeIDs := []string{
		"codex:instruction:" + filepath.Join(tr.WorktreeRoot, "a", "AGENTS.md"),
		"codex:instruction:" + filepath.Join(tr.WorktreeRoot, "a", "b", "AGENTS.override.md"),
		"codex:hook:" + filepath.Join(tr.WorktreeRoot, "a", "b", ".codex", "config.toml"),
		"codex:mcp_server:" + filepath.Join(tr.WorktreeRoot, "a", "b", ".codex", "config.toml"),
		"codex:policy:" + filepath.Join(tr.WorktreeRoot, "a", "b", ".codex", "config.toml"),
		"codex:skill:" + filepath.Join(tr.WorktreeRoot, "a", "b", ".agents", "skills", "nested-skill", "SKILL.md"),
	}
	assertExactIDs(t, filterByScope(obs, "directory"), wantDirectoryScopeIDs)

	// The root-level AGENTS.md must still be reported once, at `workspace`
	// scope, not duplicated by the directory-chain walk (directoryChain
	// excludes worktreeRoot itself — see its doc comment).
	root := findObservation(t, obs, conceptInstruction, filepath.Join(tr.WorktreeRoot, "AGENTS.md"))
	if root.Spec.Scope.Kind != "workspace" {
		t.Errorf("worktree-root AGENTS.md scope.kind = %q, want %q", root.Spec.Scope.Kind, "workspace")
	}
}

// TestObserve_Directory_ClaudeCode_Golden mirrors the Codex directory-chain
// golden test for Claude Code, which only chains Instructions (see
// claudeDirectoryChainRules's doc comment).
func TestObserve_Directory_ClaudeCode_Golden(t *testing.T) {
	tr := newClaudeTree(t)

	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "CLAUDE.md"), "# root\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "svc", "CLAUDE.md"), "# svc\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "svc", ".mcp.json"), `{"mcpServers":{"nested":{"command":"./x"}}}`)

	req := tr.request("2.1.211")
	req.WorkingDirectory = filepath.Join(tr.WorktreeRoot, "svc")

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	wantDirectoryScopeIDs := []string{
		"claude-code:instruction:" + filepath.Join(tr.WorktreeRoot, "svc", "CLAUDE.md"),
	}
	assertExactIDs(t, filterByScope(obs, "directory"), wantDirectoryScopeIDs)

	// .mcp.json under svc/ is NOT part of Claude Code's documented ancestor
	// chain (only CLAUDE.md is) — it must not appear at all, proving this
	// package does not over-apply the directory walk beyond what's
	// documented.
	if hasObservation(obs, conceptMCPServer, filepath.Join(tr.WorktreeRoot, "svc", ".mcp.json")) {
		t.Error("svc/.mcp.json must not be observed: Claude Code's directory chain covers Instructions only")
	}
}

// TestObserve_Directory_SymlinkedChainSegment_NotFollowed proves the
// scope-containment fix: a directory-chain segment that is itself a symlink
// pointing outside WorktreeRoot must never be walked into. Before the fix,
// the chain loop used os.Stat (which resolves the symlink) instead of
// os.Lstat, so a symlinked chain segment was treated as an ordinary
// directory and observeRoot walked straight through it — reading sources
// from wherever the symlink actually pointed, outside the declared scope
// root, the same boundary violation observeFile already guards against for
// individual symlinked files (see walk.go).
func TestObserve_Directory_SymlinkedChainSegment_NotFollowed(t *testing.T) {
	tr := newCodexTree(t)

	// outside is a real directory that is NOT under WorktreeRoot at all —
	// planting a canary here and proving it never appears in Observe's
	// output is the actual proof of containment, not just "no error."
	outside := filepath.Join(filepath.Dir(tr.WorktreeRoot), "outside-worktree")
	mustWriteFile(t, filepath.Join(outside, "AGENTS.md"), "# canary: must never be observed\n")

	if err := os.MkdirAll(tr.WorktreeRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktree root: %v", err)
	}
	symlinkPath := filepath.Join(tr.WorktreeRoot, "a")
	if err := os.Symlink(outside, symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	req := tr.request("0.144.5")
	req.WorkingDirectory = symlinkPath

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	if got := filterByScope(obs, "directory"); len(got) != 0 {
		t.Fatalf("directory-scope observations for a symlinked chain segment: got %d, want 0 (symlink must not be followed): %+v", len(got), got)
	}
	for _, o := range obs {
		if o.Spec.Source.Path == filepath.Join(outside, "AGENTS.md") {
			t.Fatalf("found an observation for %s: the outside-worktree canary leaked through a symlinked directory-chain segment", o.Spec.Source.Path)
		}
	}
}

func TestObserve_Directory_WorkingDirectoryWithoutWorktreeRoot_Errors(t *testing.T) {
	req := Request{
		Detection:        hostcontext.HostDetection{Host: "codex", Surface: "cli", Version: "0.144.5"},
		WorkingDirectory: filepath.Join("/some", "abs", "dir"),
	}
	if _, err := Observe(req); err == nil {
		t.Fatal("Observe with WorkingDirectory but no WorktreeRoot: want error, got nil")
	}
}

func TestObserve_Directory_NonAbsoluteWorkingDirectory_Errors(t *testing.T) {
	tr := newCodexTree(t)
	req := tr.request("0.144.5")
	req.WorkingDirectory = "relative/dir"
	if _, err := Observe(req); err == nil {
		t.Fatal("Observe with a non-absolute WorkingDirectory: want error, got nil")
	}
}

// filterByScope returns every observation in obs whose Scope.Kind equals
// scope, used by the directory-chain golden tests above to isolate the
// `directory`-scoped subset of a larger result for an exact-IDs comparison.
func filterByScope(obs []domain.Observation, scope string) []domain.Observation {
	var out []domain.Observation
	for _, o := range obs {
		if o.Spec.Scope.Kind == scope {
			out = append(out, o)
		}
	}
	return out
}
