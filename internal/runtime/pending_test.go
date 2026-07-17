package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestSetPendingGeneration_RoundTrip mirrors current_test.go's
// TestSetCurrentGeneration_RoundTrip exactly, for the "pending" pointer
// issue #18 AC adds: current/pending pointers exist under worktree state.
// Proves the pointer and its CurrentRecord sidecar both round-trip through
// SetPendingGeneration/PendingGenerationDir/ReadPendingRecord.
func TestSetPendingGeneration_RoundTrip(t *testing.T) {
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	req := BootstrapRequest{Detection: tr.detection("0.144.5"), Worktree: tr.worktree(t), Observations: obs, Now: now}

	worktreeStateDir := t.TempDir()
	gen, outputDir, err := EnsureGeneration(req, filepath.Join(worktreeStateDir, "generations"))
	if err != nil {
		t.Fatalf("EnsureGeneration: %v", err)
	}
	restoreWritable(t, outputDir)

	det := req.Detection
	det.BinaryPath = "/fake/bin/codex"
	if err := SetPendingGeneration(worktreeStateDir, "codex", outputDir, gen, det, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}

	gotDir, err := PendingGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("PendingGenerationDir: %v", err)
	}
	if gotDir != outputDir {
		t.Errorf("PendingGenerationDir = %q, want %q", gotDir, outputDir)
	}

	rec, err := ReadPendingRecord(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadPendingRecord: %v", err)
	}
	if rec.GenerationID != gen.Metadata.ID {
		t.Errorf("CurrentRecord.GenerationID = %q, want %q", rec.GenerationID, gen.Metadata.ID)
	}
	if rec.HostBinaryPath != "/fake/bin/codex" {
		t.Errorf("CurrentRecord.HostBinaryPath = %q, want %q", rec.HostBinaryPath, "/fake/bin/codex")
	}
}

// TestPendingAndCurrent_AreIndependentPointers proves "pending" and
// "current" name two genuinely separate generations at once -- the whole
// point of adding a distinct pending pointer rather than reusing "current"
// (docs/architecture/runtime.md §5's layout: current and pending are
// siblings, both symlinks into generations/). Setting pending to a second
// generation must not disturb an already-set current pointer, and vice
// versa; this PR only builds the structures, not the PR-15 transaction that
// would eventually move one into the other.
func TestPendingAndCurrent_AreIndependentPointers(t *testing.T) {
	worktreeStateDir := t.TempDir()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	tr1 := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr1.WorktreeRoot, "AGENTS.md"), "# first\n")
	obs1, err := observe.Observe(tr1.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe (1st): %v", err)
	}
	req1 := BootstrapRequest{Detection: tr1.detection("0.144.5"), Worktree: tr1.worktree(t), Observations: obs1, Now: now}
	gen1, dir1, err := EnsureGeneration(req1, filepath.Join(worktreeStateDir, "generations"))
	if err != nil {
		t.Fatalf("EnsureGeneration (1st): %v", err)
	}
	restoreWritable(t, dir1)
	if err := SetCurrentGeneration(worktreeStateDir, "codex", dir1, gen1, req1.Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}

	tr2 := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr2.WorktreeRoot, "AGENTS.md"), "# second, different content\n")
	obs2, err := observe.Observe(tr2.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe (2nd): %v", err)
	}
	req2 := BootstrapRequest{Detection: tr2.detection("0.144.5"), Worktree: tr2.worktree(t), Observations: obs2, Now: now.Add(time.Minute)}
	gen2, dir2, err := EnsureGeneration(req2, filepath.Join(worktreeStateDir, "generations"))
	if err != nil {
		t.Fatalf("EnsureGeneration (2nd): %v", err)
	}
	restoreWritable(t, dir2)
	if dir1 == dir2 {
		t.Fatalf("the two requests produced the same generation directory %q; fixture setup did not actually vary the input", dir1)
	}
	if err := SetPendingGeneration(worktreeStateDir, "codex", dir2, gen2, req2.Detection, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}

	gotCurrent, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotCurrent != dir1 {
		t.Errorf("CurrentGenerationDir = %q, want the 1st generation's dir %q (setting pending must not disturb current)", gotCurrent, dir1)
	}
	gotPending, err := PendingGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("PendingGenerationDir: %v", err)
	}
	if gotPending != dir2 {
		t.Errorf("PendingGenerationDir = %q, want the 2nd generation's dir %q", gotPending, dir2)
	}
}

// TestPendingGenerationDir_NoPointer_IsNotExist mirrors
// current_test.go's TestCurrentGenerationDir_NoPointer_IsNotExist: a host
// with no pending pointer set yet reports an os.IsNotExist-satisfying
// error.
func TestPendingGenerationDir_NoPointer_IsNotExist(t *testing.T) {
	worktreeStateDir := t.TempDir()
	_, err := PendingGenerationDir(worktreeStateDir, "codex")
	if err == nil {
		t.Fatal("PendingGenerationDir with no pointer set: want error, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("PendingGenerationDir error = %v, want an os.IsNotExist error", err)
	}
}
