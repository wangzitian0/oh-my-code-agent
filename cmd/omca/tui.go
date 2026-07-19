package main

import (
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
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
//
// Issue #35: this is also the ONE place in this binary that resolves
// worktree/state-dir/shim-dir/config-root and attaches them to the Model as
// an internal/tui.ActionContext (tui.WithActionContext) — internal/tui
// itself never reads $HOME/$XDG_STATE_HOME/$XDG_CONFIG_HOME (see
// realConfigRoot's own doc comment: only this command layer may), so every
// directory the TUI's action layer (stage/activate/rollback) needs is
// resolved here exactly once, the same way every other command in this
// package resolves them for itself.
func runTUI(stdin io.Reader, stdout, stderr io.Writer) int {
	now := time.Now()
	artifact, err := buildArtifactForCLI(stderr, now)
	if err != nil {
		fmt.Fprintf(stderr, "omca: %v\n", err)
		return 1
	}

	model := tui.NewModel(artifact)
	if ctx, ok := actionContextForTUI(stderr); ok {
		model = model.WithActionContext(ctx)
	}

	program := tea.NewProgram(
		model,
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
	)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(stderr, "omca: tui: %v\n", err)
		return 1
	}
	return 0
}

// actionContextForTUI resolves the tui.ActionContext runTUI attaches to its
// Model: the same cwd -> worktree -> state-dir -> shim-dir -> config-root
// resolution sequence every other action-performing command (activate.go's
// runActivate, mcp.go's compileFuncForMCP) already runs, plus the real
// ambient environment (hostcontext.RealEnvironment(), used both for host
// detection and for issue #35's restart-required env-var signal). ok is
// false — with a warning on stderr, never a hard failure — for any
// resolution error: a worktree-detection or state-root failure here must
// not prevent the TUI from at least opening in its original, PR-30
// read-only mode (buildArtifactForCLI above already succeeded, so there is
// real content to show); it only means this session's action keys stay
// inert (tui.ActionContext.enabled reports false for the zero value).
func actionContextForTUI(stderr io.Writer) (tui.ActionContext, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "omca: warning: resolving cwd, TUI actions disabled: %v\n", err)
		return tui.ActionContext{}, false
	}
	wt, err := hostcontext.DetectWorktree(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "omca: warning: detecting worktree, TUI actions disabled: %v\n", err)
		return tui.ActionContext{}, false
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: warning: resolving state root, TUI actions disabled: %v\n", err)
		return tui.ActionContext{}, false
	}
	configRoot, err := realConfigRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: warning: resolving config root, TUI actions disabled: %v\n", err)
		return tui.ActionContext{}, false
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)
	// envWithFilteredPath (pathfilter.go), exactly like every other
	// detect-calling command in this package (activate.go, mcp.go,
	// reportbuild.go): strips shimDir from PATH before detection, so
	// resolving codex/claude sees the same real, unmanaged binary a doctor
	// PATH-bypass check would, never the OMCA shim reflecting back at
	// itself. Only PATH is rewritten -- OMCA_RUN_ID/CODEX_HOME/
	// CLAUDE_CONFIG_DIR (issue #35's restart-required signal) pass through
	// unchanged.
	detectEnv := envWithFilteredPath(hostcontext.RealEnvironment(), shimDir)

	return tui.ActionContext{
		Worktree:         wt,
		WorktreeStateDir: worktreeStateDir,
		ShimDir:          shimDir,
		ConfigRoot:       configRoot,
		Env:              detectEnv,
	}, true
}
