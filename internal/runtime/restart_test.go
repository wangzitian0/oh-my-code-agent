package runtime

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestDetectRestartRequired_SessionMatchesCurrent_NoRestart proves a session
// launched with whatever generation is still current needs no restart.
func TestDetectRestartRequired_SessionMatchesCurrent_NoRestart(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()
	fx := compileFixture(t, worktreeStateDir, nil, nil, now)
	if err := SetCurrentGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}

	status, err := DetectRestartRequired(worktreeStateDir, "codex", fx.gen.Metadata.ID)
	if err != nil {
		t.Fatalf("DetectRestartRequired: %v", err)
	}
	if status.RestartRequired {
		t.Errorf("RestartRequired = true, want false: %+v", status)
	}
	if status.CurrentGenerationID != fx.gen.Metadata.ID {
		t.Errorf("CurrentGenerationID = %q, want %q", status.CurrentGenerationID, fx.gen.Metadata.ID)
	}
}

// TestDetectRestartRequired_SessionSuperseded_RestartRequired is the AC's
// core claim: a session running on a superseded generation is detected.
func TestDetectRestartRequired_SessionSuperseded_RestartRequired(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	old := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "code-review")}, nil, now)
	if err := SetCurrentGeneration(worktreeStateDir, "codex", old.outputDir, old.gen, old.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration (old): %v", err)
	}

	// A session launches, recording old.gen.Metadata.ID as its own
	// OMCA_RUN_ID (simulated by capturing it here).
	sessionGenID := old.gen.Metadata.ID

	// A separate activation moves current out from under that session.
	parent := old.gen.Metadata.ID
	next := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "deep-refactor")}, &parent, now.Add(time.Minute))
	if err := SetPendingGeneration(worktreeStateDir, "codex", next.outputDir, next.gen, next.req.Hosts[0].Detection, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}
	if _, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: next.req, Now: now.Add(time.Minute)}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	status, err := DetectRestartRequired(worktreeStateDir, "codex", sessionGenID)
	if err != nil {
		t.Fatalf("DetectRestartRequired: %v", err)
	}
	if !status.RestartRequired {
		t.Errorf("RestartRequired = false, want true: %+v", status)
	}
	if status.CurrentGenerationID != next.gen.Metadata.ID {
		t.Errorf("CurrentGenerationID = %q, want the newly-activated generation %q", status.CurrentGenerationID, next.gen.Metadata.ID)
	}
	if status.SessionGenerationID != sessionGenID {
		t.Errorf("SessionGenerationID = %q, want %q", status.SessionGenerationID, sessionGenID)
	}
}

// TestDetectRestartRequired_PerHost_NotWorktree proves restart_required is
// computed per host: activating codex's generation must not report claude-
// code's still-current session as needing a restart, and vice versa -- the
// AC's own "restart_required is per host" text, and docs/architecture/
// runtime.md §5.5's identical claim.
func TestDetectRestartRequired_PerHost_NotWorktree(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	// codex: activate once, then again -- its session is now superseded.
	codexOld := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "code-review")}, nil, now)
	if err := SetCurrentGeneration(worktreeStateDir, "codex", codexOld.outputDir, codexOld.gen, codexOld.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration (codex old): %v", err)
	}
	codexSessionGenID := codexOld.gen.Metadata.ID
	codexParent := codexOld.gen.Metadata.ID
	codexNext := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "deep-refactor")}, &codexParent, now.Add(time.Minute))
	if err := SetPendingGeneration(worktreeStateDir, "codex", codexNext.outputDir, codexNext.gen, codexNext.req.Hosts[0].Detection, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetPendingGeneration (codex next): %v", err)
	}
	if _, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: codexNext.req, Now: now.Add(time.Minute)}); err != nil {
		t.Fatalf("Activate (codex): %v", err)
	}

	// claude-code: activate once, never touched again -- its session
	// remains current.
	claudeFx := buildClaudeCompileFixture(t, now)
	claudeOutputDir := claudeFx.outputDir
	if err := SetCurrentGeneration(worktreeStateDir, "claude-code", claudeOutputDir, claudeFx.gen, claudeFx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration (claude-code): %v", err)
	}
	claudeSessionGenID := claudeFx.gen.Metadata.ID

	codexStatus, err := DetectRestartRequired(worktreeStateDir, "codex", codexSessionGenID)
	if err != nil {
		t.Fatalf("DetectRestartRequired (codex): %v", err)
	}
	if !codexStatus.RestartRequired {
		t.Errorf("codex RestartRequired = false, want true: %+v", codexStatus)
	}

	claudeStatus, err := DetectRestartRequired(worktreeStateDir, "claude-code", claudeSessionGenID)
	if err != nil {
		t.Fatalf("DetectRestartRequired (claude-code): %v", err)
	}
	if claudeStatus.RestartRequired {
		t.Errorf("claude-code RestartRequired = true, want false (only codex was reactivated): %+v", claudeStatus)
	}
}

// buildClaudeCompileFixture is compileFixture's claude-code analogue, using
// newClaudeFixtureTree instead of newCodexFixtureTree.
func buildClaudeCompileFixture(t *testing.T, now time.Time) activateFixture {
	t.Helper()
	tr := newClaudeFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "CLAUDE.md"), "# claude instructions\n")
	obs, err := observe.Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req := CompileRequest{
		Worktree: tr.worktree(t),
		Hosts:    []HostCompileInput{{Detection: tr.detection("2.1.211"), Observations: obs}},
		Now:      now,
	}
	genID, err := CompileGenerationID(req)
	if err != nil {
		t.Fatalf("CompileGenerationID: %v", err)
	}
	outputDir := filepath.Join(t.TempDir(), "generations", DirSafeID(genID))
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)
	return activateFixture{req: req, gen: gen, outputDir: outputDir}
}
