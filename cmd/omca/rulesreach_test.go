package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures are shaped like the two real placements that differ, measured
// 2026-09-23 on two worktrees of the same commit: one under the directory that
// carries the cross-repository rules, one in a session scratchpad.
func placeWorktree(t *testing.T, underCarrier bool) string {
	t.Helper()
	base := t.TempDir()
	if underCarrier {
		if err := os.WriteFile(filepath.Join(base, "CLAUDE.md"), []byte("workspace rules\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wt := filepath.Join(base, "repo_issue1_slug")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	// Every worktree has the repository's own committed rules. They are never
	// the thing in question, and a check that counted them would pass always.
	if err := os.WriteFile(filepath.Join(wt, "AGENTS.md"), []byte("repo rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return wt
}

func TestRulesReach_UnderTheCarrierIsOK(t *testing.T) {
	f := checkRulesReach(placeWorktree(t, true))
	if f.Status != statusOK {
		t.Fatalf("want OK, got %s: %s", f.Status, f.Detail)
	}
	if !strings.Contains(f.Detail, "inherited") {
		t.Fatalf("the detail must say which channel carried it: %q", f.Detail)
	}
}

// The case the check exists for. Nothing else reports it, and the session runs
// without the escalation discipline, the red lines and the collaboration model.
func TestRulesReach_OutsideTheCarrierIsFail(t *testing.T) {
	f := checkRulesReach(placeWorktree(t, false))
	if f.Status != statusFail {
		t.Fatalf("a worktree with no channel at all must FAIL, got %s: %s", f.Status, f.Detail)
	}
}

func TestRulesReach_RenderedArtifactAloneIsEnough(t *testing.T) {
	wt := placeWorktree(t, false)
	if err := os.WriteFile(filepath.Join(wt, "CLAUDE.local.md"), []byte("rendered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := checkRulesReach(wt)
	if f.Status != statusOK {
		t.Fatalf("a rendered artifact is the other valid channel, got %s: %s", f.Status, f.Detail)
	}
	if !strings.Contains(f.Detail, "CLAUDE.local.md") {
		t.Fatalf("the detail must name the channel: %q", f.Detail)
	}
}

// The false green this check is most likely to produce: a repository's own
// committed CLAUDE.md sits in the worktree, so a naive "is there a rule file"
// test passes everywhere and the layers above stay missing.
func TestRulesReach_TheWorktreesOwnRulesDoNotCount(t *testing.T) {
	wt := placeWorktree(t, false)
	for _, own := range []string{"CLAUDE.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(wt, own), []byte("the repo's own layer\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := checkRulesReach(wt)
	if f.Status != statusFail {
		t.Fatalf("the worktree's own committed rules must not satisfy this check, got %s: %s", f.Status, f.Detail)
	}
}

// A carrier several levels up still reaches: inheritance is not depth-limited,
// and a check that only looked at the immediate parent would fail worktrees
// that are in fact covered.
func TestRulesReach_ACarrierSeveralLevelsUpStillReaches(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "CLAUDE.md"), []byte("workspace rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(base, "a", "b", "repo_issue2_slug")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if f := checkRulesReach(deep); f.Status != statusOK {
		t.Fatalf("want OK for a carrier several levels up, got %s: %s", f.Status, f.Detail)
	}
}

// The failure message has to say what to do, because the reader is an agent
// about to start work in the wrong place.
func TestRulesReach_TheFailureSaysWhatToDo(t *testing.T) {
	d := checkRulesReach(placeWorktree(t, false)).Detail
	for _, want := range []string{"Move the worktree", "render"} {
		if !strings.Contains(d, want) {
			t.Fatalf("the failure must name a remedy (%q missing): %q", want, d)
		}
	}
}
