package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file's helpers are shared by env_test.go, run_test.go, and
// doctor_test.go: every one of those commands reads real ambient state
// (cwd, HOME, PATH, XDG_STATE_HOME) that this PR's own quality bar requires
// tests to fully control rather than depend on the real machine — "never
// touch this machine's real ~/.claude/~/.codex/XDG state dirs from any
// test." No helper here ever invokes a real codex/claude binary: every
// fake binary these tests build only ever answers `--version`, mirroring
// internal/context/host_test.go's writeFakeBinary, the established pattern
// for this exact safety boundary.

// writeFakeVersionBinary writes a hermetic POSIX shell script standing in
// for a real host binary, answering only `--version`. This is a local,
// cmd/omca-scoped copy of internal/context/host_test.go's unexported
// writeFakeBinary helper (that package does not export test helpers across
// package boundaries) with the same safety rationale: these tests must
// never depend on or invoke a real codex/claude installation.
func writeFakeVersionBinary(t *testing.T, dir, name, versionOutput string) string {
	t.Helper()
	if strings.Contains(versionOutput, "'") {
		t.Fatalf("writeFakeVersionBinary: versionOutput %q must not contain a single quote", versionOutput)
	}
	path := filepath.Join(dir, name)
	trimmed := strings.TrimSuffix(versionOutput, "\n")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"printf '%s\\n' '" + trimmed + "'\n" +
		"fi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// newFakeWorktree creates a temp directory containing a `.git` marker —
// the minimal fixture internal/context.DetectWorktree needs, matching
// internal/context/worktree_test.go's own established pattern.
func newFakeWorktree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// chdirT changes the test process's working directory to dir for the
// duration of the test, restoring the original directory in t.Cleanup.
// This is process-global state, so tests using it must not run in
// parallel with each other or with any other test in this package that
// depends on cwd — none of cmd/omca's existing tests call t.Parallel(), and
// this package's own tests here follow that same convention deliberately.
func chdirT(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}

// managedTestEnv is a fully hermetic, sandboxed process-environment
// fixture: fake HOME, a fake PATH containing (optionally) fake codex/claude
// binaries, and a fake XDG_STATE_HOME, all set via t.Setenv (which mutates
// and restores the real process environment around the test) so
// hostcontext.RealEnvironment() — which every command under test calls —
// observes exactly this synthetic state and nothing from the real machine.
type managedTestEnv struct {
	WorktreeRoot string
	HomeDir      string
	BinDir       string
	StateRoot    string
}

// setupManagedTestEnv builds a managedTestEnv and chdirs into its worktree
// root. installCodex/installClaude control whether a fake, --version-only
// codex/claude binary is placed on the synthetic PATH — a caller testing
// "host not installed" behavior passes false for one or both.
func setupManagedTestEnv(t *testing.T, installCodex, installClaude bool) managedTestEnv {
	t.Helper()
	env := managedTestEnv{
		WorktreeRoot: newFakeWorktree(t),
		HomeDir:      t.TempDir(),
		BinDir:       t.TempDir(),
		StateRoot:    t.TempDir(),
	}
	if installCodex {
		writeFakeVersionBinary(t, env.BinDir, "codex", "codex-cli 0.144.5\n")
	}
	if installClaude {
		writeFakeVersionBinary(t, env.BinDir, "claude", "2.1.211 (Claude Code)\n")
	}

	t.Setenv("HOME", env.HomeDir)
	t.Setenv("PATH", env.BinDir)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("XDG_STATE_HOME", env.StateRoot)
	t.Setenv("OMCA_SHIM_DIR", "")
	t.Setenv("OMCA_STATE_DIR", "")
	t.Setenv("OMCA_CONTEXT_ID", "")
	t.Setenv("OMCA_WORKTREE_ID", "")
	t.Setenv("OMCA_RUN_ID", "")

	// A compiled generation tree is read-only on disk
	// (internal/runtime/readonly.go), which would otherwise make
	// t.TempDir()'s own cleanup fail to remove env.StateRoot. Registered
	// after StateRoot's own t.TempDir() call, so — t.Cleanup runs
	// LIFO — this restores write permission before TempDir's removal runs.
	t.Cleanup(func() { restoreWritableSkippingSymlinks(env.StateRoot) })

	chdirT(t, env.WorktreeRoot)
	return env
}

// restoreWritableSkippingSymlinks is restoreWritableTree's careful sibling:
// it never calls os.Chmod on a symlink entry. This distinction is not
// cosmetic — installShims (env.go) plants shimDir/codex and shimDir/claude
// as symlinks to the running omca binary (os.Executable()), and
// env.StateRoot's tree (once a test has called runEnv) contains exactly
// those symlinks. os.Chmod on Unix follows symlinks: calling it on a
// symlink path changes the TARGET file's mode, not the symlink's. In a
// `go test` binary, that target is the shared, single, currently-running
// test executable — chmod'ing shimDir/codex to 0o644 during one test's
// cleanup silently strips the executable bit off the test binary itself
// for the remainder of the whole `go test` process, breaking every
// subsequent test's exec.LookPath-based PATH resolution in ways that look
// like a completely unrelated flaky failure. This was caught by exactly
// that symptom during this PR's own development — see the fix commit.
func restoreWritableSkippingSymlinks(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort cleanup only
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // never chmod through a symlink -- see doc comment above
		}
		if d.IsDir() {
			_ = os.Chmod(path, 0o755)
		} else {
			_ = os.Chmod(path, 0o644)
		}
		return nil
	})
}
