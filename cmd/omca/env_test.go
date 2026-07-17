package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// wantExportKeys is the exact docs/architecture/runtime.md §4 variable set
// `omca env` must print (issue #14: "prints shell export statements to
// stdout matching docs/architecture/runtime.md §4 exactly").
var wantExportKeys = []string{
	"OMCA_CONTEXT_ID",
	"OMCA_WORKTREE_ID",
	"OMCA_REAL_HOME",
	"OMCA_STATE_DIR",
	"OMCA_SHIM_DIR",
}

// TestRunEnv_PrintsExpectedExportsAndInstallsShims exercises `omca env`
// against a fully hermetic environment (both hosts "installed" via fake
// --version-only binaries) and checks every piece of issue #14's AC #1:
// the five named variables plus the PATH line are on stdout, nothing else
// is, and $OMCA_SHIM_DIR actually contains working codex/claude entries.
func TestRunEnv_PrintsExpectedExportsAndInstallsShims(t *testing.T) {
	env := setupManagedTestEnv(t, true, true)

	var stdout, stderr bytes.Buffer
	code := runEnv(&stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runEnv = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	out := stdout.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(wantExportKeys)+1 {
		t.Fatalf("stdout has %d lines, want %d (5 export lines + 1 PATH line):\n%s", len(lines), len(wantExportKeys)+1, out)
	}
	for i, key := range wantExportKeys {
		prefix := "export " + key + "="
		if !strings.HasPrefix(lines[i], prefix) {
			t.Errorf("line %d = %q, want prefix %q", i, lines[i], prefix)
		}
	}
	wantLastLine := `export PATH="$OMCA_SHIM_DIR:$PATH"`
	if lines[len(lines)-1] != wantLastLine {
		t.Errorf("last line = %q, want %q", lines[len(lines)-1], wantLastLine)
	}

	wantWorktreeID, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	if !strings.Contains(out, "OMCA_WORKTREE_ID='"+wantWorktreeID.ID+"'") {
		t.Errorf("stdout does not contain the expected worktree ID %q:\n%s", wantWorktreeID.ID, out)
	}

	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wantWorktreeID.ID)
	shimDir := shimDirPath(worktreeStateDir)
	if !strings.Contains(out, "OMCA_SHIM_DIR='"+shimDir+"'") {
		t.Errorf("stdout does not contain the expected shim dir %q:\n%s", shimDir, out)
	}
	if !strings.Contains(out, "OMCA_STATE_DIR='"+worktreeStateDir+"'") {
		t.Errorf("stdout does not contain the expected state dir %q:\n%s", worktreeStateDir, out)
	}

	for _, name := range []string{"codex", "claude"} {
		linkPath := filepath.Join(shimDir, name)
		info, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("shim entry %s: %v", name, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("shim entry %s is not a symlink", name)
		}
		if _, err := os.Stat(linkPath); err != nil {
			t.Errorf("shim entry %s does not resolve to an existing file: %v", name, err)
		}
	}

	for _, host := range []string{"codex", "claude-code"} {
		if _, err := runtime.CurrentGenerationDir(worktreeStateDir, host); err != nil {
			t.Errorf("no current generation recorded for %s: %v", host, err)
		}
	}
}

// TestRunEnv_SecondCall_ReusesCompiledGeneration proves the idempotent-
// generation design decision end to end through the CLI layer: running
// `omca env` twice against an unchanged worktree compiles the generation
// exactly once.
func TestRunEnv_SecondCall_ReusesCompiledGeneration(t *testing.T) {
	setupManagedTestEnv(t, true, false)

	var stdout1, stderr1 bytes.Buffer
	if code := runEnv(&stdout1, &stderr1, nil); code != 0 {
		t.Fatalf("runEnv (1st) = %d; stderr:\n%s", code, stderr1.String())
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	cwd, _ := os.Getwd()
	wt, err := hostcontext.DetectWorktree(cwd)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	genDir, err := runtime.CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	manifestPath := filepath.Join(genDir, "manifest.json")
	firstInfo, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatalf("stat manifest: %v", err)
	}

	var stdout2, stderr2 bytes.Buffer
	if code := runEnv(&stdout2, &stderr2, nil); code != 0 {
		t.Fatalf("runEnv (2nd) = %d; stderr:\n%s", code, stderr2.String())
	}
	secondInfo, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatalf("stat manifest (2nd): %v", err)
	}
	if !firstInfo.ModTime().Equal(secondInfo.ModTime()) {
		t.Errorf("manifest.json mtime changed on the 2nd `omca env` run: it was recompiled instead of reused")
	}
	if stdout1.String() != stdout2.String() {
		t.Errorf("stdout differs across identical `omca env` runs:\n1st: %s\n2nd: %s", stdout1.String(), stdout2.String())
	}
}

// TestRunEnv_HostNotInstalled_StillSucceeds proves `omca env` degrades
// gracefully (issue #14's own generation-compile step says "runs
// internal/observe.Observe per installed host" — an uninstalled host is
// simply skipped, not a command failure) when neither host is present.
func TestRunEnv_HostNotInstalled_StillSucceeds(t *testing.T) {
	setupManagedTestEnv(t, false, false)

	var stdout, stderr bytes.Buffer
	code := runEnv(&stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runEnv = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OMCA_WORKTREE_ID=") {
		t.Errorf("stdout missing OMCA_WORKTREE_ID even with no hosts installed:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not installed") {
		t.Errorf("stderr does not mention 'not installed':\n%s", stderr.String())
	}
}

// TestRunEnv_RejectsUnsupportedShell proves the documented M1 scope cut
// (bash-only export syntax) is an explicit, actionable error rather than
// silently emitting bash syntax under a different shell's name.
func TestRunEnv_RejectsUnsupportedShell(t *testing.T) {
	setupManagedTestEnv(t, false, false)

	var stdout, stderr bytes.Buffer
	code := runEnv(&stdout, &stderr, []string{"--shell", "fish"})
	if code != 2 {
		t.Fatalf("runEnv(--shell fish) = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on a rejected --shell value", stdout.String())
	}
}
