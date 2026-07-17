package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// omcaStateDirName is the fixed leaf directory name this project's real
// state lives under, per docs/architecture/README.md §8's layout
// (`~/.local/state/omca/`). There is no realCacheRoot counterpart here:
// M1 has no derived/re-creatable cache content yet (Knowledge Pack/
// observation caching, `~/.cache/omca/` in the same §8 layout, is later
// milestone scope) -- adding an unused XDG_CACHE_HOME resolver ahead of any
// caller would be exactly the kind of speculative code this project's own
// quality bar (and golangci-lint's unused check) rejects.
const omcaStateDirName = "omca"

// realStateRoot resolves this machine's real XDG state root for OMCA:
// $XDG_STATE_HOME/omca if XDG_STATE_HOME is set (the XDG Base Directory
// spec's override), else $HOME/.local/state/omca. This is the one place in
// this command that reads XDG_STATE_HOME/HOME — every core package
// (internal/runtime, internal/shim, internal/observe, internal/context)
// takes its paths as explicit parameters instead, exactly like
// internal/runtime.Bootstrap's own outputDir (see internal/runtime/
// bootstrap.go's doc comment: "outputDir is injected by the caller, never
// derived from ~/.local/state/omca... internally"). Only cmd/omca's command
// layer (this file) is allowed to call this, per this PR's own scope
// instructions.
func realStateRoot() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		if !filepath.IsAbs(xdg) {
			return "", fmt.Errorf("omca: XDG_STATE_HOME %q is not an absolute path", xdg)
		}
		return filepath.Join(xdg, omcaStateDirName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("omca: resolving real state root: %w", err)
	}
	return filepath.Join(home, ".local", "state", omcaStateDirName), nil
}

// worktreeStateDirPath computes one worktree's state directory under
// stateRoot: stateRoot/worktrees/<dir-safe worktree ID>
// (docs/architecture/README.md §8's `worktrees/<worktree-id>/`).
// runtime.DirSafeID turns the worktree's colon-delimited logical ID into a
// plain directory name, the same sanitization internal/runtime.
// EnsureGeneration applies to a generation ID.
func worktreeStateDirPath(stateRoot, worktreeID string) string {
	return filepath.Join(stateRoot, "worktrees", runtime.DirSafeID(worktreeID))
}

// shimDirPath is the fixed location, within one worktree's state directory,
// that omca env/omca run install the codex/claude PATH shim entries into
// and export as OMCA_SHIM_DIR. Kept as its own tiny named function (rather
// than an inline filepath.Join at each call site) so every caller agrees on
// exactly the same path.
func shimDirPath(worktreeStateDir string) string {
	return filepath.Join(worktreeStateDir, "shims")
}

// resolveOMCABinaryPath resolves the absolute, symlink-evaluated path to
// the currently running omca binary. Its one job today is supplying the
// symlink target for the shim directory's own "omca" entry (env.go's
// installShims — the MCP registration's command points at that stable
// shimDir/omca path, via omcaCommandPath, never at this function's result
// directly).
//
// This comment previously (incorrectly) described this function as also
// feeding runtime.BootstrapRequest.OMCABinaryPath and therefore
// GenerationID/checkStaleGeneration — that described an earlier design,
// abandoned mid-development because os.Executable() resolves to a
// different path on every `go run` invocation (see
// internal/runtime/request.go's OMCABinaryPath doc comment and this PR's
// own description for the full history). The actual current design is the
// opposite: OMCABinaryPath is always the STABLE omcaCommandPath(shimDir)
// value (env.go's runEnv, run.go's runIsolated), which this function has no
// part in, and GenerationID deliberately excludes OMCABinaryPath entirely
// (TestGenerationID_StableAcrossOMCABinaryPathChange) — so this function's
// own possibly-per-invocation result can never make a generation look
// spuriously stale (Copilot review finding on this PR: the old comment
// text was misleading future readers about which function does what).
func resolveOMCABinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("omca: resolveOMCABinaryPath: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}
