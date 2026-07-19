package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
)

// This file's helpers back every scripted interaction test in
// model_actions_test.go and actions_test.go: a fully hermetic, sandboxed
// ActionContext (every directory a fresh t.TempDir(), Env a literal
// hostcontext.Environment{Vars: [...]} this file constructs by hand) that
// exercises the REAL internal/runtime/internal/profiles machinery
// end-to-end, never a mocked-out activation path -- matching every other
// real-write-path test in this codebase. Env is never RealEnvironment() and
// nothing here ever calls t.Setenv on the real process environment, so
// these tests cannot read or write this machine's real
// HOME/CODEX_HOME/CLAUDE_CONFIG_DIR under any circumstance.

// mustWriteFileForActionTest writes content to path, creating parent
// directories as needed -- this file's own tiny fixture helper, matching
// cmd/omca/activate_test.go's identically-named-in-spirit
// mustWriteFileForActivateTest (that helper lives in package main and
// cannot be imported here).
func mustWriteFileForActionTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mustWriteFileForActionTest: mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("mustWriteFileForActionTest: write %s: %v", path, err)
	}
}

// writeFakeCodexBinary writes a hermetic POSIX shell script standing in for
// a real `codex` binary, answering only `--version` -- a local copy of
// internal/context/host_test.go's writeFakeBinary (unexported, cannot be
// imported across the package boundary) and cmd/omca/testenv_test.go's
// identical writeFakeVersionBinary, with the same safety rationale: this
// package's tests must never depend on or invoke a real codex/claude
// installation.
func writeFakeCodexBinary(t *testing.T, dir, versionOutput string) {
	t.Helper()
	path := filepath.Join(dir, "codex")
	trimmed := strings.TrimSuffix(versionOutput, "\n")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"printf '%s\\n' '" + trimmed + "'\n" +
		"fi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// restoreWritableForActionTest undoes runtime.Compile's read-only
// generation trees (internal/runtime/readonly.go) so t.TempDir()'s own
// cleanup can remove root -- the identical pattern fixture_test.go's own
// restoreGenerationDirWritable and cmd/omca/testenv_test.go's
// restoreWritableSkippingSymlinks both establish for the same reason;
// duplicated here since neither crosses this package's boundary.
func restoreWritableForActionTest(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // best-effort cleanup only
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil // never chmod through a symlink
			}
			if d.IsDir() {
				_ = os.Chmod(path, 0o755)
			} else {
				_ = os.Chmod(path, 0o644)
			}
			return nil
		})
	})
}

// bindingYAMLFor is the minimal Binding document that selects
// company:example for wtRoot -- without one, profiles.Compose resolves
// zero Profiles regardless of what exists under profiles/ (the same
// requirement cmd/omca/activate_test.go's buildPendingFixtureForActivate
// documents on its own identical fixture).
func bindingYAMLFor(wtRoot string) string {
	return "apiVersion: omca.dev/v1alpha1\nkind: Binding\nmetadata:\n  id: binding:example\nspec:\n  match:\n    repository: " + wtRoot + "\n    paths: [\"**\"]\n  profiles:\n    - company:example\n"
}

// setupActionTestEnv builds a fully sandboxed ActionContext: a fresh
// worktree/state/shim directory triple, a fake --version-only codex binary
// on a synthetic PATH, and, when profileYAML is non-empty, that content
// written to $ConfigRoot/profiles/company/example.yaml plus a matching
// Binding selecting it for this worktree -- the same fixture shape
// cmd/omca/activate_test.go's buildPendingFixtureForActivate establishes
// for the CLI activation path, reproduced here because this package cannot
// import that unexported test helper across the package-main boundary
// either.
func setupActionTestEnv(t *testing.T, profileYAML string) ActionContext {
	t.Helper()

	homeDir := t.TempDir()
	worktreeRoot := t.TempDir()
	configRoot := filepath.Join(homeDir, ".config", "omca")
	stateDir := t.TempDir()
	shimDir := t.TempDir()
	binDir := t.TempDir()

	writeFakeCodexBinary(t, binDir, "codex-cli 0.144.5")

	wt := hostcontext.Worktree{ID: "worktree:sha256:tui-action-test", Root: worktreeRoot}

	if profileYAML != "" {
		mustWriteFileForActionTest(t, filepath.Join(configRoot, "profiles", "company", "example.yaml"), profileYAML)
		mustWriteFileForActionTest(t, filepath.Join(configRoot, "bindings", "example.yaml"), bindingYAMLFor(wt.Root))
	}

	restoreWritableForActionTest(t, stateDir)

	return ActionContext{
		Worktree:         wt,
		WorktreeStateDir: stateDir,
		ShimDir:          shimDir,
		ConfigRoot:       configRoot,
		Env: hostcontext.Environment{Vars: []string{
			"HOME=" + homeDir,
			"PATH=" + binDir,
		}},
	}
}

// rewriteProfile overwrites the Profile fixture setupActionTestEnv wrote,
// simulating real desired-state evolution between two staging/activation
// cycles (mirroring cmd/omca/activate_test.go's own identical
// two-sequential-profileYAML-edits pattern).
func rewriteProfile(t *testing.T, ctx ActionContext, profileYAML string) {
	t.Helper()
	mustWriteFileForActionTest(t, filepath.Join(ctx.ConfigRoot, "profiles", "company", "example.yaml"), profileYAML)
}
