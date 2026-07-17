package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// This file holds the two subprocess-based integration tests issue #14's
// acceptance criteria describe literally:
//
//   - "The shim never recursively invokes itself, even with the shim dir
//     first in PATH (test)."
//   - "exec semantics: signals and exit codes pass through (SIGINT test)."
//
// Both require an actual separate OS process built from this repository's
// real cmd/omca main() — internal/shim/plan_test.go and resolve_test.go
// already prove the underlying decision logic (ResolveReal's non-recursion,
// Build's env resolution) in-process and exhaustively; what only a real
// subprocess can prove is that main()'s own dispatch, syscall.Exec, and
// real OS signal delivery actually compose the way the unit tests assume.
// syscall.Exec replaces the calling process's image and never returns on
// success, so it is never safe to call from inside the `go test` process
// itself — hence building and exec'ing a real, separate omca binary here.

// testFixtureBinaries are built once, in TestMain, and reused by every test
// in this file: the real omca binary (so shimDir/codex, a symlink to it,
// behaves exactly like a production shim install — env.go's installShims
// does the identical symlink-to-the-running-binary thing), plus the two
// fixture "host" binaries under testdata/ (excluded from every real
// build/vet/lint pass; see testdata/fakehost's doc comment).
var testFixtureBinaries struct {
	omca       string
	fakeHost   string
	fakeSigint string
}

func TestMain(m *testing.M) {
	if goruntime.GOOS == "windows" {
		// syscall.Exec-based shim mode is macOS-first scope (this PR's own
		// instructions); skip building/running these fixtures on windows
		// rather than gold-plating a fallback nothing else in this module
		// supports either.
		os.Exit(m.Run())
	}

	tmp, err := os.MkdirTemp("", "omca-shim-fixtures")
	if err != nil {
		fmt.Fprintln(os.Stderr, "omca-shim-fixtures: MkdirTemp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	pkgDir := packageDir()

	testFixtureBinaries.omca = filepath.Join(tmp, "omca")
	if err := buildFixtureBinary(pkgDir, ".", testFixtureBinaries.omca); err != nil {
		fmt.Fprintln(os.Stderr, "omca-shim-fixtures: building omca:", err)
		os.Exit(1)
	}
	testFixtureBinaries.fakeHost = filepath.Join(tmp, "fakehost")
	if err := buildFixtureBinary(pkgDir, "./testdata/fakehost", testFixtureBinaries.fakeHost); err != nil {
		fmt.Fprintln(os.Stderr, "omca-shim-fixtures: building fakehost:", err)
		os.Exit(1)
	}
	testFixtureBinaries.fakeSigint = filepath.Join(tmp, "fakesigint")
	if err := buildFixtureBinary(pkgDir, "./testdata/fakesigint", testFixtureBinaries.fakeSigint); err != nil {
		fmt.Fprintln(os.Stderr, "omca-shim-fixtures: building fakesigint:", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// packageDir locates cmd/omca's own source directory via runtime.Caller,
// the same technique internal/observe/helpers_test.go's repoFixturesDir and
// internal/qualify/fixtures_test.go use, so `go build` below resolves
// correctly regardless of the test binary's working directory.
func packageDir() string {
	_, file, _, _ := goruntime.Caller(0)
	return filepath.Dir(file)
}

func buildFixtureBinary(dir, pkg, out string) error {
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, output)
	}
	return nil
}

// buildFixtureCurrentGeneration compiles a real codex bootstrap generation
// (internal/runtime.Bootstrap, via EnsureGeneration) into a fresh worktree
// state directory and points a "current" pointer at it (SetCurrentGeneration)
// — exactly the sequence `omca env` performs — so the shim tests below
// exercise internal/shim.Build against the real on-disk shape that command
// produces, not a hand-rolled shortcut.
func buildFixtureCurrentGeneration(t *testing.T) (worktreeStateDir, generationDir string) {
	t.Helper()
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "project")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	det := hostcontext.HostDetection{
		Host:       "codex",
		Surface:    "cli",
		Version:    "0.144.5",
		Installed:  true,
		BinaryPath: filepath.Join(root, "bin", "codex"),
	}
	wt := hostcontext.Worktree{ID: "worktree:sha256:" + strings.Repeat("ab", 32), Root: worktreeRoot}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	req := runtime.BootstrapRequest{Detection: det, Worktree: wt, Now: now}

	worktreeStateDir = t.TempDir()
	gen, outputDir, err := runtime.EnsureGeneration(req, filepath.Join(worktreeStateDir, "generations"))
	if err != nil {
		t.Fatalf("EnsureGeneration: %v", err)
	}
	restoreWritableTree(t, outputDir)
	if err := runtime.SetCurrentGeneration(worktreeStateDir, "codex", outputDir, gen, det, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}
	return worktreeStateDir, outputDir
}

// restoreWritableTree chmods a compiled (and therefore read-only,
// internal/runtime/readonly.go) generation tree back to writable before
// t.TempDir()'s own cleanup tries to remove it. Mirrors internal/runtime/
// helpers_test.go's identical helper, but — like testenv_test.go's
// restoreWritableSkippingSymlinks, which this delegates to — never chmods
// through a symlink; see that function's doc comment for why that
// distinction matters.
func restoreWritableTree(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() { restoreWritableSkippingSymlinks(root) })
}

// installFixtureShim symlinks shimDir/codex to the built omca fixture
// binary, exactly what env.go's installShims does in production.
func installFixtureShim(t *testing.T, shimDir string) {
	t.Helper()
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(testFixtureBinaries.omca, filepath.Join(shimDir, "codex")); err != nil {
		t.Fatal(err)
	}
}

// TestShim_EndToEnd_NonRecursionAndEnvInjection is issue #14's literal,
// paired AC test: "build a fake 'real' binary, put the shim dir FIRST in
// PATH ahead of it (exactly the production condition), invoke the shim,
// and assert it reaches the fake real binary exactly once rather than
// looping/erroring/re-invoking itself," together with "verify with a fake
// host binary that dumps its env: assert the invoked fake binary's dumped
// environment actually contains the expected CODEX_HOME... pointing into
// the real compiled generation directory."
//
// If shim.ResolveReal's shim-directory exclusion were ever broken (e.g.
// reverted to a bare exec.LookPath against the ambient PATH), this test
// fails two different ways depending on the exact regression: either the
// invocation count in markerFile is 0 (the shim recursed into itself,
// which — since the shim it found is *also* this same omca binary in shim
// mode — would try to Build/Exec again, and again find OMCA_STATE_DIR/
// current pointing at the same generation, recursing until some other
// limit intervenes) or the process simply never terminates within the
// test's timeout.
func TestShim_EndToEnd_NonRecursionAndEnvInjection(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("syscall.Exec-based shim mode is macOS-first scope")
	}

	worktreeStateDir, generationDir := buildFixtureCurrentGeneration(t)

	shimDir := t.TempDir()
	installFixtureShim(t, shimDir)

	realDir := t.TempDir()
	if err := os.Symlink(testFixtureBinaries.fakeHost, filepath.Join(realDir, "codex")); err != nil {
		t.Fatal(err)
	}

	markerFile := filepath.Join(t.TempDir(), "invocations.log")

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + realDir,
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + worktreeStateDir,
		"FAKEHOST_MARKER=" + markerFile,
	}

	cmd := exec.Command(filepath.Join(shimDir, "codex"))
	cmd.Env = environ
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting shim: %v", err)
	}
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shim invocation failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("shim invocation did not exit within 10s (possible recursion); stderr so far:\n%s", stderr.String())
	}

	wantHomeDir := filepath.Join(generationDir, "hosts", "codex", "cli", "codex-home")
	wantLine := "CODEX_HOME=" + wantHomeDir
	if !strings.Contains(stdout.String(), wantLine) {
		t.Errorf("fakehost's dumped environment did not contain %q; stdout:\n%s", wantLine, stdout.String())
	}

	markerData, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("reading invocation marker: %v", err)
	}
	invocations := strings.Count(string(markerData), "invoked")
	if invocations != 1 {
		t.Errorf("fakehost was invoked %d times, want exactly 1 (non-recursion): marker file contents: %q", invocations, string(markerData))
	}
}

