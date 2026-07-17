package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestRunDoctor_PathBypass_ShimDirBehindSymlink_StillReportsManaged is a
// regression test for a real Copilot review finding on this PR:
// checkPathBypass used to compare filepath.Dir(exec.LookPath(...)) against
// shimDir with plain filepath.Abs, not symlink-evaluated canonicalization.
// On macOS, /tmp resolves through /private/tmp, so a shim directory that
// itself lives under a symlinked path (t.TempDir() on macOS is exactly
// this) compared against an OMCA_SHIM_DIR value spelled through the
// symlink would false-positive as a bypass. This constructs the shim dir
// through an explicit extra symlink hop (independent of whatever the test
// machine's own /tmp happens to be) and proves doctor still reports
// "managed", not a bypass.
func TestRunDoctor_PathBypass_ShimDirBehindSymlink_StillReportsManaged(t *testing.T) {
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
	realShimDir := shimDirPath(worktreeStateDirPath(stateRoot, wt.ID))

	// A second symlink hop, distinct from realShimDir's own literal path,
	// resolving to the exact same directory.
	symlinkedShimDir := filepath.Join(t.TempDir(), "shim-via-symlink")
	if err := os.Symlink(realShimDir, symlinkedShimDir); err != nil {
		t.Fatal(err)
	}

	// PATH is set through the symlinked spelling; OMCA_SHIM_DIR (what
	// checkPathBypass compares against) is set through the OTHER,
	// original spelling — exactly the "reaches the same directory through
	// a different symlink chain" case CleanAbs exists to handle.
	t.Setenv("PATH", symlinkedShimDir+string(os.PathListSeparator)+env.BinDir)
	t.Setenv("OMCA_SHIM_DIR", realShimDir)

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 0 {
		t.Fatalf("runDoctor = %d, want 0 (shim reached via a different symlink spelling must still count as managed); stdout:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "path-bypass:codex: codex resolves to the OMCA shim") {
		t.Errorf("stdout does not report codex as resolving to the shim despite the symlink hop:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "PATH bypass") {
		t.Errorf("stdout falsely reports a PATH bypass caused only by a symlink spelling difference:\n%s", stdout.String())
	}
}

// TestRunDoctor_GenerationPointerCorrupt_ReportsFail is a regression test
// for a real Copilot review finding on this PR: checkGenerationFreshness
// used to treat every CurrentGenerationDir error, not just "no pointer
// yet" (os.IsNotExist), as the same WARN-level "not managed yet" finding —
// masking real corruption (e.g. the "current" entry existing as something
// other than a readable symlink) as an expected, benign state. This
// replaces the "current" pointer with a plain regular file (Readlink on a
// non-symlink fails with a non-NotExist error) and proves doctor now
// reports FAIL with a corruption-specific message, not the ordinary
// not-yet-managed WARN.
func TestRunDoctor_GenerationPointerCorrupt_ReportsFail(t *testing.T) {
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
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	linkPath := filepath.Join(worktreeStateDir, "current", "codex")

	if err := os.Remove(linkPath); err != nil {
		t.Fatalf("removing the real 'current' symlink: %v", err)
	}
	if err := os.WriteFile(linkPath, []byte("not a symlink"), 0o644); err != nil {
		t.Fatalf("planting a corrupt (non-symlink) 'current' entry: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runDoctor(&stdout, &stderr)
	if code != 1 {
		t.Fatalf("runDoctor = %d, want 1 (corrupt pointer is a FAIL); stdout:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "generation:codex") || !strings.Contains(stdout.String(), "corrupt") {
		t.Errorf("stdout does not report the corrupt generation pointer as such:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "has no compiled generation yet") {
		t.Errorf("stdout downgraded real pointer corruption to the ordinary not-yet-managed WARN:\n%s", stdout.String())
	}
}

// TestRunDoctor_DirenvApproval_StatusTimesOut is a regression test for a
// real Copilot review finding on this PR: checkDirenvApproval used to
// discard `direnv status`'s own error entirely (`out, _ := cmd.Output()`),
// so a timeout or exec failure fell through to the generic "could not
// determine approval state" default case instead of being reported as
// what it actually was. This installs a fake "direnv" that sleeps well
// past a (test-shrunk) direnvStatusTimeout and proves the finding now
// names the timeout specifically.
//
// The shrunk timeout is deliberately generous (500ms, against a script that
// blocks indefinitely) rather than tight (an earlier version of this test
// used 50ms and was flaky on a loaded CI runner). The point under test is
// "is ctx.Err() checked at all," not "exactly how fast is the timeout" — a
// wide margin proves the same thing with no flake risk.
//
// The fake script busy-blocks using only the shell's own builtins
// (`while :; do :; done`), never an external `sleep` binary: this test's
// PATH (via setupManagedTestEnv) is deliberately a hermetic directory
// containing only the fake host binaries, with no /bin or /usr/bin on it,
// so a script that shelled out to `sleep` would fail immediately with
// "sleep: not found" (exit 127) instead of blocking — exactly the false
// failure an earlier version of this test hit, which looked identical to
// the regression it was meant to catch (the same fallback message) until
// traced down to this, not a real ctx.Err() problem.
func TestRunDoctor_DirenvApproval_StatusTimesOut(t *testing.T) {
	orig := direnvStatusTimeout
	direnvStatusTimeout = 500 * time.Millisecond
	t.Cleanup(func() { direnvStatusTimeout = orig })

	env := setupManagedTestEnv(t, false, false)
	if err := os.WriteFile(filepath.Join(env.WorktreeRoot, ".envrc"), []byte(`eval "$(omca env --shell bash)"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	direnvScript := "#!/bin/sh\nwhile :; do :; done\n"
	if err := os.WriteFile(filepath.Join(env.BinDir, "direnv"), []byte(direnvScript), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	_ = runDoctor(&stdout, &stderr)
	if !strings.Contains(stdout.String(), "did not complete within") {
		t.Errorf("stdout does not report the direnv status timeout specifically:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "could not determine direnv approval state") {
		t.Errorf("stdout fell back to the generic could-not-determine message instead of reporting the timeout:\n%s", stdout.String())
	}
}
