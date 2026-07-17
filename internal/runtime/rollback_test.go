package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestRollback_RestoresParentAndLedgers is the M2 AC's own rollback test:
// "rollback restores the parent generation and is itself ledgered."
func TestRollback_RestoresParentAndLedgers(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")

	parentFx := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "code-review")}, nil, now)
	parentID := parentFx.gen.Metadata.ID
	childFx := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "deep-refactor")}, &parentID, now.Add(time.Minute))
	if childFx.gen.Metadata.ID == parentFx.gen.Metadata.ID {
		t.Fatal("fixture setup did not actually vary content between parent and child generations")
	}

	// Activate the parent first, then the child, exactly like two real
	// activations in sequence, so "current" genuinely has Metadata.Parent
	// set the way Compile's own Parent field is meant to be used.
	if err := SetPendingGeneration(worktreeStateDir, "codex", parentFx.outputDir, parentFx.gen, parentFx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration (parent): %v", err)
	}
	if _, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: parentFx.req, Now: now}); err != nil {
		t.Fatalf("Activate (parent): %v", err)
	}
	if err := SetPendingGeneration(worktreeStateDir, "codex", childFx.outputDir, childFx.gen, childFx.req.Hosts[0].Detection, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetPendingGeneration (child): %v", err)
	}
	if _, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: childFx.req, Now: now.Add(time.Minute)}); err != nil {
		t.Fatalf("Activate (child): %v", err)
	}

	gotCurrent, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir before rollback: %v", err)
	}
	if gotCurrent != childFx.outputDir {
		t.Fatalf("CurrentGenerationDir before rollback = %q, want the child generation %q", gotCurrent, childFx.outputDir)
	}

	result, err := Rollback(worktreeStateDir, generationsRoot, "codex", childFx.req.Hosts[0].Detection, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if result.RestoredGenerationID != parentFx.gen.Metadata.ID {
		t.Errorf("RestoredGenerationID = %q, want the parent %q", result.RestoredGenerationID, parentFx.gen.Metadata.ID)
	}
	if result.SupersededGenerationID != childFx.gen.Metadata.ID {
		t.Errorf("SupersededGenerationID = %q, want the child %q", result.SupersededGenerationID, childFx.gen.Metadata.ID)
	}

	gotCurrentAfter, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir after rollback: %v", err)
	}
	if gotCurrentAfter != parentFx.outputDir {
		t.Errorf("CurrentGenerationDir after rollback = %q, want the parent generation %q", gotCurrentAfter, parentFx.outputDir)
	}

	entries, err := ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Kind == "rolledback" && e.GenerationID == parentFx.gen.Metadata.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("ledger has no 'rolledback' entry for the restored parent %s: %+v", parentFx.gen.Metadata.ID, entries)
	}
}

// TestRollback_NoCurrent_ReturnsError proves Rollback refuses when there is
// nothing to roll back from.
func TestRollback_NoCurrent_ReturnsError(t *testing.T) {
	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	fx := compileFixture(t, worktreeStateDir, nil, nil, now)
	_, err := Rollback(worktreeStateDir, generationsRoot, "codex", fx.req.Hosts[0].Detection, now)
	if err == nil {
		t.Fatal("Rollback with no current generation: want error, got nil")
	}
}

// TestRollback_NoParent_ReturnsError proves Rollback refuses (rather than
// guessing) when the current generation names no parent at all -- e.g. a
// worktree's very first activation.
func TestRollback_NoParent_ReturnsError(t *testing.T) {
	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	fx := compileFixture(t, worktreeStateDir, nil, nil, now)
	if err := SetCurrentGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}

	_, err := Rollback(worktreeStateDir, generationsRoot, "codex", fx.req.Hosts[0].Detection, now.Add(time.Minute))
	if err == nil {
		t.Fatal("Rollback with a current generation that has no parent: want error, got nil")
	}
}

// TestRollback_ParentGenerationMissingOnDisk_ReturnsError proves Rollback
// fails clearly (never a guess, never a panic) when the current generation
// names a parent ID that is not actually present at its expected content-
// addressed path -- e.g. garbage-collected, or generationsRoot points at
// the wrong worktree's state.
func TestRollback_ParentGenerationMissingOnDisk_ReturnsError(t *testing.T) {
	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	missingParent := "generation:sha256:" + strings.Repeat("0", 64)
	fx := compileFixture(t, worktreeStateDir, nil, &missingParent, now)
	if err := SetCurrentGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}

	_, err := Rollback(worktreeStateDir, generationsRoot, "codex", fx.req.Hosts[0].Detection, now.Add(time.Minute))
	if err == nil {
		t.Fatal("Rollback with a parent generation missing on disk: want error, got nil")
	}
}