// TestShim_SIGINT_ExitCodePassthrough is issue #14's literal exec-semantics
// AC test: "invoke the shim wrapping a fake binary that blocks until it
// receives SIGINT then exits with a specific code, send SIGINT to the shim
// process, and assert the shim process itself (not a child) exits with
// that code — proving syscall.Exec actually replaced the process image
// rather than merely forwarding." cmd.Process here names the OS process
// that was originally the shim; sending it SIGINT and observing the fixed
// exit code from fakesigint's own os.Exit(42) is only possible if
// syscall.Exec truly replaced that process's image in place (same PID,
// new program) — os/exec.Cmd.Run-style fork+wait would leave the shim a
// separate parent process that received the signal instead and would have
// to forward it, which this project's shim deliberately never does.
func TestShim_SIGINT_ExitCodePassthrough(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("syscall.Exec-based shim mode is macOS-first scope")
	}

	worktreeStateDir, _ := buildFixtureCurrentGeneration(t)

	shimDir := t.TempDir()
	installFixtureShim(t, shimDir)

	realDir := t.TempDir()
	if err := os.Symlink(testFixtureBinaries.fakeSigint, filepath.Join(realDir, "codex")); err != nil {
		t.Fatal(err)
	}

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + realDir,
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + worktreeStateDir,
	}

	cmd := exec.Command(filepath.Join(shimDir, "codex"))
	cmd.Env = environ
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting shim: %v", err)
	}

	reader := bufio.NewReader(stdoutPipe)
	readyLine := make(chan string, 1)
	readyErr := make(chan error, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			readyErr <- err
			return
		}
		readyLine <- line
	}()

	select {
	case line := <-readyLine:
		if strings.TrimSpace(line) != "READY" {
			t.Fatalf("first line of output = %q, want %q", line, "READY\n")
		}
	case err := <-readyErr:
		t.Fatalf("reading READY line: %v\nstderr:\n%s", err, stderr.String())
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("fakesigint never printed READY within 10s; stderr:\n%s", stderr.String())
	}

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT: %v", err)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("Wait() error = %v (%T), want *exec.ExitError with exit code 42; stderr:\n%s", err, err, stderr.String())
		}
		if exitErr.ExitCode() != 42 {
			t.Errorf("exit code = %d, want 42 (fakesigint's own os.Exit(42), reached only if syscall.Exec truly replaced the shim process's image); stderr:\n%s", exitErr.ExitCode(), stderr.String())
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("process did not exit within 10s after SIGINT")
	}
}
