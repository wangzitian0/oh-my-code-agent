package main

import (
	"strings"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/shim"
)

// envWithFilteredPath returns a copy of env with PATH rewritten to exclude
// shimDir (via internal/shim.FilterOutDir), leaving every other entry
// unchanged. `omca env` and `omca run --mode isolated` both need to detect
// the REAL host binary — never a stale or freshly-installed copy of the
// OMCA shim itself — before internal/context.DetectHost ever runs; without
// this, a contributor re-running `omca env` from inside an already-managed
// shell (OMCA_SHIM_DIR already first on PATH, exactly the steady-state
// condition) would have DetectHost resolve "codex" back to the shim, and
// everything downstream (the recorded BinaryPath, the probed Version) would
// silently describe the shim instead of the real installation.
//
// shimDir is always the freshly, deterministically computed path
// (shimDirPath(worktreeStateDir)) — never read back from an OMCA_SHIM_DIR
// environment variable that may not be set yet (first-ever `omca env` run
// in a worktree) or may be stale.
func envWithFilteredPath(env hostcontext.Environment, shimDir string) hostcontext.Environment {
	filtered := shim.FilterOutDir(env.Get("PATH"), shimDir)
	out := make([]string, 0, len(env.Vars)+1)
	for _, kv := range env.Vars {
		if strings.HasPrefix(kv, "PATH=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "PATH="+filtered)
	return hostcontext.Environment{Vars: out}
}
