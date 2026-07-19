package main

import (
	"fmt"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wangzitian0/oh-my-code-agent/internal/tui"
)

// runTUI implements the bare `omca` command (docs/product/requirements.md
// §5.3: "The omca command opens the management TUI") — main()'s own
// no-subcommand branch calls this, never run() (see main.go's doc
// comment on why: run()'s own no-args behavior stays exactly what
// main_test.go already pins it to, usage/exit 2, for any caller that
// invokes run(nil, ...) directly).
//
// It builds this worktree's one immutable report.Artifact via the exact
// same buildArtifactForCLI pipeline every other read command (report,
// drift, explain, matrix, compare, diff) already uses, then hands it to
// internal/tui.NewModel and runs bubbletea's real interactive event loop
// against the real terminal (stdin/stdout) — the only place in this
// codebase internal/tui's pure Model is driven by a live terminal instead
// of by a test's direct View() call.
func runTUI(stdin io.Reader, stdout, stderr io.Writer) int {
	artifact, err := buildArtifactForCLI(stderr, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "omca: %v\n", err)
		return 1
	}

	program := tea.NewProgram(
		tui.NewModel(artifact),
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
	)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(stderr, "omca: tui: %v\n", err)
		return 1
	}
	return 0
}
