package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// This file's tests reuse testFixtureBinaries (built once by shim_test.go's
// TestMain) to exercise `omca run --mode isolated|native` as a real
// subprocess — required for the same reason cmd/omca/shim_test.go needs a
// subprocess: the success path calls internal/shim.ExecReplace
// (syscall.Exec), which replaces the calling process and never returns, so
// it is never safe to call in-process from `go test` itself.

// runOmcaSubprocess runs the built omca fixture binary with args, dir as
// its working directory, and environ as its complete environment (no
// inheritance from the test process — the same explicit-environment
// discipline this whole PR's production code follows). It waits up to 10s.
func runOmcaSubprocess(t *testing.T, dir string, args, environ []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(testFixtureBinaries.omca, args...)
	cmd.Dir = dir
	cmd.Env = environ
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting omca subprocess: %v", err)
	}
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			return stdoutBuf.String(), stderrBuf.String(), 0
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdoutBuf.String(), stderrBuf.String(), exitErr.ExitCode()
		}
		t.Fatalf("omca subprocess failed to run: %v\nstderr:\n%s", err, stderrBuf.String())
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("omca subprocess did not exit within 10s\nstdout:\n%s\nstderr:\n%s", stdoutBuf.String(), stderrBuf.String())
	}
	panic("unreachable")
}

