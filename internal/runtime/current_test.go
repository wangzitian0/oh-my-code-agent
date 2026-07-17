package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestDirSafeID_StripsColons proves DirSafeID turns the ":"-delimited IDs
// this project produces into a plain, portable directory-name shape.
func TestDirSafeID_StripsColons(t *testing.T) {
	got := DirSafeID("generation:sha256:abcd1234")
	want := "generation-sha256-abcd1234"
	if got != want {
		t.Errorf("DirSafeID = %q, want %q", got, want)
	}
}

// TestEnsureGeneration_CompilesOnFirstCall_ReusesOnSecond is issue #14's
// idempotent-generation design decision made concrete: calling
// EnsureGeneration twice with an identical request must compile exactly
// once (Bootstrap actually runs) and reuse the existing directory the
// second time (no error, no rewrite, identical Generation returned) — the
// "cheap re-entry" this PR's own scope note names as the M1 replacement for
// a separate pending/activate step.
func TestEnsureGeneration_CompilesOnFirstCall_ReusesOnSecond(t *testing.T) {
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
	generationsRoot := filepath.Join(t.TempDir(), "generations")

	gen1, dir1, err := EnsureGeneration(req, generationsRoot)
	if err != nil {
		t.Fatalf("EnsureGeneration (1st): %v", err)
	}
	restoreWritable(t, dir1)
	firstWriteTime := mustModTime(t, filepath.Join(dir1, "manifest.json"))

	// A second EnsureGeneration call must not rewrite manifest.json — proof
	// that it actually reused the directory rather than recompiling
	// (recompiling would still produce a byte-identical manifest per
	// Bootstrap's own idempotency, so content equality alone would not
	// distinguish "reused" from "recompiled"; comparing mtimes does).
	time.Sleep(10 * time.Millisecond)
	gen2, dir2, err := EnsureGeneration(req, generationsRoot)
	if err != nil {
		t.Fatalf("EnsureGeneration (2nd): %v", err)
	}
	if dir1 != dir2 {
		t.Fatalf("EnsureGeneration (2nd) outputDir = %q, want the same directory %q", dir2, dir1)
	}
	if gen1.Metadata.ID != gen2.Metadata.ID {
		t.Errorf("Generation.Metadata.ID differs across calls: %q != %q", gen1.Metadata.ID, gen2.Metadata.ID)
	}
	secondWriteTime := mustModTime(t, filepath.Join(dir2, "manifest.json"))
	if !firstWriteTime.Equal(secondWriteTime) {
		t.Errorf("manifest.json mtime changed on the 2nd EnsureGeneration call (%v -> %v): it was recompiled instead of reused", firstWriteTime, secondWriteTime)
	}
}

func mustModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}

// TestEnsureGeneration_RejectsRelativeGenerationsRoot mirrors Bootstrap's
// own TestBootstrap_RejectsRelativeOutputDir: EnsureGeneration must not
// silently resolve a relative generationsRoot against the process cwd.
func TestEnsureGeneration_RejectsRelativeGenerationsRoot(t *testing.T) {
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
	if _, _, err := EnsureGeneration(req, "relative/generations"); err == nil {
		t.Fatal("EnsureGeneration with a relative generationsRoot: want error, got nil")
	}
}

// TestSetCurrentGeneration_RoundTrip proves the "current" pointer and its
// CurrentRecord sidecar both round-trip through
// SetCurrentGeneration/CurrentGenerationDir/ReadCurrentRecord — the
// lightweight pointer mechanism internal/shim.Build and cmd/omca's doctor
// both depend on.
func TestSetCurrentGeneration_RoundTrip(t *testing.T) {
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
	if err := SetCurrentGeneration(worktreeStateDir, "codex", outputDir, gen, det, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}

	gotDir, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotDir != outputDir {
		t.Errorf("CurrentGenerationDir = %q, want %q", gotDir, outputDir)
	}

	rec, err := ReadCurrentRecord(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadCurrentRecord: %v", err)
	}
	if rec.GenerationID != gen.Metadata.ID {
		t.Errorf("CurrentRecord.GenerationID = %q, want %q", rec.GenerationID, gen.Metadata.ID)
	}
	if rec.HostBinaryPath != "/fake/bin/codex" {
		t.Errorf("CurrentRecord.HostBinaryPath = %q, want %q", rec.HostBinaryPath, "/fake/bin/codex")
	}
	if rec.HostVersion != "0.144.5" {
		t.Errorf("CurrentRecord.HostVersion = %q, want %q", rec.HostVersion, "0.144.5")
	}
}

// TestSetCurrentGeneration_OverwritesPreviousPointer proves calling
// SetCurrentGeneration twice for the same host (the realistic "omca env
// run again after a native config change" case) replaces the pointer
// rather than erroring or leaving stale state — M1 has no distinct
// pending/activate step (see doc.go), so "recompile, then immediately
// repoint current" is the whole mechanism.
func TestSetCurrentGeneration_OverwritesPreviousPointer(t *testing.T) {
	worktreeStateDir := t.TempDir()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	tr1 := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr1.WorktreeRoot, "AGENTS.md"), "# first\n")
	obs1, err := observe.Observe(tr1.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req1 := BootstrapRequest{Detection: tr1.detection("0.144.5"), Worktree: tr1.worktree(t), Observations: obs1, Now: now}
	gen1, dir1, err := EnsureGeneration(req1, filepath.Join(worktreeStateDir, "generations"))
	if err != nil {
		t.Fatalf("EnsureGeneration (1st): %v", err)
	}
	restoreWritable(t, dir1)
	if err := SetCurrentGeneration(worktreeStateDir, "codex", dir1, gen1, req1.Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration (1st): %v", err)
	}

	tr2 := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr2.WorktreeRoot, "AGENTS.md"), "# second, different content\n")
	obs2, err := observe.Observe(tr2.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
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
	if err := SetCurrentGeneration(worktreeStateDir, "codex", dir2, gen2, req2.Detection, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetCurrentGeneration (2nd): %v", err)
	}

	gotDir, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotDir != dir2 {
		t.Errorf("CurrentGenerationDir after 2nd SetCurrentGeneration = %q, want the 2nd generation's dir %q", gotDir, dir2)
	}
}

// TestCurrentGenerationDir_NoPointer_IsNotExist proves a host with no
// pointer set yet reports an os.IsNotExist-satisfying error, the contract
// internal/shim.Build and doctor both rely on to distinguish "not managed
// yet" from a real failure.
func TestCurrentGenerationDir_NoPointer_IsNotExist(t *testing.T) {
	worktreeStateDir := t.TempDir()
	_, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err == nil {
		t.Fatal("CurrentGenerationDir with no pointer set: want error, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("CurrentGenerationDir error = %v, want an os.IsNotExist error", err)
	}
}
