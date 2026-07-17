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
