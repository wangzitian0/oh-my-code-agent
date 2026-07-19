package main

import (
	"bytes"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
)

// TestRunTUI_BuildFailure_ReturnsNonZeroAndReportsError exercises runTUI's
// one hard-failure path: buildArtifactForCLI itself failing (here, because
// the working directory is not inside any worktree — hostcontext.
// DetectWorktree finds no ".git" walking up from a bare t.TempDir()) must
// be reported to stderr and return a non-zero exit code, never attempt to
// launch the real bubbletea event loop over a half-built Artifact.
//
// The success path (building a real Artifact and driving bubbletea's
// actual terminal event loop) is intentionally not exercised here:
// internal/tui's own Model is already exhaustively tested directly
// (Init/Update/View, and all four views against a committed golden
// fixture) without a terminal, which is exactly why bubbletea was chosen
// (internal/tui/doc.go's "Library choice"); runTUI itself is a thin, ~15
// line wiring function whose only remaining untested seam is
// tea.Program.Run() driving a real terminal loop — the same kind of
// "real interactive/exec path, not unit tested" seam this package's own
// runShim doc comment already documents as expected (its Exec success
// path is "unreachable" from a unit test for the identical reason).
func TestRunTUI_BuildFailure_ReturnsNonZeroAndReportsError(t *testing.T) {
	chdirT(t, t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runTUI(strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("runTUI in a non-worktree directory = 0, want non-zero")
	}
	if stderr.Len() == 0 {
		t.Error("expected an explanatory message on stderr")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on a build failure", stdout.String())
	}
}

// TestActionContextForTUI_ResolvesRealPaths is issue #35's own wiring test:
// runTUI's actionContextForTUI must resolve the exact same worktree/
// state-dir/shim-dir/config-root quadruplet every other action-performing
// command in this package resolves for itself (worktreeStateDirPath(
// realStateRoot(), wt.ID), shimDirPath, realConfigRoot), and attach the real
// ambient environment (PATH already filtered of the shim dir, exactly like
// every other detect-calling command) -- proving internal/tui's Model, once
// handed this ActionContext, is driving the SAME real worktree state every
// CLI command in this package already operates on, not a second,
// independently-resolved notion of it.
func TestActionContextForTUI_ResolvesRealPaths(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	var stderr bytes.Buffer
	ctx, ok := actionContextForTUI(&stderr)
	if !ok {
		t.Fatalf("actionContextForTUI: ok = false, stderr:\n%s", stderr.String())
	}

	wt, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	if ctx.Worktree.ID != wt.ID || ctx.Worktree.Root != wt.Root {
		t.Errorf("Worktree = %+v, want %+v", ctx.Worktree, wt)
	}

	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	wantStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	if ctx.WorktreeStateDir != wantStateDir {
		t.Errorf("WorktreeStateDir = %q, want %q", ctx.WorktreeStateDir, wantStateDir)
	}
	if ctx.ShimDir != shimDirPath(wantStateDir) {
		t.Errorf("ShimDir = %q, want %q", ctx.ShimDir, shimDirPath(wantStateDir))
	}

	configRoot, err := realConfigRoot()
	if err != nil {
		t.Fatalf("realConfigRoot: %v", err)
	}
	if ctx.ConfigRoot != configRoot {
		t.Errorf("ConfigRoot = %q, want %q", ctx.ConfigRoot, configRoot)
	}

	if ctx.Env.Get("HOME") != env.HomeDir {
		t.Errorf("Env HOME = %q, want %q", ctx.Env.Get("HOME"), env.HomeDir)
	}
}

// TestActionContextForTUI_NotAWorktree_DisablesActionsWithoutFailing proves
// actionContextForTUI degrades honestly (ok=false, a warning on stderr)
// rather than panicking or returning a half-built ActionContext when this
// process is not inside any worktree at all -- runTUI's own doc comment:
// a resolution failure here must never prevent the TUI from at least
// opening in its original PR-30 read-only mode.
func TestActionContextForTUI_NotAWorktree_DisablesActionsWithoutFailing(t *testing.T) {
	chdirT(t, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var stderr bytes.Buffer
	ctx, ok := actionContextForTUI(&stderr)
	if ok {
		t.Fatalf("actionContextForTUI outside any worktree: ok = true, want false; ctx=%+v", ctx)
	}
	if stderr.Len() == 0 {
		t.Error("expected a warning on stderr")
	}
	if ctx.WorktreeStateDir != "" || ctx.Worktree.Root != "" {
		t.Errorf("ctx = %+v, want the zero value (tui.ActionContext.enabled() reports false for it)", ctx)
	}
}
