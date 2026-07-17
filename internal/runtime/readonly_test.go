package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestBootstrap_GeneratedTreeIsReadOnly is issue #13 AC #5, "Generated
// artifact trees are read-only on disk": after Bootstrap returns
// successfully, attempting to write a new file into the generation
// directory, and attempting to overwrite an existing generated file, both
// fail with a permission error. Skipped when running as root (root bypasses
// Unix permission bits, the same skip condition
// internal/observe/observe_test.go's TestObserve_UnreadableFile_EmitsE0
// uses).
func TestBootstrap_GeneratedTreeIsReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits only")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not block writes")
	}

	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req := BootstrapRequest{
		Detection:    tr.detection("0.144.5"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}
	outputDir := filepath.Join(t.TempDir(), "generation")
	if _, err := Bootstrap(req, outputDir); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, outputDir) // let t.TempDir() clean up successfully

	// Attempt 1: create a brand-new file directly under the generation
	// root -- blocked because outputDir itself lost its write bit.
	if err := os.WriteFile(filepath.Join(outputDir, "hacked.txt"), []byte("nope"), 0o644); err == nil {
		t.Error("wrote a new file directly under the read-only generation root; want a permission error")
	}

	// Attempt 2: overwrite manifest.json itself.
	manifestPath := filepath.Join(outputDir, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("{}"), 0o644); err == nil {
		t.Error("overwrote manifest.json in the read-only generation tree; want a permission error")
	}

	// Attempt 3: overwrite a compiled artifact inside the per-host tree.
	codexHomeConfig := filepath.Join(outputDir, "hosts", "codex", "cli", "codex-home", "config.toml")
	if _, statErr := os.Stat(codexHomeConfig); statErr != nil {
		t.Fatalf("expected %s to exist: %v", codexHomeConfig, statErr)
	}
	if err := os.WriteFile(codexHomeConfig, []byte("tampered"), 0o644); err == nil {
		t.Error("overwrote a compiled artifact in the read-only generation tree; want a permission error")
	}

	// Attempt 4: create a new file inside an existing subdirectory of the
	// tree (proves the subdirectory itself, not just the root, lost its
	// write bit).
	if err := os.WriteFile(filepath.Join(outputDir, "hosts", "codex", "cli", "codex-home", "new.txt"), []byte("nope"), 0o644); err == nil {
		t.Error("created a new file inside a read-only generation subdirectory; want a permission error")
	}
}
