package main

import (
	stdcontext "context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// runRollback implements `omca rollback <host>` (docs/architecture/
// runtime.md §11's `omca rollback <generation-id>` synopsis, narrowed here
// to "roll back to the parent of whatever is current" -- the concrete M2 AC
// this PR closes, "rollback restores the parent generation"; restoring an
// arbitrary named generation, not just the immediate parent, is a natural
// follow-up but not what any AC this PR closes requires). Restores host's
// parent generation as "current" (runtime.Rollback) and ledgers the
// restoration.
func runRollback(stdout, stderr io.Writer, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: omca rollback <codex|claude>")
		return 2
	}
	host, err := normalizeHostArg(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "omca: rollback: %v\n", err)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "omca: rollback: %v\n", err)
		return 1
	}
	wt, err := hostcontext.DetectWorktree(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "omca: rollback: %v\n", err)
		return 1
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: rollback: %v\n", err)
		return 1
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)
	generationsRoot := filepath.Join(worktreeStateDir, "generations")

	realEnv := hostcontext.RealEnvironment()
	detectEnv := envWithFilteredPath(realEnv, shimDir)
	hd, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: rollback: %v\n", err)
		return 1
	}

	result, err := runtime.Rollback(worktreeStateDir, generationsRoot, host, hd, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "omca: rollback: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "omca: rollback: %s: restored %s, superseding %s, at %s\n", result.Host, result.RestoredGenerationID, result.SupersededGenerationID, result.RolledBackAt)
	return 0
}
