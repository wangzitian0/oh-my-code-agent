package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// codexConfigTOMLRelPath is the one artifact every codex generation always
// renders regardless of Profiles (compile_full_test.go's
// TestCompile_Permission_DENIED_NeverWeakened already relies on this same
// path existing unconditionally) -- a stable, always-present artifact this
// file's tests tamper with to simulate a compiled generation whose on-disk
// output no longer matches its own manifest.
var codexConfigTOMLRelPath = filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")

// tamperArtifact overwrites generationDir/relPath's content, simulating
// disk corruption or an out-of-band write to an otherwise read-only,
// content-addressed generation tree -- Compile's own makeTreeReadOnly
// leaves the file mode-locked, so this chmods it writable first.
func tamperArtifact(t *testing.T, generationDir, relPath string) {
	t.Helper()
	full := filepath.Join(generationDir, relPath)
	if err := os.Chmod(full, 0o644); err != nil {
		t.Fatalf("tamperArtifact: chmod: %v", err)
	}
	if err := os.WriteFile(full, []byte("tampered, does not match the manifest\n"), 0o644); err != nil {
		t.Fatalf("tamperArtifact: write: %v", err)
	}
}

// TestVerifyActivation_Passes proves the healthy path: a freshly activated,
// untampered generation verifies clean.
func TestVerifyActivation_Passes(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	fx := compileFixture(t, worktreeStateDir, nil, nil, now)
	if err := SetPendingGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}
	if _, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: fx.req, Now: now}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	result, err := VerifyActivation(worktreeStateDir, "codex", now)
	if err != nil {
		t.Fatalf("VerifyActivation: %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Detail: %s; FailedArtifacts: %v", result.Detail, result.FailedArtifacts)
	}
	if result.GenerationID != fx.gen.Metadata.ID {
		t.Errorf("GenerationID = %q, want %q", result.GenerationID, fx.gen.Metadata.ID)
	}
	if len(result.FailedArtifacts) != 0 {
		t.Errorf("FailedArtifacts = %v, want none", result.FailedArtifacts)
	}
}

// TestVerifyActivation_DetectsTamperedArtifact proves VerifyActivation
// actually catches a compiled generation whose on-disk artifact tree has
// diverged from its own manifest since compile time -- the failure mode
// this whole mechanism exists to detect.
func TestVerifyActivation_DetectsTamperedArtifact(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	fx := compileFixture(t, worktreeStateDir, nil, nil, now)
	if err := SetPendingGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}
	if _, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: fx.req, Now: now}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	tamperArtifact(t, fx.outputDir, codexConfigTOMLRelPath)

	result, err := VerifyActivation(worktreeStateDir, "codex", now)
	if err != nil {
		t.Fatalf("VerifyActivation: %v", err)
	}
	if result.Passed {
		t.Fatal("Passed = true after tampering with a compiled artifact, want false")
	}
	found := false
	for _, p := range result.FailedArtifacts {
		if p == codexConfigTOMLRelPath {
			found = true
		}
	}
	if !found {
		t.Errorf("FailedArtifacts = %v, want it to include %q", result.FailedArtifacts, codexConfigTOMLRelPath)
	}
}

// TestVerifyActivation_NoCurrentGeneration_ReturnsError proves
// VerifyActivation refuses (rather than silently reporting "verified" for
// nothing) when host has no current generation.
func TestVerifyActivation_NoCurrentGeneration_ReturnsError(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	if _, err := VerifyActivation(t.TempDir(), "codex", now); err == nil {
		t.Fatal("VerifyActivation with no current generation: want error, got nil")
	}
}

// TestActivateAndVerify_VerificationPasses_ActivatesNormally proves the
// healthy path: ActivateAndVerify behaves exactly like a plain Activate
// when post-activation verification passes -- no rollback, no
// "verification-failed" ledger entry.
func TestActivateAndVerify_VerificationPasses_ActivatesNormally(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")

	fx := compileFixture(t, worktreeStateDir, nil, nil, now)
	if err := SetPendingGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}

	result, err := ActivateAndVerify(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: fx.req, Now: now}, generationsRoot)
	if err != nil {
		t.Fatalf("ActivateAndVerify: %v", err)
	}
	if result.RolledBack {
		t.Fatal("RolledBack = true on the healthy path, want false")
	}
	if !result.Verification.Passed {
		t.Errorf("Verification.Passed = false, want true")
	}
	if result.Activation.ActivatedGenerationID != fx.gen.Metadata.ID {
		t.Errorf("Activation.ActivatedGenerationID = %q, want %q", result.Activation.ActivatedGenerationID, fx.gen.Metadata.ID)
	}

	gotCurrent, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotCurrent != fx.outputDir {
		t.Errorf("CurrentGenerationDir = %q, want %q", gotCurrent, fx.outputDir)
	}

	entries, err := ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	for _, e := range entries {
		if e.Kind == "verification-failed" || e.Kind == "rolledback" {
			t.Errorf("unexpected ledger entry on the healthy path: %+v", e)
		}
	}
}

