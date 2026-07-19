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
	// forward, so the launched process can still recover it. dumpedEnvLine,
	// not strings.Contains: a substring check on the whole dump could false-
	// positive if some other env var's value happens to contain scratchHome
	// as a substring (e.g. a var whose own value is itself HOME-prefixed).
	execdRealHome, ok := dumpedEnvLine(stdout, "OMCA_REAL_HOME")
	if !ok {
		t.Fatalf("fakehost's dumped environment did not contain an OMCA_REAL_HOME line at all; stdout:\n%s", stdout)
	}
	if execdRealHome != scratchHome {
		t.Errorf("OMCA_REAL_HOME = %q, want %q", execdRealHome, scratchHome)
	}
}

// TestRunIsolated_StaleGenerationMissingVirtualHome_FailsClosed proves the
// Copilot-review fix: EnsureGeneration treats any directory with a valid
// manifest.json as a cache hit, without re-checking that the rest of the
// on-disk directory set still matches what the current compiler promises
// to have created. A generation compiled before HOME virtualization
// existed (simulated here by deleting an already-compiled generation's
// virtual-home directory and reusing the same worktree/state dir, so the
// second `omca run` call is guaranteed to hit EnsureGeneration's cache
// path rather than recompiling) must make `omca run` fail loudly rather
// than silently launching with HOME pointed at a path that does not
// exist -- which would reopen the exact isolation gap this generation-
// compiler change exists to close.
func TestRunIsolated_StaleGenerationMissingVirtualHome_FailsClosed(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("syscall.Exec-based `omca run` is macOS-first scope")
	}

	worktreeRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(worktreeRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	scratchHome := t.TempDir()
	stateRoot := t.TempDir()
	restoreWritableTree(t, stateRoot)
	binDir := t.TempDir()
	if err := os.Symlink(testFixtureBinaries.fakeHost, filepath.Join(binDir, "codex")); err != nil {
		t.Fatal(err)
	}
	environ := []string{
		"HOME=" + scratchHome,
		"PATH=" + binDir,
		"XDG_STATE_HOME=" + stateRoot,
	}

	// First call: a genuinely fresh compile, virtual-home included. Reuses
	// the exact same environ/worktreeRoot on the second call below so
	// EnsureGeneration's content-address computation is identical and it
	// returns the (about to be tampered with) cached directory instead of
	// recompiling.
	stdout, stderr, code := runOmcaSubprocess(t, worktreeRoot, []string{"run", "codex", "--mode", "isolated"}, environ)
	if code != 0 {
		t.Fatalf("first omca run (fresh compile) = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	execdHome, ok := dumpedEnvLine(stdout, "HOME")
	if !ok {
		t.Fatalf("fakehost's dumped environment did not contain a HOME line at all; stdout:\n%s", stdout)
	}
	// The compiled generation tree lands read-only (internal/runtime/
	// readonly.go); restore write permission before tampering with it.
	// restoreWritableTree only registers a t.Cleanup hook for end-of-test,
	// so call the underlying restore function directly to get an
	// immediate, mid-test effect.
	restoreWritableSkippingSymlinks(stateRoot)
	if err := os.RemoveAll(execdHome); err != nil {
		t.Fatalf("removing the compiled generation's virtual-home directory (%s): %v", execdHome, err)
	}

	// Second call: same environ, same worktree -- EnsureGeneration must find
	// the same content-addressed generation directory (manifest.json is
	// untouched) and treat it as a cache hit, exactly the scenario a real
	// pre-fix-compiled generation would present.
	_, stderr2, code2 := runOmcaSubprocess(t, worktreeRoot, []string{"run", "codex", "--mode", "isolated"}, environ)
	if code2 == 0 {
		t.Fatal("second omca run (virtual-home missing) exited 0, want a non-zero fail-closed exit -- HOME would have been set to a nonexistent path")
	}
	if !strings.Contains(stderr2, "virtual-home") {
		t.Errorf("stderr does not mention the missing virtual-home directory, want an actionable message; stderr:\n%s", stderr2)
	}
}

// writeFakeASDFShim writes a synthetic asdf shim script at
// <asdfDataDir>/shims/<name> whose body faithfully replicates the one real
// behavior issue #69's bug hinges on -- needing a real, resolvable HOME to
// find asdf's own state before it can dispatch -- without depending on any
// real asdf installation or invoking the real `asdf` binary anywhere in
// this test: it checks for a marker file this test plants only under a
// scratch "real" HOME (never a compiled generation's virtual-home
// directory, which is always a fresh, empty, compiler-created directory,
// exactly like a real asdf-unaware virtual home would be) and exits 126 --
// the exact bare, unhelpful exit code the issue reports -- if that marker
// is not reachable via $HOME. pluginVersions mirrors asdf's own real
// shim-generation metadata format (one "# asdf-plugin: <plugin> <version>"
// comment line per candidate), verified read-only against a real, installed
// asdf 0.18.0 during this fix's investigation (see internal/shim/asdf.go's
// doc comment) -- one line means unambiguous, two or more means asdf's own
// .tool-versions precedence would be required to disambiguate.
func writeFakeASDFShim(t *testing.T, asdfDataDir, name string, pluginVersions [][2]string, dispatchTarget string) string {
	t.Helper()
	shimsDir := filepath.Join(asdfDataDir, "shims")
	if err := os.MkdirAll(shimsDir, 0o755); err != nil {
		t.Fatalf("writeFakeASDFShim: MkdirAll: %v", err)
	}
	body := "#!/usr/bin/env bash\n"
	for _, pv := range pluginVersions {
		body += "# asdf-plugin: " + pv[0] + " " + pv[1] + "\n"
	}
	body += `if [ ! -f "$HOME/.asdf-marker" ]; then
  exit 126
fi
exec "` + dispatchTarget + `" "$@"
`
	path := filepath.Join(shimsDir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writeFakeASDFShim: WriteFile: %v", err)
	}
	return path
}

// writeFakeASDFInstalledBinary symlinks
// <asdfDataDir>/installs/<plugin>/<version>/bin/<name> to
// testFixtureBinaries.fakeHost -- the concrete, per-version real binary
// ResolveASDFShimTarget (internal/shim/asdf.go) must resolve an
// unambiguous asdf shim to.
func writeFakeASDFInstalledBinary(t *testing.T, asdfDataDir, plugin, version, name string) string {
	t.Helper()
	binDir := filepath.Join(asdfDataDir, "installs", plugin, version, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("writeFakeASDFInstalledBinary: MkdirAll: %v", err)
	}
	path := filepath.Join(binDir, name)
	if err := os.Symlink(testFixtureBinaries.fakeHost, path); err != nil {
		t.Fatalf("writeFakeASDFInstalledBinary: Symlink: %v", err)
	}
	return path
}

// TestRunIsolated_EndToEnd_ResolvesPastASDFShim is issue #69's core
// regression proof: `omca run codex --mode isolated` against a host binary
// that resolves via PATH to an asdf-managed shim script must not bare-fail
// with exit 126 the moment isolated mode virtualizes HOME. Before this fix,
// runIsolated (run.go) always exec'd hd.BinaryPath directly -- here, the
// synthetic asdf shim script itself -- with HOME overridden to the compiled
// generation's virtual-home directory; the shim's own "$HOME/.asdf-marker"
// check (writeFakeASDFShim's doc comment explains why this faithfully
// stands in for asdf's real HOME-dependent dispatch) then fails, because a
// freshly compiled virtual-home directory never has that marker, producing
// exactly the bare "exit 126, no output" issue #69 reports. After this fix,
// shim.IsASDFShim/ResolveASDFShimTarget resolve the exec target past the
// shim script entirely -- straight to the concrete real binary
// (testFixtureBinaries.fakeHost, standing in for the real per-version
// asdf-managed binary) -- so the marker check is never reached and the
// launch succeeds under the fully virtualized HOME the rest of isolated
// mode requires.
//
// This test is written to FAIL against the pre-fix runIsolated (asserted by
// temporarily reverting the shim.IsASDFShim/ResolveASDFShimTarget call in
// run.go during this fix's own development) and PASS once the fix resolves
// past the shim, per this project's fail-before/pass-after verification
// discipline.
func TestRunIsolated_EndToEnd_ResolvesPastASDFShim(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("syscall.Exec-based `omca run` is macOS-first scope")
	}

	worktreeRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(worktreeRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// realHome stands in for the developer's actual $HOME: it is where a
	// real asdf shim would find real asdf-derived state (~/.tool-versions,
	// ~/.asdf/...) to dispatch successfully. The marker file here is this
	// test's own stand-in for that real, reachable state -- never anything
	// under the real machine's own ~/.asdf (this repo's hard rule: tests
	// touching HOME/asdf-shaped state always use t.TempDir(), never real
	// paths).
	realHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(realHome, ".asdf-marker"), []byte("stand-in for real asdf state\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	realBinary := writeFakeASDFInstalledBinary(t, asdfDataDir, "fakeplugin", "1.0.0", "codex")
	writeFakeASDFShim(t, asdfDataDir, "codex", [][2]string{{"fakeplugin", "1.0.0"}}, realBinary)

	stateRoot := t.TempDir()
	restoreWritableTree(t, stateRoot)

	environ := []string{
		"HOME=" + realHome,
		// "/usr/bin:/bin" is appended purely so the shim script's own
		// "#!/usr/bin/env bash" shebang resolves (a real system bash/env,
		// not anything asdf- or test-specific) -- without it, a pre-fix
		// exec of the shim script under a PATH containing only the shims
		// directory fails one step earlier, on the shebang lookup itself
		// (exit 127), rather than on the marker check this test means to
		// exercise (exit 126, matching issue #69's own real-world number).
		"PATH=" + filepath.Join(asdfDataDir, "shims") + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		"XDG_STATE_HOME=" + stateRoot,
	}
	stdout, stderr, code := runOmcaSubprocess(t, worktreeRoot, []string{"run", "codex", "--mode", "isolated"}, environ)
	if code != 0 {
		t.Fatalf("omca run --mode isolated codex (asdf-shimmed) = %d, want 0 (the pre-fix bug reproduces here as exit 126 with no output)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "CODEX_HOME=") {
		t.Fatalf("fakehost's dumped environment did not contain CODEX_HOME -- the resolved-past-the-shim real binary was never reached; stdout:\n%s", stdout)
	}
	execdHome, ok := dumpedEnvLine(stdout, "HOME")
	if !ok {
		t.Fatalf("fakehost's dumped environment did not contain a HOME line; stdout:\n%s", stdout)
	}
	if execdHome == realHome {
		t.Fatalf("exec'd process's HOME (%s) is the real scratch HOME verbatim; HOME was never virtualized for the resolved asdf target", execdHome)
	}
}

// TestRunIsolated_EndToEnd_AmbiguousASDFShim_FailsWithActionableError proves
// the fallback half of this fix: when the asdf shim names two or more
// candidate plugin versions (ResolveASDFShimTarget's own deliberate refusal
// to replicate asdf's .tool-versions precedence, internal/shim/asdf.go),
// `omca run --mode isolated` must fail with a clear, actionable, non-zero
// exit -- naming the problem and a workaround -- rather than either
// guessing at a version or propagating a bare, confusing exec failure.
func TestRunIsolated_EndToEnd_AmbiguousASDFShim_FailsWithActionableError(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("syscall.Exec-based `omca run` is macOS-first scope")
	}

	worktreeRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(worktreeRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	realHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(realHome, ".asdf-marker"), []byte("stand-in for real asdf state\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	realBinaryA := writeFakeASDFInstalledBinary(t, asdfDataDir, "fakeplugin", "1.0.0", "codex")
	writeFakeASDFInstalledBinary(t, asdfDataDir, "fakeplugin", "2.0.0", "codex")
	writeFakeASDFShim(t, asdfDataDir, "codex", [][2]string{
		{"fakeplugin", "1.0.0"},
		{"fakeplugin", "2.0.0"},
	}, realBinaryA)

	stateRoot := t.TempDir()
	restoreWritableTree(t, stateRoot)

	environ := []string{
		"HOME=" + realHome,
		"PATH=" + filepath.Join(asdfDataDir, "shims") + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		"XDG_STATE_HOME=" + stateRoot,
	}
	_, stderr, code := runOmcaSubprocess(t, worktreeRoot, []string{"run", "codex", "--mode", "isolated"}, environ)
	if code == 0 {
		t.Fatal("omca run --mode isolated codex against an ambiguous asdf shim exited 0, want a non-zero, actionable failure")
	}
	if !strings.Contains(stderr, "asdf") {
		t.Errorf("stderr does not mention asdf, want an actionable message naming the problem; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "native") {
		t.Errorf("stderr does not mention the --mode native workaround; stderr:\n%s", stderr)
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
