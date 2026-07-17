package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
)

// TestRunDoctor_UnmanagedSession_NoOMCAEnvVars is issue #14's "doctor
// distinguishes managed vs unmanaged sessions" AC, the unmanaged half: a
// shell with no OMCA_* variables set is reported as such. This fixture's
// PATH points straight at the fake binaries (never through a shim), so
// checkPathBypass also, correctly, reports both hosts as bypassed — a
// genuine FAIL this test does not itself exercise but must not mask; see
// TestRunDoctor_PathBypass_* for that check in isolation.
func TestRunDoctor_UnmanagedSession_NoOMCAEnvVars(t *testing.T) {
	setupManagedTestEnv(t, true, true)

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 1 {
		t.Fatalf("runDoctor = %d, want 1 (PATH bypass is also present in this fixture); stdout:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "UNMANAGED") {
		t.Errorf("stdout does not report the session as unmanaged:\n%s", stdout.String())
	}
}

// TestRunDoctor_ManagedSession_ReportsManaged is the managed half: once
// OMCA_CONTEXT_ID/OMCA_WORKTREE_ID are set to values a real `omca env`
// eval would have exported for this exact worktree, doctor reports the
// session as managed. As above, PATH is not routed through the shim here,
// so PATH bypass is independently, correctly still a FAIL.
func TestRunDoctor_ManagedSession_ReportsManaged(t *testing.T) {
	setupManagedTestEnv(t, true, true)

	var envOut, envErr bytes.Buffer
	if code := runEnv(&envOut, &envErr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, envErr.String())
	}
	applyExportsToEnv(t, envOut.String())

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 1 {
		t.Fatalf("runDoctor = %d, want 1 (PATH bypass is also present in this fixture); stdout:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "managed:") {
		t.Errorf("stdout does not report the session as managed:\n%s", stdout.String())
	}
}

// applyExportsToEnv parses `omca env`'s own stdout (export KEY='VALUE'
// lines) and applies OMCA_CONTEXT_ID/OMCA_WORKTREE_ID to the real test
// process environment via t.Setenv, standing in for what a shell's `eval`
// would have done — the minimal parse this test needs, not a general shell
// evaluator.
func applyExportsToEnv(t *testing.T, exports string) {
	t.Helper()
	for _, line := range strings.Split(exports, "\n") {
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := line[:eq]
		val := strings.Trim(line[eq+1:], "'")
		if key == "OMCA_CONTEXT_ID" || key == "OMCA_WORKTREE_ID" {
			t.Setenv(key, val)
		}
	}
}

// TestRunDoctor_PathBypass_NativeBinaryBypassesShim is issue #14's "PATH
// bypass" AC: with a fake codex reachable on the real ambient PATH but no
// shim installed there (PATH points straight at a native-binary
// directory), doctor must report a FAIL — and, since this is the one
// doctor finding this PR treats as severe enough to affect exit status,
// the command exits 1.
func TestRunDoctor_PathBypass_NativeBinaryBypassesShim(t *testing.T) {
	setupManagedTestEnv(t, true, false)

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 1 {
		t.Fatalf("runDoctor = %d, want 1 (PATH bypass is a FAIL); stdout:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "PATH bypass") {
		t.Errorf("stdout does not report a PATH bypass:\n%s", stdout.String())
	}
}

// TestRunDoctor_PathBypass_ShimFirstOnPath_ReportsManaged proves the
// counterpart: once the shim directory is genuinely first on PATH (the
// state a real direnv-approved shell reaches after `eval "$(omca env)"`),
// doctor reports codex as managed, not bypassed.
func TestRunDoctor_PathBypass_ShimFirstOnPath_ReportsManaged(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	var envOut, envErr bytes.Buffer
	if code := runEnv(&envOut, &envErr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, envErr.String())
	}
	wt, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	shimDir := shimDirPath(worktreeStateDirPath(stateRoot, wt.ID))
	// Simulate the shell state after `eval "$(omca env)"`: shim directory
	// prepended to PATH.
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+env.BinDir)

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 0 {
		t.Fatalf("runDoctor = %d, want 0; stdout:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "path-bypass:codex: codex resolves to the OMCA shim") {
		t.Errorf("stdout does not report codex as resolving to the shim:\n%s", stdout.String())
	}
}

// TestRunDoctor_NoGenerationYet proves a worktree that has never had `omca
// env`/`omca run` executed against it reports a clear "no compiled
// generation yet" finding for each installed host, rather than crashing or
// silently omitting the check.
func TestRunDoctor_NoGenerationYet(t *testing.T) {
	setupManagedTestEnv(t, true, false)

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 1 {
		// PATH is not routed through the shim in this fixture, so PATH
		// bypass is independently, correctly a FAIL too.
		t.Fatalf("runDoctor = %d, want 1; stdout:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "has no compiled generation yet") {
		t.Errorf("stdout does not report the missing generation:\n%s", stdout.String())
	}
}

// TestRunDoctor_StaleGeneration_AfterWorktreeChange proves issue #14's
// "stale generation" AC: compiling a generation, then changing the
// worktree's observable input (adding a repository Instructions file) and
// running doctor again, reports the generation stale rather than falsely
// green.
func TestRunDoctor_StaleGeneration_AfterWorktreeChange(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	var envOut, envErr bytes.Buffer
	if code := runEnv(&envOut, &envErr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, envErr.String())
	}

	if err := os.WriteFile(filepath.Join(env.WorktreeRoot, "AGENTS.md"), []byte("# new instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 1 {
		// Stale generation is itself only a WARN, but PATH is not routed
		// through the shim in this fixture, so PATH bypass is
		// independently, correctly a FAIL too.
		t.Fatalf("runDoctor = %d, want 1; stdout:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "stale:") {
		t.Errorf("stdout does not report a stale generation after the worktree changed:\n%s", stdout.String())
	}
}

// TestRunDoctor_FreshGeneration_NoStaleWarning is TestRunDoctor_
// StaleGeneration_AfterWorktreeChange's negative control: immediately
// after `omca env`, with nothing changed, doctor must NOT report the
// generation stale — proving the stale check is not vacuously true.
func TestRunDoctor_FreshGeneration_NoStaleWarning(t *testing.T) {
	setupManagedTestEnv(t, true, false)

	var envOut, envErr bytes.Buffer
	if code := runEnv(&envOut, &envErr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, envErr.String())
	}

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 1 {
		// PATH is not routed through the shim in this fixture, so PATH
		// bypass is independently, correctly a FAIL too.
		t.Fatalf("runDoctor = %d, want 1; stdout:\n%s", code, stdout.String())
	}
	if strings.Contains(stdout.String(), "stale:") {
		t.Errorf("stdout reports stale immediately after `omca env` with nothing changed:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "matches fresh inputs") {
		t.Errorf("stdout does not confirm the generation matches fresh inputs:\n%s", stdout.String())
	}
}

// TestRunDoctor_BinaryMoved proves issue #14's "host binary moved since
// qualification" AC: after `omca env` records codex's binary path, moving
// that binary to a different PATH directory and running doctor again
// reports the move.
func TestRunDoctor_BinaryMoved(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	var envOut, envErr bytes.Buffer
	if code := runEnv(&envOut, &envErr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, envErr.String())
	}

	newBinDir := t.TempDir()
	newPath := writeFakeVersionBinary(t, newBinDir, "codex", "codex-cli 0.144.5\n")
	if err := os.Remove(filepath.Join(env.BinDir, "codex")); err != nil {
		t.Fatal(err)
	}
	_ = newPath
	t.Setenv("PATH", newBinDir)

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 1 {
		// Binary-moved is itself only a WARN, but PATH is not routed
		// through the shim in this fixture, so PATH bypass is
		// independently, correctly a FAIL too.
		t.Fatalf("runDoctor = %d, want 1; stdout:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "binary moved since its generation was recorded") {
		t.Errorf("stdout does not report the moved binary:\n%s", stdout.String())
	}
}

// TestRunDoctor_DirenvApproval_NoEnvrc proves the "missing direnv
// approval" AC's first-order case: no .envrc at all.
func TestRunDoctor_DirenvApproval_NoEnvrc(t *testing.T) {
	setupManagedTestEnv(t, false, false)

	var stdout, stderr bytes.Buffer
	_ = runDoctor(&stdout, &stderr)
	if !strings.Contains(stdout.String(), "no .envrc found") {
		t.Errorf("stdout does not report a missing .envrc:\n%s", stdout.String())
	}
}

// TestRunDoctor_DirenvApproval_NotInstalled proves direnv-not-installed is
// reported as its own distinct finding, never conflated with "not
// approved" — the test's PATH is a fully synthetic temp directory
// containing no direnv binary, deterministically regardless of whether the
// host machine running this test suite happens to have direnv installed.
func TestRunDoctor_DirenvApproval_NotInstalled(t *testing.T) {
	env := setupManagedTestEnv(t, false, false)
	if err := os.WriteFile(filepath.Join(env.WorktreeRoot, ".envrc"), []byte(`eval "$(omca env --shell bash)"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	_ = runDoctor(&stdout, &stderr)
	if !strings.Contains(stdout.String(), "direnv is not installed") {
		t.Errorf("stdout does not report direnv as not installed:\n%s", stdout.String())
	}
}

// TestRunDoctor_DirenvApproval_EnvrcMissingOmcaEnv proves a .envrc that
// exists but never invokes `omca env` is flagged too, not treated as
// equivalent to a properly wired one.
func TestRunDoctor_DirenvApproval_EnvrcMissingOmcaEnv(t *testing.T) {
	env := setupManagedTestEnv(t, false, false)
	if err := os.WriteFile(filepath.Join(env.WorktreeRoot, ".envrc"), []byte("export FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	_ = runDoctor(&stdout, &stderr)
	if !strings.Contains(stdout.String(), "does not appear to invoke") {
		t.Errorf("stdout does not flag the .envrc for missing `omca env`:\n%s", stdout.String())
	}
}
