package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// TestVerifyActivation_MissingArtifact_FailedArtifactsIsPathOnly is a
// regression test for a Copilot review finding on this PR: the struct
// comment on VerificationResult.FailedArtifacts promises "every artifact
// path (relative to the generation directory)", but the read-error branch
// previously appended a formatted "path: error" string instead, breaking
// that contract for exactly the callers this field exists for (anything
// that wants to compare FailedArtifacts entries against known-good paths
// programmatically, e.g. a future `omca doctor`/`omca bisect` consumer).
// Deleting the artifact outright (rather than tamperArtifact's
// content-swap) exercises the os.ReadFile error branch specifically, which
// TestVerifyActivation_DetectsTamperedArtifact's digest-mismatch branch
// does not reach.
func TestVerifyActivation_MissingArtifact_FailedArtifactsIsPathOnly(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	fx := compileFixture(t, worktreeStateDir, nil, nil, now)
	if err := SetPendingGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}
	if _, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: fx.req, Now: now}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	full := filepath.Join(fx.outputDir, codexConfigTOMLRelPath)
	if err := os.Chmod(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("chmod parent dir writable: %v", err)
	}
	if err := os.Remove(full); err != nil {
		t.Fatalf("removing artifact to force a read error: %v", err)
	}

	result, err := VerifyActivation(worktreeStateDir, "codex", now)
	if err != nil {
		t.Fatalf("VerifyActivation: %v", err)
	}
	if result.Passed {
		t.Fatal("Passed = true after deleting a compiled artifact, want false")
	}
	found := false
	for _, p := range result.FailedArtifacts {
		if p == codexConfigTOMLRelPath {
			found = true
		}
		if p != codexConfigTOMLRelPath {
			t.Errorf("FailedArtifacts entry %q is not a bare path (contract: FailedArtifacts lists paths only) -- got an entry that isn't the expected artifact path at all", p)
		}
	}
	if !found {
		t.Errorf("FailedArtifacts = %v, want it to include the bare path %q (not a formatted %q: <error> string)", result.FailedArtifacts, codexConfigTOMLRelPath, codexConfigTOMLRelPath)
	}
	if !strings.Contains(result.Detail, "no such file") && !strings.Contains(result.Detail, "no such file or directory") {
		// The error detail must still be discoverable somewhere -- just not
		// smuggled inside FailedArtifacts. Detail is the right place for it.
		t.Errorf("Detail = %q, want it to still explain the underlying read error somewhere", result.Detail)
	}
}

