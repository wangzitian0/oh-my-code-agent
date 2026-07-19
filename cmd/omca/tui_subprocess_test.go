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

// TestMainBareInvocation_OpensTUI_QuitsOnQ is the real-subprocess proof for
// main.go's own doc comment claim: a bare `omca` invocation (no subcommand
// at all) opens the management TUI (docs/product/requirements.md §5.3)
// rather than printing usage. It reuses testFixtureBinaries.omca, the same
// real compiled binary shim_test.go's TestMain already builds once for
// this whole package's subprocess-based integration tests (that file's own
// doc comment explains why a real subprocess is required here too: only
// one actually proves main()'s own os.Args-length dispatch and a real
// bubbletea event loop compose the way the unit tests — internal/tui's
// own Model tests, and this package's TestRunTUI_BuildFailure_* — assume).
//
// Sending a single "q" byte on stdin must make the TUI quit and the
// process exit cleanly within the timeout: if main() ever regressed to
// printing usage/exiting 2 for no-args (rather than reaching runTUI), or
// if the TUI wiring failed to read stdin/handle the quit key at all, this
// test fails either on a non-zero exit code or a timeout.
func TestMainBareInvocation_OpensTUI_QuitsOnQ(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("syscall.Exec-based shim mode is macOS-first scope; this test shares TestMain's fixture binary with the shim tests")
	}

	worktreeRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(worktreeRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	environ := []string{
		"HOME=" + t.TempDir(),
		"PATH=" + t.TempDir(), // empty: no codex/claude on PATH, so no host is "installed"
		"XDG_STATE_HOME=" + t.TempDir(),
	}

	cmd := exec.Command(testFixtureBinaries.omca)
	cmd.Dir = worktreeRoot
	cmd.Env = environ
	cmd.Stdin = strings.NewReader("q")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting bare omca: %v", err)
	}
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bare `omca` (quit via 'q') exited with error: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("bare `omca` did not exit within 10s after sending 'q' on stdin (possible TUI wiring regression); stderr so far:\n%s", stderr.String())
	}
}
