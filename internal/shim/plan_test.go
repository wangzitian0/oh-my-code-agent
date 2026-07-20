package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// buildFixtureGeneration compiles a real, minimal codex bootstrap
// generation via internal/runtime.Bootstrap (never a hand-rolled directory
// tree) and points a "current" pointer at it via SetCurrentGeneration —
// exactly the sequence `omca env`/`omca run` performs, so plan_test.go
// exercises Build against the real on-disk shape those commands produce,
// not a shortcut approximation of it.
func buildFixtureGeneration(t *testing.T, worktreeStateDir string) (domain.Generation, string) {
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
	wt := hostcontext.Worktree{ID: "worktree:sha256:" + fixtureHex(root), Root: worktreeRoot}
	req := runtime.BootstrapRequest{
		Detection: det,
		Worktree:  wt,
		Now:       time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}
	gen, outputDir, err := runtime.EnsureGeneration(req, filepath.Join(worktreeStateDir, "generations"))
	if err != nil {
		t.Fatalf("EnsureGeneration: %v", err)
	}
	restoreWritable(t, outputDir)
	if err := runtime.SetCurrentGeneration(worktreeStateDir, "codex", outputDir, gen, det, req.Now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}
	return gen, outputDir
}

// restoreWritable chmods every file and directory under root back to a
// writable mode before t.TempDir()'s own cleanup tries to remove it —
// otherwise removal itself fails, since runtime.Bootstrap deliberately
// leaves a compiled generation tree read-only on disk (internal/runtime/
// readonly.go). Mirrors internal/runtime/helpers_test.go's identical
// helper, duplicated here rather than exported across a package boundary
// for a test-only concern.
func restoreWritable(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // best-effort cleanup, never fail the test here
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

// fixtureHex is a tiny, non-cryptographic stand-in good enough to make a
// Worktree.ID look plausible for this package's own tests; internal/runtime
// itself does not validate the digest shape of Worktree.ID.
func fixtureHex(seed string) string {
	sum := 0
	for _, c := range seed {
		sum = sum*31 + int(c)
	}
	if sum < 0 {
		sum = -sum
	}
	hex := "0123456789abcdef"
	out := make([]byte, 64)
	for i := range out {
		out[i] = hex[(sum+i)%16]
	}
	return string(out)
}

// TestBuild_ResolvesRealBinaryAndInjectsGenerationEnv is issue #14's other
// literal AC: "assert the invoked fake binary's dumped environment
// actually contains the expected CODEX_HOME ... pointing into the real
// compiled generation directory." Build is the pure half of that pipeline —
// it must resolve NativeHomeDir to exactly
// <generationDir>/hosts/codex/cli/codex-home, the real directory
// runtime.Bootstrap wrote. cmd/omca/shim_test.go separately proves the
// injected env actually reaches a real exec'd process.
func TestBuild_ResolvesRealBinaryAndInjectsGenerationEnv(t *testing.T) {
	stateDir := t.TempDir()
	gen, outputDir := buildFixtureGeneration(t, stateDir)

	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")
	realDir := t.TempDir()
	wantReal := writeFakeExecutable(t, realDir, "codex")
	realHome := t.TempDir()

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + realDir,
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + stateDir,
		"HOME=" + realHome,
	}

	plan, err := Build("codex", environ)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Host != "codex" {
		t.Errorf("Host = %q, want %q", plan.Host, "codex")
	}
	if plan.RealBinaryPath != wantReal {
		t.Errorf("RealBinaryPath = %q, want %q", plan.RealBinaryPath, wantReal)
	}
	if plan.NativeHomeEnvVar != "CODEX_HOME" {
		t.Errorf("NativeHomeEnvVar = %q, want CODEX_HOME", plan.NativeHomeEnvVar)
	}
	// NativeHomeDir must be the writable, worktree-scoped mutable-state
	// directory (runtime.MutableNativeHomeDir), never outputDir's own
	// read-only codex-home -- a real host launched against the latter fails
	// outright the moment it tries to write its own runtime state (e.g.
	// Codex's own CODEX_HOME/state_5.sqlite: "unable to open database
	// file").
	wantHomeDir := filepath.Join(stateDir, "state", "hosts", "codex", "cli", "codex-home")
	if plan.NativeHomeDir != wantHomeDir {
		t.Errorf("NativeHomeDir = %q, want %q", plan.NativeHomeDir, wantHomeDir)
	}
	if info, statErr := os.Stat(plan.NativeHomeDir); statErr != nil || info.Mode().Perm()&0o200 == 0 {
		t.Errorf("NativeHomeDir %s is not writable (stat err: %v, mode: %v) -- the exact class of bug this test guards against", plan.NativeHomeDir, statErr, info)
	}
	wantConfig := filepath.Join(outputDir, "hosts", "codex", "cli", "codex-home", "config.toml")
	wantContent, err := os.ReadFile(wantConfig)
	if err != nil {
		t.Fatalf("reading generation's own config.toml: %v", err)
	}
	gotContent, err := os.ReadFile(filepath.Join(plan.NativeHomeDir, "config.toml"))
	if err != nil {
		t.Fatalf("NativeHomeDir was not synced with the generation's compiled config.toml: %v", err)
	}
	if string(gotContent) != string(wantContent) {
		t.Errorf("synced config.toml content = %q, want %q", gotContent, wantContent)
	}
	if plan.GenerationID != gen.Metadata.ID {
		t.Errorf("GenerationID = %q, want %q", plan.GenerationID, gen.Metadata.ID)
	}
	wantVirtualHomeDir := filepath.Join(outputDir, "hosts", "codex", "cli", runtime.VirtualHomeDirName)
	if plan.VirtualHomeDir != wantVirtualHomeDir {
		t.Errorf("VirtualHomeDir = %q, want %q", plan.VirtualHomeDir, wantVirtualHomeDir)
	}
	if plan.RealHomeDir != realHome {
		t.Errorf("RealHomeDir = %q, want %q", plan.RealHomeDir, realHome)
	}
}

