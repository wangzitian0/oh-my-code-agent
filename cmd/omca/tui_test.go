package main

import (
	"bytes"
	"strings"
	"testing"
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
