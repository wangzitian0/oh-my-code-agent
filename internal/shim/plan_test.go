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
	wantHomeDir := filepath.Join(outputDir, "hosts", "codex", "cli", "codex-home")
	if plan.NativeHomeDir != wantHomeDir {
		t.Errorf("NativeHomeDir = %q, want %q", plan.NativeHomeDir, wantHomeDir)
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