// TestBuild_PreservesHostStateAcrossRelaunch is this fix's own
// worktree-shared-state regression proof: a second Build call against the
// same worktree (simulating a second launch -- e.g. a codex/claude session
// relaunched after a generation recompile) must never wipe state a previous
// launch's host process already wrote into NativeHomeDir (Codex's own
// state_5.sqlite, session history, auth.json, ...). Only the OMCA-authored
// config files a generation compiles are ever refreshed
// (runtime.SyncMutableNativeHome).
func TestBuild_PreservesHostStateAcrossRelaunch(t *testing.T) {
	stateDir := t.TempDir()
	buildFixtureGeneration(t, stateDir)

	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")
	realDir := t.TempDir()
	writeFakeExecutable(t, realDir, "codex")
	realHome := t.TempDir()

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + realDir,
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + stateDir,
		"HOME=" + realHome,
	}

	first, err := Build("codex", environ)
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}

	// Simulate the host process (from the first launch) having written its
	// own runtime state into NativeHomeDir -- exactly what a real Codex
	// session does with state_5.sqlite.
	hostState := filepath.Join(first.NativeHomeDir, "state_5.sqlite")
	if err := os.WriteFile(hostState, []byte("real session state"), 0o644); err != nil {
		t.Fatalf("simulating host-written state: %v", err)
	}

	second, err := Build("codex", environ)
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if second.NativeHomeDir != first.NativeHomeDir {
		t.Fatalf("NativeHomeDir changed across relaunches in the same worktree: first %q, second %q -- host state would be silently orphaned", first.NativeHomeDir, second.NativeHomeDir)
	}

	got, err := os.ReadFile(hostState)
	if err != nil {
		t.Fatalf("a second launch removed the first launch's host-written state: %v", err)
	}
	if string(got) != "real session state" {
		t.Errorf("host-written state content changed = %q, want untouched %q", got, "real session state")
	}
}

// TestBuild_ResolvesPastASDFShim is issue #69's own regression proof for
// the PATH-shim launch path (the "or the PATH-shim launch path" half of the
// issue's exit gate; cmd/omca/run_exec_test.go separately covers `omca run
// --mode isolated`'s own equivalent). When ResolveReal's PATH search lands
// on an asdf-managed shim script (per IsASDFShim's location heuristic),
// Build must resolve RealBinaryPath straight past it to the concrete,
// per-version real binary asdf's own shim-generation metadata names --
// never the shim script itself, which Exec (exec.go) would go on to fail
// exec'ing once HOME is overridden to this generation's virtual-home
// directory (an asdf shim's own dispatch needs a real, resolvable HOME).
func TestBuild_ResolvesPastASDFShim(t *testing.T) {
	stateDir := t.TempDir()
	buildFixtureGeneration(t, stateDir)

	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")

	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	writeASDFShim(t, asdfDataDir, "codex", [][2]string{{"nodejs", "20.19.0"}})
	wantReal := writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "20.19.0", "codex")

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + filepath.Join(asdfDataDir, "shims"),
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + stateDir,
		"HOME=" + t.TempDir(),
	}

	plan, err := Build("codex", environ)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.RealBinaryPath != wantReal {
		t.Errorf("RealBinaryPath = %q, want %q (the resolved real binary, not the asdf shim script at %s)", plan.RealBinaryPath, wantReal, filepath.Join(asdfDataDir, "shims", "codex"))
	}
}