// TestActivateAndVerify_FailedVerification_TriggersAutomatedRollback_BothLedgered
// is this PR's own headline AC (issue #28): "Failed post-activation
// verification triggers automated rollback to the parent; both events are
// ledgered." It activates a baseline (parent) generation, then activates a
// second (child) generation whose on-disk artifact tree is tampered with
// immediately after Activate's switch -- simulating a generation that
// somehow diverged from its own manifest right as it became current -- and
// proves: verification fails, Rollback restores the parent automatically,
// and the Ledger records both the verification failure and the
// restoration, in that order.
func TestActivateAndVerify_FailedVerification_TriggersAutomatedRollback_BothLedgered(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")

	parentFx := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "code-review")}, nil, now)
	if err := SetPendingGeneration(worktreeStateDir, "codex", parentFx.outputDir, parentFx.gen, parentFx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration (parent): %v", err)
	}
	if _, err := ActivateAndVerify(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: parentFx.req, Now: now}, generationsRoot); err != nil {
		t.Fatalf("activating the baseline parent generation: %v", err)
	}

	parentID := parentFx.gen.Metadata.ID
	childFx := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "deep-refactor")}, &parentID, now.Add(time.Minute))
	if childFx.gen.Metadata.ID == parentFx.gen.Metadata.ID {
		t.Fatal("fixture setup did not actually vary content between parent and child generations")
	}
	if err := SetPendingGeneration(worktreeStateDir, "codex", childFx.outputDir, childFx.gen, childFx.req.Hosts[0].Detection, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetPendingGeneration (child): %v", err)
	}

	// Tamper the child's artifact tree BEFORE activating it: Activate's own
	// CAS check only ever recomputes a digest over DESIRED-STATE INPUTS
	// (freshSourceDigest), never over the generation directory's own
	// on-disk output, so it does not (and must not) catch this -- proving
	// VerifyActivation is genuinely needed, not redundant with the
	// pre-existing CAS check.
	tamperArtifact(t, childFx.outputDir, codexConfigTOMLRelPath)

	result, err := ActivateAndVerify(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: childFx.req, Now: now.Add(time.Minute)}, generationsRoot)
	if err != nil {
		t.Fatalf("ActivateAndVerify: want a successful automated recovery (nil error), got: %v", err)
	}
	if result.Verification.Passed {
		t.Fatal("Verification.Passed = true despite the tampered artifact, want false")
	}
	if !result.RolledBack {
		t.Fatal("RolledBack = false, want true")
	}
	if result.Rollback == nil {
		t.Fatal("Rollback = nil despite RolledBack = true")
	}
	if result.Rollback.RestoredGenerationID != parentID {
		t.Errorf("Rollback.RestoredGenerationID = %q, want the parent %q", result.Rollback.RestoredGenerationID, parentID)
	}

	gotCurrent, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotCurrent != parentFx.outputDir {
		t.Errorf("CurrentGenerationDir after automated rollback = %q, want the parent %q", gotCurrent, parentFx.outputDir)
	}

	entries, err := ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	var verifyFailedIdx, rolledBackIdx = -1, -1
	for i, e := range entries {
		if e.Kind == "verification-failed" && e.GenerationID == childFx.gen.Metadata.ID {
			verifyFailedIdx = i
		}
		if e.Kind == "rolledback" && e.GenerationID == parentID {
			rolledBackIdx = i
		}
	}
	if verifyFailedIdx == -1 {
		t.Errorf("ledger has no 'verification-failed' entry for the child generation %s: %+v", childFx.gen.Metadata.ID, entries)
	}
	if rolledBackIdx == -1 {
		t.Errorf("ledger has no 'rolledback' entry for the restored parent %s: %+v", parentID, entries)
	}
	if verifyFailedIdx != -1 && rolledBackIdx != -1 && verifyFailedIdx > rolledBackIdx {
		t.Errorf("'verification-failed' (index %d) must be ledgered before 'rolledback' (index %d)", verifyFailedIdx, rolledBackIdx)
	}
}

// TestActivateAndVerify_FailedVerification_NoParent_LedgersFailureReturnsError
// proves the honest edge case: a first-ever activation (no parent to roll
// back to) that fails verification still gets its failure ledgered, but
// ActivateAndVerify returns a clear error rather than silently pretending
// recovery happened -- docs/project/roadmap.md's M5 exit gate line "failed
// verification leaves a recoverable previous generation" cannot apply when
// there is no previous generation at all.
func TestActivateAndVerify_FailedVerification_NoParent_LedgersFailureReturnsError(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")

	fx := compileFixture(t, worktreeStateDir, nil, nil, now)
	if err := SetPendingGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}
	tamperArtifact(t, fx.outputDir, codexConfigTOMLRelPath)

	result, err := ActivateAndVerify(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: fx.req, Now: now}, generationsRoot)
	if err == nil {
		t.Fatal("ActivateAndVerify with no parent to roll back to: want a non-nil error, got nil")
	}
	if result.RolledBack {
		t.Error("RolledBack = true despite there being no parent generation to roll back to")
	}

	entries, ledgerErr := ReadLedger(worktreeStateDir, "codex")
	if ledgerErr != nil {
		t.Fatalf("ReadLedger: %v", ledgerErr)
	}
	found := false
	for _, e := range entries {
		if e.Kind == "verification-failed" && e.GenerationID == fx.gen.Metadata.ID {
			found = true
		}
		if e.Kind == "rolledback" {
			t.Errorf("unexpected 'rolledback' ledger entry when no parent exists: %+v", e)
		}
	}
	if !found {
		t.Errorf("ledger has no 'verification-failed' entry even though rollback could not proceed: %+v", entries)
	}
}