// TestRollback_CorruptCurrentPointer_ReportsCorruptionNotMissing is a
// regression test for a real Copilot review finding on this PR: Rollback
// used to wrap every CurrentGenerationDir error -- not just os.IsNotExist --
// under the same "no current generation ... to roll back from" message,
// which was misleading for a genuinely corrupt pointer (e.g. a "current"
// entry that exists but isn't a readable symlink) and made debugging
// harder. This plants exactly that corruption and proves the error now
// says "corrupt," not "no current generation."
func TestRollback_CorruptCurrentPointer_ReportsCorruptionNotMissing(t *testing.T) {
	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	fx := compileFixture(t, worktreeStateDir, nil, nil, now)

	linkPath := pointerLinkPath(worktreeStateDir, "current", "codex")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linkPath, []byte("not a symlink"), 0o644); err != nil {
		t.Fatalf("planting a corrupt (non-symlink) 'current' entry: %v", err)
	}

	_, err := Rollback(worktreeStateDir, generationsRoot, "codex", fx.req.Hosts[0].Detection, now)
	if err == nil {
		t.Fatal("Rollback with a corrupt current pointer: want error, got nil")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error = %q, want it to mention the pointer is corrupt, not just \"no current generation\"", err.Error())
	}
	if strings.Contains(err.Error(), "no current generation") {
		t.Errorf("error = %q, downgraded real pointer corruption to the ordinary not-yet-managed message", err.Error())
	}
}

// TestRollback_BlockedByInProgressActivate proves Rollback takes the exact
// same activation lock Activate does (activationlock.go's
// ActivationInProgressError doc comment: "Rollback has the identical
// read-then-write shape for the same 'current' pointer... an Activate racing
// a Rollback" is one of the two race shapes the lock closes, alongside two
// concurrent Activate calls -- activate_test.go's
// TestActivate_ConcurrentActivations_LockPreventsSilentClobber already
// covers that half). It pauses a real, in-flight Activate call right before
// it writes "current" (still holding its lock) and proves a concurrent
// Rollback call for the same host fails fast with a
// *ActivationInProgressError -- never blocking forever, and never racing
// Activate's own write to "current" -- then confirms Activate itself still
// completes normally once released.
func TestRollback_BlockedByInProgressActivate(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	parentFx := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "code-review")}, nil, now)
	if err := SetCurrentGeneration(worktreeStateDir, "codex", parentFx.outputDir, parentFx.gen, parentFx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}
	parentID := parentFx.gen.Metadata.ID
	childFx := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "deep-refactor")}, &parentID, now.Add(time.Minute))
	if err := SetPendingGeneration(worktreeStateDir, "codex", childFx.outputDir, childFx.gen, childFx.req.Hosts[0].Detection, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}

	reachedPause := make(chan struct{})
	release := make(chan struct{})
	hook := func(step ActivationStep) error {
		if step == StepSwitchCurrent {
			close(reachedPause)
			<-release
		}
		return nil
	}
	doneActivate := make(chan error, 1)
	go func() {
		_, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: childFx.req, Now: now.Add(2 * time.Minute), OnStep: hook})
		doneActivate <- err
	}()

	waitOrFatal(t, reachedPause, "Activate to reach StepSwitchCurrent") // Activate holds the lock and is paused right before writing "current".

	generationsRoot := filepath.Join(worktreeStateDir, "generations")
	_, rollbackErr := Rollback(worktreeStateDir, generationsRoot, "codex", parentFx.req.Hosts[0].Detection, now.Add(3*time.Minute))
	close(release)
	activateErr := <-doneActivate

	if rollbackErr == nil {
		t.Fatal("Rollback concurrent with an in-flight Activate for the same host: want an error, got nil")
	}
	var inProgress *ActivationInProgressError
	if !errors.As(rollbackErr, &inProgress) {
		t.Fatalf("Rollback error = %v (%T), want a *ActivationInProgressError", rollbackErr, rollbackErr)
	}
	if activateErr != nil {
		t.Fatalf("Activate (released after the blocked Rollback attempt): want success, got %v", activateErr)
	}

	gotCurrent, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotCurrent != childFx.outputDir {
		t.Errorf("CurrentGenerationDir = %q, want Activate's own generation %q (Rollback must not have touched it)", gotCurrent, childFx.outputDir)
	}
}
