package observe

import (
	"path/filepath"
	"testing"
)

// TestObserve_Local_ClaudeCode_Golden is this PR's (issue #20) golden
// fixture for the `local` scope: CLAUDE.local.md at the worktree root.
// Codex has no local-scope counterpart (claudeLocalRules's doc comment), so
// there is no Codex golden test for this scope.
func TestObserve_Local_ClaudeCode_Golden(t *testing.T) {
	tr := newClaudeTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "CLAUDE.local.md"), "# local-only, gitignored\n")

	obs, err := Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	wantIDs := []string{
		"claude-code:instruction:" + filepath.Join(tr.WorktreeRoot, "CLAUDE.local.md"),
	}
	assertExactIDs(t, filterByScope(obs, "local"), wantIDs)

	o := findObservation(t, obs, conceptInstruction, filepath.Join(tr.WorktreeRoot, "CLAUDE.local.md"))
	if o.Spec.Scope.Kind != "local" {
		t.Errorf("scope.kind = %q, want %q", o.Spec.Scope.Kind, "local")
	}
	if o.Spec.Scope.Root != tr.WorktreeRoot {
		t.Errorf("scope.root = %q, want %q", o.Spec.Scope.Root, tr.WorktreeRoot)
	}
}

// TestObserve_Local_Codex_NoLocalScopeSource proves Codex's absence of a
// local-scope source is not silently different from "not implemented yet":
// Observe simply never produces a `local`-scope record for Codex, by design
// (see claudeLocalRules's doc comment).
func TestObserve_Local_Codex_NoLocalScopeSource(t *testing.T) {
	tr := newCodexTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# project\n")

	obs, err := Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if got := filterByScope(obs, "local"); len(got) != 0 {
		t.Errorf("Codex produced %d `local`-scope observations, want 0: %+v", len(got), got)
	}
}
