package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// TestParseBisectArgs_DryRunFlag proves --dry-run parses in either position
// relative to the host, mirroring parseRunArgs/parseActivateArgs's own
// established flag-parsing conventions in this package.
func TestParseBisectArgs_DryRunFlag(t *testing.T) {
	got, err := parseBisectArgs([]string{"--dry-run", "codex"})
	if err != nil {
		t.Fatalf("parseBisectArgs: %v", err)
	}
	if !got.DryRun || got.Host != "codex" {
		t.Errorf("parseBisectArgs = %+v, want {DryRun:true Host:codex}", got)
	}

	got2, err := parseBisectArgs([]string{"claude", "--dry-run"})
	if err != nil {
		t.Fatalf("parseBisectArgs: %v", err)
	}
	if !got2.DryRun || got2.Host != "claude" {
		t.Errorf("parseBisectArgs = %+v, want {DryRun:true Host:claude}", got2)
	}
}

// TestParseBisectArgs_NoDryRunFlag_DefaultsFalse proves the real (compiling)
// path is what runs when --dry-run is omitted -- the round-3 safety audit's
// own distinction between the mandatory report-only mode and the real one.
func TestParseBisectArgs_NoDryRunFlag_DefaultsFalse(t *testing.T) {
	got, err := parseBisectArgs([]string{"codex"})
	if err != nil {
		t.Fatalf("parseBisectArgs: %v", err)
	}
	if got.DryRun {
		t.Error("DryRun = true without the flag, want false")
	}
}

// TestParseBisectArgs_MissingHost_Errors proves a bare `omca bisect` (or
// `omca bisect --dry-run` with no host) is a clear parse error, not a silent
// default to some host.
func TestParseBisectArgs_MissingHost_Errors(t *testing.T) {
	if _, err := parseBisectArgs(nil); err == nil {
		t.Fatal("parseBisectArgs(nil): want error, got nil")
	}
	if _, err := parseBisectArgs([]string{"--dry-run"}); err == nil {
		t.Fatal("parseBisectArgs([--dry-run]): want error, got nil")
	}
}

// bisectFixtureEnv builds a managed test environment with three candidate
// Skill sources under the (synthetic, never-real) codex native home, plus a
// repository Instruction, giving `omca bisect` a non-trivial candidate set
// to sequence over.
func bisectFixtureEnv(t *testing.T) managedTestEnv {
	t.Helper()
	env := setupManagedTestEnv(t, true, false)
	mustWriteFileForActivateTest(t, filepath.Join(env.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	mustWriteFileForActivateTest(t, filepath.Join(env.HomeDir, ".codex", "skills", "alpha", "SKILL.md"), "# alpha\n")
	mustWriteFileForActivateTest(t, filepath.Join(env.HomeDir, ".codex", "skills", "bravo", "SKILL.md"), "# bravo\n")
	mustWriteFileForActivateTest(t, filepath.Join(env.HomeDir, ".codex", "skills", "charlie", "SKILL.md"), "# charlie\n")
	return env
}

// TestRunBisect_DryRun_ReportsPlanWithoutWritingAnything is this PR's own
// CLI-level proof of the round-3 pre-dispatch audit's mandatory safety
// mode: `omca bisect --dry-run <host>` prints a plan naming every step but
// creates no generations/ directory at all -- not a partially-populated
// one, none.
func TestRunBisect_DryRun_ReportsPlanWithoutWritingAnything(t *testing.T) {
	env := bisectFixtureEnv(t)

	var stdout, stderr bytes.Buffer
	code := runBisect(&stdout, &stderr, []string{"--dry-run", "codex"})
	if code != 0 {
		t.Fatalf("runBisect --dry-run = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "DRY RUN") {
		t.Errorf("stdout does not announce DRY RUN:\n%s", out)
	}
	if !strings.Contains(out, "step 1/4") || !strings.Contains(out, "step 4/4") {
		t.Errorf("stdout does not show all 4 steps (1 instruction + 3 skills):\n%s", out)
	}
	if strings.Contains(out, "not compiled") == false {
		t.Errorf("stdout does not mark steps as not compiled:\n%s", out)
	}

	generationsRoot := filepath.Join(worktreeStateDirForTest(t, env), "generations")
	if _, err := os.Stat(generationsRoot); !os.IsNotExist(err) {
		t.Errorf("generations/ exists after a --dry-run bisect call (want: never created); stat err = %v", err)
	}
}

// TestRunBisect_Real_BuildsDisposableGenerationsNeverActivates is the real
// (compiling) path's own CLI-level proof: generations actually land on
// disk, one per candidate, and NEITHER host's current/pending pointer is
// ever touched -- `omca bisect` never activates anything it builds.
func TestRunBisect_Real_BuildsDisposableGenerationsNeverActivates(t *testing.T) {
	env := bisectFixtureEnv(t)

	var stdout, stderr bytes.Buffer
	code := runBisect(&stdout, &stderr, []string{"codex"})
	if code != 0 {
		t.Fatalf("runBisect = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "DRY RUN") {
		t.Errorf("stdout announces DRY RUN on a real bisect call:\n%s", out)
	}
	if !strings.Contains(out, "built 4 disposable generation(s)") {
		t.Errorf("stdout does not report 4 built generations:\n%s", out)
	}

	worktreeStateDir := worktreeStateDirForTest(t, env)
	generationsRoot := filepath.Join(worktreeStateDir, "generations")
	entries, err := os.ReadDir(generationsRoot)
	if err != nil {
		t.Fatalf("reading generations/: %v", err)
	}
	if len(entries) != 4 {
		t.Errorf("generations/ has %d entries, want 4 (one disposable generation per candidate)", len(entries))
	}

	if _, err := runtime.CurrentGenerationDir(worktreeStateDir, "codex"); !os.IsNotExist(err) {
		t.Errorf("CurrentGenerationDir after a real bisect call: want os.IsNotExist, got %v", err)
	}
	if _, err := runtime.PendingGenerationDir(worktreeStateDir, "codex"); !os.IsNotExist(err) {
		t.Errorf("PendingGenerationDir after a real bisect call: want os.IsNotExist, got %v", err)
	}
	ledgerEntries, err := runtime.ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	if len(ledgerEntries) != 0 {
		t.Errorf("ReadLedger after a real bisect call = %+v, want no entries", ledgerEntries)
	}
}

// TestRunBisect_HostNotInstalled_Errors proves bisect refuses, clearly,
// rather than silently reporting an empty plan for a host that was never
// even detected.
func TestRunBisect_HostNotInstalled_Errors(t *testing.T) {
	setupManagedTestEnv(t, false, false)

	var stdout, stderr bytes.Buffer
	code := runBisect(&stdout, &stderr, []string{"codex"})
	if code == 0 {
		t.Fatalf("runBisect for an uninstalled host: want non-zero, got 0; stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not installed") {
		t.Errorf("stderr does not explain the host is not installed:\n%s", stderr.String())
	}
}