// TestBuild_AmbiguousASDFShim_FailsWithActionableError proves Build refuses
// to guess when the resolved binary is an asdf shim naming two or more
// plugin versions (ResolveASDFShimTarget's own refusal to replicate asdf's
// .tool-versions precedence), and that the resulting error is actionable --
// names the asdf shim and points at a workaround -- rather than a bare
// propagated "not found"/"cannot execute".
func TestBuild_AmbiguousASDFShim_FailsWithActionableError(t *testing.T) {
	stateDir := t.TempDir()
	buildFixtureGeneration(t, stateDir)

	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")

	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	writeASDFShim(t, asdfDataDir, "codex", [][2]string{
		{"nodejs", "20.19.0"},
		{"nodejs", "18.20.0"},
	})
	writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "20.19.0", "codex")
	writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "18.20.0", "codex")

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + filepath.Join(asdfDataDir, "shims"),
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + stateDir,
		"HOME=" + t.TempDir(),
	}

	_, err := Build("codex", environ)
	if err == nil {
		t.Fatal("Build against an ambiguous asdf shim: want error, got nil")
	}
	if !strings.Contains(err.Error(), "asdf") {
		t.Errorf("Build error does not mention asdf, want an actionable message naming the problem: %v", err)
	}
	// Regression test (Copilot review finding on this PR): the message must
	// describe the resolved path's LOCATION, not assert it is a confirmed
	// asdf shim -- IsASDFShim's location heuristic can match a path that
	// ResolveASDFShimTarget then fails to resolve for a reason other than
	// "genuinely asdf-managed" (e.g. an unrecognized/foreign shim format),
	// so claiming confirmed asdf-ness here would be misleading in that case.
	if strings.Contains(err.Error(), "resolves to an asdf-managed shim") {
		t.Errorf("Build error overclaims confirmed asdf-shim identity instead of describing the path's location: %v", err)
	}
}

// TestBuild_MissingHOME proves Build fails closed, with a clear actionable
// error, when the shim's own received environment has no HOME at all --
// mirroring TestBuild_MissingStateDir's identical fail-closed treatment of
// OMCA_STATE_DIR. Without this check, Exec (exec.go) would silently set
// OMCA_REAL_HOME="" on the exec'd process: indistinguishable from "this
// really is empty" rather than "the real value was never known."
func TestBuild_MissingHOME(t *testing.T) {
	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")
	realDir := t.TempDir()
	writeFakeExecutable(t, realDir, "codex")

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + realDir,
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + t.TempDir(),
	}
	if _, err := Build("codex", environ); err == nil {
		t.Fatal("Build with no HOME: want error, got nil")
	}
}

// TestBuild_UnrecognizedInvokedName proves Build refuses anything other
// than its two known entry points rather than guessing.
func TestBuild_UnrecognizedInvokedName(t *testing.T) {
	if _, err := Build("omca", []string{}); err == nil {
		t.Fatal("Build(\"omca\", ...): want error, got nil")
	}
}

// TestBuild_MissingStateDir proves a clear, actionable error rather than a
// panic or a silent unmanaged fallback when OMCA_STATE_DIR was never set —
// e.g. the shim binary invoked directly, outside any `omca env`/direnv
// session.
func TestBuild_MissingStateDir(t *testing.T) {
	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")
	realDir := t.TempDir()
	writeFakeExecutable(t, realDir, "codex")

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + realDir,
		"OMCA_SHIM_DIR=" + shimDir,
		"HOME=" + t.TempDir(),
	}
	if _, err := Build("codex", environ); err == nil {
		t.Fatal("Build with no OMCA_STATE_DIR: want error, got nil")
	}
}