// TestVerifyActivation_EffectiveGraphCatchesUndiscoverableRendering is issue
// #70's own regression proof: a generation manifest that is perfectly
// internally consistent -- every artifact digest matches its own recorded
// content -- can still misrepresent what activation actually produced, when
// a (simulated) compiler bug renders an Included:true source at a path
// internal/observe's real discovery rules would never recognize as that
// concept again. The artifact-digest check alone (proven insufficient by
// this test's own mid-test assertion) cannot catch this; only the
// EffectiveGraph re-derivation this file adds can.
//
// The scenario: compile and activate a real generation carrying a genuine,
// correctly-rendered repository Instructions source (AGENTS.md, which
// internal/observe/rules.go's codexWorkspaceRules recognizes). Then, from
// OUTSIDE Compile -- the same "simulate the failure mode directly" approach
// tamperArtifact already uses for disk corruption -- rename the rendered
// file to a name codexWorkspaceRules does NOT recognize for the instruction
// concept (it only ever looks for "AGENTS.override.md"/"AGENTS.md", never
// an arbitrary filename) and update the manifest's own recorded artifact
// Path to match, keeping the artifact digest perfectly self-consistent
// throughout (identical bytes, identical digest, just a different,
// undiscoverable path). The manifest's Sources list still says, truthfully
// from the original compile, that this instruction was Included:true --
// nothing about the rename touches that entry, exactly the "internally
// consistent but wrong" scenario issue #70 describes.
func TestVerifyActivation_EffectiveGraphCatchesUndiscoverableRendering(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	fx := compileFixture(t, worktreeStateDir, nil, nil, now)
	if err := SetPendingGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}
	if _, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: fx.req, Now: now}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// Sanity: the compiled generation really did include a genuine
	// instruction source, Included:true -- otherwise this test would prove
	// nothing.
	foundInstructionSource := false
	for _, s := range fx.gen.Spec.Sources {
		if s.Host == "codex" && s.Concept == "instruction" && s.Included {
			foundInstructionSource = true
		}
	}
	if !foundInstructionSource {
		t.Fatal("fixture generation has no Included=true instruction source -- test setup is broken")
	}

	oldRel := filepath.Join("hosts", "codex", "cli", "instructions", "AGENTS.md")
	newRel := filepath.Join("hosts", "codex", "cli", "instructions", "NOTES-not-a-recognized-instructions-filename.md")
	oldFull := filepath.Join(fx.outputDir, oldRel)
	newFull := filepath.Join(fx.outputDir, newRel)

	content, err := os.ReadFile(oldFull)
	if err != nil {
		t.Fatalf("reading original rendered instruction file: %v", err)
	}
	if err := os.Chmod(filepath.Dir(oldFull), 0o755); err != nil {
		t.Fatalf("chmod instructions dir writable: %v", err)
	}
	if err := os.Rename(oldFull, newFull); err != nil {
		t.Fatalf("renaming rendered instruction file: %v", err)
	}

	manifestPath := filepath.Join(fx.outputDir, "manifest.json")
	if err := os.Chmod(manifestPath, 0o644); err != nil {
		t.Fatalf("chmod manifest.json writable: %v", err)
	}
	gen, err := ReadGenerationManifest(fx.outputDir)
	if err != nil {
		t.Fatalf("re-reading manifest: %v", err)
	}
	entry := gen.Spec.Hosts["codex"]
	renamedArtifact := false
	for i := range entry.Artifacts {
		if entry.Artifacts[i].Path == oldRel {
			entry.Artifacts[i].Path = newRel
			renamedArtifact = true
		}
	}
	if !renamedArtifact {
		t.Fatalf("manifest has no artifact entry for %s -- test setup assumption is wrong", oldRel)
	}
	gen.Spec.Hosts["codex"] = entry
	manifestBytes, err := json.MarshalIndent(gen, "", "  ")
	if err != nil {
		t.Fatalf("re-marshaling manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatalf("writing edited manifest: %v", err)
	}

	// Confirm the artifact-digest check ALONE would still pass here: the
	// renamed file's content is byte-identical, and the manifest's own
	// artifact entry now points at its new path with the SAME recorded
	// digest -- exactly the "internally consistent" property this whole
	// issue is about. domain.CanonicalDigest mirrors Compile's (and
	// VerifyActivation's digest loop's) own computation.
	renamedDigest, err := domain.CanonicalDigest(string(content))
	if err != nil {
		t.Fatalf("computing renamed file's digest: %v", err)
	}
	var recordedDigest string
	for _, a := range gen.Spec.Hosts["codex"].Artifacts {
		if a.Path == newRel {
			recordedDigest = a.Digest
		}
	}
	if recordedDigest != renamedDigest {
		t.Fatalf("recorded digest %q != renamed file's actual digest %q -- test setup did not keep the manifest internally consistent", recordedDigest, renamedDigest)
	}

	result, err := VerifyActivation(worktreeStateDir, "codex", now)
	if err != nil {
		t.Fatalf("VerifyActivation: %v", err)
	}
	if result.Passed {
		t.Fatal("Passed = true for a generation whose manifest claims an Included=true instruction source that internal/observe's real discovery rules can no longer find -- want false")
	}
	for _, p := range result.FailedArtifacts {
		if p == newRel || p == oldRel {
			t.Errorf("FailedArtifacts contains an artifact-digest-style path entry (%q) -- this scenario is specifically constructed so the artifact-digest check alone passes; only the effective-graph check should report a failure here", p)
		}
	}
	if !strings.Contains(result.Detail, "effective") && !strings.Contains(result.Detail, "graph") {
		t.Errorf("Detail = %q, want it to mention the effective-graph check as the source of this failure", result.Detail)
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