// TestRunIsolated_EndToEnd_ExecsWithGenerationEnv is the `omca run --mode
// isolated` analogue of TestShim_EndToEnd_NonRecursionAndEnvInjection:
// issue #14 says this mode shares "the same generation-selection/compile
// logic as omca env" and execs "with the same syscall.Exec discipline as
// the shim." This proves both halves through one real subprocess: a
// generation actually gets compiled from scratch (no pre-existing state,
// unlike the shim tests which start from an already-compiled fixture), and
// the real binary that finally runs (fakehost) sees CODEX_HOME pointing
// into it.
//
// It also proves a real bug this PR's own review caught and fixed: earlier,
// `omca run <host>` (unlike `omca env`) never injected OMCA_STATE_DIR or
// OMCA_WORKTREE_ID into the exec'd environment, so a spawned `omca mcp
// serve` subprocess (per this PR's own MCP-registration wiring,
// internal/runtime/compile.go's hostConfigFiles) could never answer
// omca_status for a session launched this way — internal/mcp.ComputeStatus
// hard-fails without OMCA_STATE_DIR. fakehost's dumped environment is the
// only observable proof available to a real-subprocess test of what the
// exec'd process actually received.
func TestRunIsolated_EndToEnd_ExecsWithGenerationEnv(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("syscall.Exec-based `omca run` is macOS-first scope")
	}

	worktreeRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(worktreeRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	homeDir := t.TempDir()
	stateRoot := t.TempDir()
	restoreWritableTree(t, stateRoot) // a compiled generation lands read-only under here; see internal/runtime/readonly.go
	binDir := t.TempDir()
	if err := os.Symlink(testFixtureBinaries.fakeHost, filepath.Join(binDir, "codex")); err != nil {
		t.Fatal(err)
	}

	environ := []string{
		"HOME=" + homeDir,
		"PATH=" + binDir,
		"XDG_STATE_HOME=" + stateRoot,
	}
	stdout, stderr, code := runOmcaSubprocess(t, worktreeRoot, []string{"run", "codex", "--mode", "isolated"}, environ)
	if code != 0 {
		t.Fatalf("omca run --mode isolated codex = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "CODEX_HOME=") {
		t.Fatalf("fakehost's dumped environment did not contain CODEX_HOME; stdout:\n%s", stdout)
	}
	// The generation must have landed under stateRoot/omca (realStateRoot's
	// own XDG_STATE_HOME/omca convention), not anywhere ambient.
	if !strings.Contains(stdout, filepath.Join(stateRoot, "omca")) {
		t.Errorf("CODEX_HOME does not point inside the configured XDG_STATE_HOME (%s); stdout:\n%s", stateRoot, stdout)
	}
	if !strings.Contains(stdout, "OMCA_STATE_DIR=") {
		t.Errorf("fakehost's dumped environment did not contain OMCA_STATE_DIR -- a spawned `omca mcp serve` subprocess could never answer omca_status; stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "OMCA_WORKTREE_ID=") {
		t.Errorf("fakehost's dumped environment did not contain OMCA_WORKTREE_ID; stdout:\n%s", stdout)
	}
}

// dumpedEnvLine returns the value of the exact "key=..." line inside a
// fakehost environment dump (output), or "" plus false if no such line
// exists — a whole-line lookup, not strings.Contains/strings.TrimPrefix
// against the raw dump, so a longer variable that happens to contain key as
// a substring (e.g. "OMCA_REAL_HOME=" containing "HOME=") can never be
// mistaken for it.
func dumpedEnvLine(output, key string) (string, bool) {
	prefix := key + "="
	for _, line := range strings.Split(output, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return rest, true
		}
	}
	return "", false
}

// TestRunIsolated_EndToEnd_VirtualizesHome is this fix's own hard-requirement
// regression test: this project's entire reason to exist is that launching a
// coding-agent CLI through it loads ZERO unselected native MCP servers/
// Skills, and internal/context/host.go's codexNativeHomes/claudeNativeHomes
// both resolve a "HOME/.agents/skills" native home directly from HOME,
// independent of CODEX_HOME/CLAUDE_CONFIG_DIR -- so a real, unmanaged
// $HOME/.agents/skills still loads on every launch unless the exec'd
// process's own HOME is actually redirected away from the caller's real
// home. This plants a real skill file at a scratch $HOME/.agents/skills/
// some-skill/SKILL.md, runs the real `omca run codex --mode isolated` exec
// path (never the real codex/claude binary -- testFixtureBinaries.fakeHost
// stands in, dumping the environment it actually received), and asserts:
//
//  1. the exec'd process's own HOME is NOT the scratch HOME (it is the
//     compiled generation's virtual-home directory instead), and
//  2. the scratch HOME's planted skill file is therefore unreachable via
//     $HOME/.agents/skills from the exec'd process's own perspective --
//     computed by joining the exec'd process's OWN dumped HOME value (not
//     the scratch HOME the test planted the file under) with
//     ".agents/skills/some-skill/SKILL.md" and confirming nothing exists
//     there.
//
// Before this fix, `overrides` in both internal/shim/exec.go's Plan.Exec and
// this function's own production counterpart (runIsolated, run.go) only ever
// set CODEX_HOME/CLAUDE_CONFIG_DIR -- HOME itself passed through completely
// unmodified, so this test's step 1 assertion fails against the pre-fix
// code (HOME dumped by fakehost equals the scratch HOME verbatim) and step 2
// would have found the real planted file reachable. See this file's git
// history / the PR description for the fail-before/pass-after proof this
// test was written to satisfy.
func TestRunIsolated_EndToEnd_VirtualizesHome(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("syscall.Exec-based `omca run` is macOS-first scope")
	}

	worktreeRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(worktreeRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Plant a real, scratch-HOME native skill exactly where
	// codexNativeHomes (internal/context/host.go) says a real installation
	// would resolve "HOME/.agents/skills" from -- never the real machine's
	// actual $HOME (this repo's own hard rule for any test touching
	// HOME/CODEX_HOME/CLAUDE_CONFIG_DIR: always t.TempDir(), never the real
	// paths of the machine actually running this suite).
	scratchHome := t.TempDir()
	skillFile := filepath.Join(scratchHome, ".agents", "skills", "some-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, []byte("# some-skill\n\nunmanaged, native, unselected.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stateRoot := t.TempDir()
	restoreWritableTree(t, stateRoot) // a compiled generation lands read-only under here; see internal/runtime/readonly.go
	binDir := t.TempDir()
	if err := os.Symlink(testFixtureBinaries.fakeHost, filepath.Join(binDir, "codex")); err != nil {
		t.Fatal(err)
	}

	environ := []string{
		"HOME=" + scratchHome,
		"PATH=" + binDir,
		"XDG_STATE_HOME=" + stateRoot,
	}
	stdout, stderr, code := runOmcaSubprocess(t, worktreeRoot, []string{"run", "codex", "--mode", "isolated"}, environ)
	if code != 0 {
		t.Fatalf("omca run --mode isolated codex = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	execdHome, ok := dumpedEnvLine(stdout, "HOME")
	if !ok {
		t.Fatalf("fakehost's dumped environment did not contain a HOME line at all; stdout:\n%s", stdout)
	}

	// Assertion 1: the exec'd process's own HOME must not be the scratch
	// HOME this test planted the skill file under -- this is the literal
	// bug: pre-fix, HOME passed straight through untouched.
	if execdHome == scratchHome {
		t.Fatalf("exec'd process's HOME (%s) is the scratch HOME verbatim; HOME was never virtualized -- the real, unmanaged %s is still reachable through it", execdHome, skillFile)
	}
	if !strings.Contains(execdHome, filepath.Join(stateRoot, "omca")) {
		t.Errorf("exec'd process's HOME (%s) does not point inside the compiled generation under the configured XDG_STATE_HOME (%s); stdout:\n%s", execdHome, stateRoot, stdout)
	}

	// Assertion 2: from the exec'd process's OWN perspective (its own
	// reported HOME, not the scratch HOME this test happens to know about),
	// the planted skill file must be unreachable -- codexNativeHomes'
	// "HOME/.agents/skills" resolution, replayed here against the exec'd
	// HOME, must find nothing.
	unreachablePath := filepath.Join(execdHome, ".agents", "skills", "some-skill", "SKILL.md")
	if _, statErr := os.Stat(unreachablePath); statErr == nil {
		t.Errorf("the planted skill file is reachable via the exec'd process's own HOME/.agents/skills (%s); isolation was not applied", unreachablePath)
	} else if !os.IsNotExist(statErr) {
		t.Errorf("unexpected error statting %s: %v", unreachablePath, statErr)
	}

	// The real, scratch-HOME skill file must still physically exist
	// untouched (this test never claims to delete or hide real state, only
	// that the exec'd process cannot see it through its own HOME).
	if _, err := os.Stat(skillFile); err != nil {
		t.Fatalf("planted skill file %s no longer exists: %v", skillFile, err)
	}

	// docs/architecture/runtime.md §7.1's other required export:
	// OMCA_REAL_HOME must carry the caller's real (here: scratch) HOME
	// forward, so the launched process can still recover it.
	wantRealHomeLine := "OMCA_REAL_HOME=" + scratchHome
	if !strings.Contains(stdout, wantRealHomeLine) {
		t.Errorf("fakehost's dumped environment did not contain %q; stdout:\n%s", wantRealHomeLine, stdout)
	}
}

// TestRunNative_EndToEnd_NoGenerationEnvInjected proves `--mode native`
// really does exec with the caller's ambient environment completely
// unmodified — no CODEX_HOME injected — after printing its unmanaged
// warning to stderr, never stdout.
func TestRunNative_EndToEnd_NoGenerationEnvInjected(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("syscall.Exec-based `omca run` is macOS-first scope")
	}

	worktreeRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(worktreeRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(testFixtureBinaries.fakeHost, filepath.Join(binDir, "codex")); err != nil {
		t.Fatal(err)
	}

	environ := []string{
		"HOME=" + t.TempDir(),
		"PATH=" + binDir,
	}
	stdout, stderr, code := runOmcaSubprocess(t, worktreeRoot, []string{"run", "codex", "--mode", "native"}, environ)
	if code != 0 {
		t.Fatalf("omca run --mode native codex = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if strings.Contains(stdout, "CODEX_HOME=") {
		t.Errorf("native mode injected CODEX_HOME into the exec'd process's environment, want it fully unmodified; stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "UNMANAGED") {
		t.Errorf("stderr does not contain the unmanaged warning: %q", stderr)
	}
}