// TestBuild_NoCurrentGeneration proves a clear error when OMCA_STATE_DIR is
// set but no generation has ever been compiled for this host in it (a
// worktree that has never had `omca env` run against it yet).
func TestBuild_NoCurrentGeneration(t *testing.T) {
	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")
	realDir := t.TempDir()
	writeFakeExecutable(t, realDir, "codex")
	stateDir := t.TempDir()

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + realDir,
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + stateDir,
		"HOME=" + t.TempDir(),
	}
	if _, err := Build("codex", environ); err == nil {
		t.Fatal("Build with no compiled generation: want error, got nil")
	}
}

// TestBuild_ResolvesInterpreter_WhenASDFTargetIsItselfAnEnvIndirectScript is
// this fix's own regression proof, reproducing the real machine's exact
// shape found while dogfooding this project (a new instance of issue #69's
// class of bug, one interpreter layer deeper than TestBuild_
// ResolvesPastASDFShim above covers): codex's own asdf-installed binary is
// not a native executable but a "#!/usr/bin/env node" script, and node is
// *also* asdf-managed on the same machine. Without this fix, Build would
// hand back RealBinaryPath pointing at that script as-is, and Exec would
// rely on the OS's own shebang handling to find "node" via PATH at exec
// time -- exactly when HOME has already been virtualized, which breaks
// node's own asdf shim dispatch (silently: exit 126, no output, verified by
// hand against a real asdf+node+codex install during this fix's own
// investigation). Build must instead resolve "node" itself past its asdf
// shim to a concrete binary and report it via Plan.InterpreterPath.
func TestBuild_ResolvesInterpreter_WhenASDFTargetIsItselfAnEnvIndirectScript(t *testing.T) {
	stateDir := t.TempDir()
	buildFixtureGeneration(t, stateDir)

	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")

	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	writeASDFShim(t, asdfDataDir, "codex", [][2]string{{"nodejs", "20.19.0"}})
	writeASDFShim(t, asdfDataDir, "node", [][2]string{{"nodejs", "20.19.0"}})

	// The "installed" codex binary is itself an env-indirect script, not a
	// native binary -- exactly codex-cli's own real, observed shape.
	codexBinDir := filepath.Join(asdfDataDir, "installs", "nodejs", "20.19.0", "bin")
	if err := os.MkdirAll(codexBinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codexScript := filepath.Join(codexBinDir, "codex")
	if err := os.WriteFile(codexScript, []byte("#!/usr/bin/env node\nconsole.log(\"codex\");\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantInterpreter := writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "20.19.0", "node")

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + filepath.Join(asdfDataDir, "shims"),
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + stateDir,
		"HOME=" + t.TempDir(),
	}

	plan, err := Build("codex", environ)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.RealBinaryPath != codexScript {
		t.Errorf("RealBinaryPath = %q, want %q (the resolved codex script itself, unresolved further)", plan.RealBinaryPath, codexScript)
	}
	if plan.InterpreterPath != wantInterpreter {
		t.Errorf("InterpreterPath = %q, want %q (node resolved past its own asdf shim)", plan.InterpreterPath, wantInterpreter)
	}
}

// TestBuild_InterpreterPath_Empty_WhenTargetIsNotEnvIndirectScript proves
// the overwhelmingly common case -- RealBinaryPath is an ordinary native
// binary, not any kind of shebang script -- leaves InterpreterPath empty,
// so Exec's existing, unmodified behavior (exec RealBinaryPath directly)
// still applies. This is TestBuild_ResolvesRealBinaryAndInjectsGenerationEnv's
// own fixture, with only the new field asserted, proving this fix does not
// change behavior for the non-asdf, non-script case.
func TestBuild_InterpreterPath_Empty_WhenTargetIsNotEnvIndirectScript(t *testing.T) {
	stateDir := t.TempDir()
	buildFixtureGeneration(t, stateDir)

	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")
	realDir := t.TempDir()
	writeFakeExecutable(t, realDir, "codex") // "#!/bin/sh\nexit 0\n" -- an absolute-path shebang, not env-indirect

	environ := []string{
		"PATH=" + shimDir + string(os.PathListSeparator) + realDir,
		"OMCA_SHIM_DIR=" + shimDir,
		"OMCA_STATE_DIR=" + stateDir,
		"HOME=" + t.TempDir(),
	}

	plan, err := Build("codex", environ)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.InterpreterPath != "" {
		t.Errorf("InterpreterPath = %q, want \"\" (RealBinaryPath is not an env-indirect script)", plan.InterpreterPath)
	}
}
