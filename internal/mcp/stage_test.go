package mcp

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// --- fixtures --------------------------------------------------------------

// panicCompile is a CompileFunc that fails the test loudly if ComputeStage
// ever calls it -- used by every rejection test below to prove ComputeStage
// never reaches the one function capable of mutating anything on disk
// (CompileFunc's own doc comment: it is the sole channel through which a
// pending -- or, if misused, current -- generation could ever be written).
// A panic (rather than merely a call counter checked after the fact) fails
// at the exact point of misuse, with a stack trace naming it.
func panicCompile(t *testing.T) CompileFunc {
	return func(map[string]domain.HostActivation) (domain.Generation, map[string]domain.Generation, error) {
		t.Helper()
		t.Fatal("CompileFunc was called for a proposal ComputeStage should have rejected before ever reaching compile")
		return domain.Generation{}, nil, nil
	}
}

// recordingCompile is a CompileFunc that records every call's
// hostActivations argument and answers with a fixed (pending,
// currentByHost) pair -- it deliberately builds currentByHost from a
// caller-supplied, already-existing map (never mutating it, never writing
// through a "current" pointer of any kind) so TestComputeStage_
// NeverMutatesCurrent can assert that map is byte-identical before and
// after a ComputeStage call.
type recordingCompile struct {
	calls   int
	lastArg map[string]domain.HostActivation
	pending domain.Generation
	current map[string]domain.Generation
	err     error
}

func (r *recordingCompile) fn() CompileFunc {
	return func(hostActivations map[string]domain.HostActivation) (domain.Generation, map[string]domain.Generation, error) {
		r.calls++
		r.lastArg = hostActivations
		if r.err != nil {
			return domain.Generation{}, nil, r.err
		}
		return r.pending, r.current, nil
	}
}

// stagePendingGeneration builds a minimal domain.Generation naming host and
// carrying sources -- enough for runtime.DiffProposedChanges to project a
// diff from; ComputeStage never validates the Generation's own schema
// (domain.ValidateGeneration), only reads Metadata.ID/Spec.Hosts/
// Spec.Sources, so this fixture stays deliberately minimal.
func stagePendingGeneration(host string, sources []domain.GenerationSourceEntry) domain.Generation {
	return domain.Generation{
		Metadata: domain.GenerationMetadata{ID: "generation:sha256:pending-fixture"},
		Spec: domain.GenerationSpec{
			Hosts:   map[string]domain.GenerationHostEntry{host: {}},
			Sources: sources,
		},
	}
}

func newlyIncludedSkillSource(host, skill string) domain.GenerationSourceEntry {
	return domain.GenerationSourceEntry{Concept: "skill", Source: skill, Host: host, Included: true, Scope: "desired-state", Reason: "resolved desired state (intent=\"DEFAULT\"): resolved from company Profile"}
}

// --- AUTO_STAGE-only ---------------------------------------------------

func TestComputeStage_AutoStage_CompilesAndReturnsDiff(t *testing.T) {
	rec := &recordingCompile{
		pending: stagePendingGeneration("codex", []domain.GenerationSourceEntry{newlyIncludedSkillSource("codex", "code-review")}),
		current: map[string]domain.Generation{
			"codex": {Metadata: domain.GenerationMetadata{ID: "generation:sha256:current-fixture"}, Spec: domain.GenerationSpec{Sources: nil}},
		},
	}
	result, err := ComputeStage(defaultProposeContext(), rec.fn(), validProposal(t))
	if err != nil {
		t.Fatalf("ComputeStage: %v", err)
	}
	if rec.calls != 1 {
		t.Fatalf("CompileFunc was called %d times, want exactly 1", rec.calls)
	}
	if result.PendingGenerationID != "generation:sha256:pending-fixture" {
		t.Errorf("PendingGenerationID = %q, want %q", result.PendingGenerationID, "generation:sha256:pending-fixture")
	}
	if len(result.Hosts) != 1 || result.Hosts[0].Host != "codex" {
		t.Fatalf("Hosts = %+v, want exactly one codex entry", result.Hosts)
	}
	hr := result.Hosts[0]
	if len(hr.Diff) != 1 || hr.Diff[0].AssetID != "code-review" {
		t.Errorf("Diff = %+v, want exactly one change naming code-review", hr.Diff)
	}
	if !hr.RestartRequired {
		t.Error("RestartRequired = false, want true (an existing current generation + a non-empty diff means activating pending would move current out from under a running session)")
	}
}

func TestComputeStage_AutoStage_NoExistingCurrent_RestartNotRequired(t *testing.T) {
	rec := &recordingCompile{
		pending: stagePendingGeneration("codex", []domain.GenerationSourceEntry{newlyIncludedSkillSource("codex", "code-review")}),
		current: map[string]domain.Generation{}, // codex has never had a current generation in this worktree
	}
	result, err := ComputeStage(defaultProposeContext(), rec.fn(), validProposal(t))
	if err != nil {
		t.Fatalf("ComputeStage: %v", err)
	}
	if result.Hosts[0].RestartRequired {
		t.Error("RestartRequired = true, want false (nothing is running against a generation that never existed)")
	}
	if len(result.Hosts[0].Diff) != 1 {
		t.Errorf("Diff = %+v, want exactly one change even though restart is not required", result.Hosts[0].Diff)
	}
}

func TestComputeStage_HostActivations_PassedToCompileFunc(t *testing.T) {
	rec := &recordingCompile{pending: stagePendingGeneration("codex", nil), current: map[string]domain.Generation{}}
	if _, err := ComputeStage(defaultProposeContext(), rec.fn(), validProposal(t)); err != nil {
		t.Fatalf("ComputeStage: %v", err)
	}
	ha, ok := rec.lastArg["codex"]
	if !ok {
		t.Fatalf("CompileFunc's hostActivations has no \"codex\" entry: %v", rec.lastArg)
	}
	if len(ha.Enable.Skills) != 1 || ha.Enable.Skills[0] != "code-review" {
		t.Errorf("hostActivations[codex].Enable.Skills = %v, want [code-review]", ha.Enable.Skills)
	}
}

// TestComputeStage_NeverMutatesCurrent proves the AC's explicit "never
// mutates current (test)" property at ComputeStage's own layer: compile's
// currentByHost map (standing in for whatever a real CompileFunc would have
// read, read-only, from disk) is byte-identical before and after a
// successful ComputeStage call -- ComputeStage has no code path that could
// write through it (it only ever reads Metadata.ID off of it, to compute
// restartRequired), and this test proves that stays true even as ComputeStage
// evolves.
func TestComputeStage_NeverMutatesCurrent(t *testing.T) {
	before := domain.Generation{Metadata: domain.GenerationMetadata{ID: "generation:sha256:current-fixture"}, Spec: domain.GenerationSpec{Sources: []domain.GenerationSourceEntry{newlyIncludedSkillSource("codex", "already-active")}}}
	currentMap := map[string]domain.Generation{"codex": before}
	rec := &recordingCompile{
		pending: stagePendingGeneration("codex", []domain.GenerationSourceEntry{newlyIncludedSkillSource("codex", "already-active"), newlyIncludedSkillSource("codex", "code-review")}),
		current: currentMap,
	}
	if _, err := ComputeStage(defaultProposeContext(), rec.fn(), validProposal(t)); err != nil {
		t.Fatalf("ComputeStage: %v", err)
	}
	if got := currentMap["codex"]; !generationsEqual(got, before) {
		t.Errorf("currentByHost[\"codex\"] changed across the ComputeStage call: got %+v, want unchanged %+v", got, before)
	}
}

func generationsEqual(a, b domain.Generation) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

// --- AUTO_STAGE-only: every other class is a hard rejection ---------------

func TestComputeStage_RejectsAt_ConfirmRequired_NamesClass(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Changes[0].Patch = mustActivationPatch(t, "codex", nil, []string{"some-mcp-server"})
	_, err := ComputeStage(defaultProposeContext(), panicCompile(t), rp)
	if err == nil {
		t.Fatal("ComputeStage: want an error for a CONFIRM_REQUIRED proposal, got nil")
	}
	var stageErr *StageRejectedError
	if !errors.As(err, &stageErr) {
		t.Fatalf("error is %T (%v), want *StageRejectedError", err, err)
	}
	if stageErr.Class != domain.RepairConfirmRequired {
		t.Errorf("Class = %q, want %q", stageErr.Class, domain.RepairConfirmRequired)
	}
}

func TestComputeStage_RejectsAt_ReviewableDiff_NamesClass(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Changes = []domain.RepairChange{
		{TargetKind: "Profile", TargetID: "company:baseline", Patch: mustProfilePatch(t, `{"assets":{"skills":[{"id":"code-review","intent":"DEFAULT"}]}}`)},
	}
	_, err := ComputeStage(defaultProposeContext(), panicCompile(t), rp)
	if err == nil {
		t.Fatal("ComputeStage: want an error for a REVIEWABLE_DIFF proposal, got nil")
	}
	var stageErr *StageRejectedError
	if !errors.As(err, &stageErr) {
		t.Fatalf("error is %T (%v), want *StageRejectedError", err, err)
	}
	if stageErr.Class != domain.RepairReviewableDiff {
		t.Errorf("Class = %q, want %q", stageErr.Class, domain.RepairReviewableDiff)
	}
}

func TestComputeStage_RejectsAt_Prohibited_NamesRiskGate(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Confirmation = domain.RepairProhibited
	_, err := ComputeStage(defaultProposeContext(), panicCompile(t), rp)
	if err == nil {
		t.Fatal("ComputeStage: want an error for a PROHIBITED proposal, got nil")
	}
	var stageErr *StageRejectedError
	if !errors.As(err, &stageErr) {
		t.Fatalf("error is %T (%v), want *StageRejectedError", err, err)
	}
	var proposeErr *ProposeRejectedError
	if !errors.As(stageErr.Underlying, &proposeErr) {
		t.Fatalf("Underlying is %T (%v), want it to wrap a *ProposeRejectedError", stageErr.Underlying, stageErr.Underlying)
	}
	if proposeErr.Gate != "risk" {
		t.Errorf("Underlying gate = %q, want %q", proposeErr.Gate, "risk")
	}
}

// TestComputeStage_FullyRevalidates_StaleFingerprint proves the round-4
// audit's CAS-style re-check: ComputeStage re-runs the fingerprint gate
// against pc's CURRENT Artifact, not merely trusting that the proposal was
// valid whenever it was first proposed -- a proposal whose reportFingerprint
// no longer matches (the underlying state moved since propose time) is
// rejected here exactly like omca_propose itself would reject it, and
// compile is never reached.
func TestComputeStage_FullyRevalidates_StaleFingerprint(t *testing.T) {
	staleArtifact := proposeFixtureArtifact()
	staleArtifact.Report.Spec.Fingerprint = anotherFingerprint // the report moved since this proposal was generated
	pc := ProposeContext{Artifact: staleArtifact, CapabilityFor: staticCapability(true)}

	_, err := ComputeStage(pc, panicCompile(t), validProposal(t))
	if err == nil {
		t.Fatal("ComputeStage: want an error for a stale reportFingerprint, got nil")
	}
	var stageErr *StageRejectedError
	if !errors.As(err, &stageErr) {
		t.Fatalf("error is %T (%v), want *StageRejectedError", err, err)
	}
	var proposeErr *ProposeRejectedError
	if !errors.As(stageErr.Underlying, &proposeErr) || proposeErr.Gate != "fingerprint" {
		t.Errorf("Underlying = %v, want a *ProposeRejectedError at the fingerprint gate", stageErr.Underlying)
	}
}

func TestComputeStage_NilCompileFunc_Errors(t *testing.T) {
	if _, err := ComputeStage(defaultProposeContext(), nil, validProposal(t)); err == nil {
		t.Fatal("ComputeStage: want an error when no CompileFunc is wired, got nil")
	}
}

func TestComputeStage_CompileFuncError_IsWrapped(t *testing.T) {
	rec := &recordingCompile{err: errors.New("boom: disk full")}
	_, err := ComputeStage(defaultProposeContext(), rec.fn(), validProposal(t))
	if err == nil {
		t.Fatal("ComputeStage: want an error when CompileFunc fails, got nil")
	}
}

// --- tool wiring -------------------------------------------------------

func TestStageToolEntry_RegisteredAndDispatches(t *testing.T) {
	rec := &recordingCompile{pending: stagePendingGeneration("codex", nil), current: map[string]domain.Generation{}}
	entry := StageToolEntry(
		func() (report.Artifact, error) { return proposeFixtureArtifact(), nil },
		staticCapability(true),
		rec.fn(),
	)
	if entry.definition.Name != toolNameStage {
		t.Fatalf("Name = %q, want %q", entry.definition.Name, toolNameStage)
	}
	argsJSON, err := json.Marshal(StageArguments{Proposal: validProposal(t)})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	resultAny, err := entry.handler(argsJSON)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if _, ok := resultAny.(StageResult); !ok {
		t.Fatalf("handler returned %T, want StageResult", resultAny)
	}
	if rec.calls != 1 {
		t.Errorf("CompileFunc was called %d times, want exactly 1", rec.calls)
	}
}

func TestStageToolEntry_RejectedProposal_IsToolLevelError(t *testing.T) {
	entry := StageToolEntry(
		func() (report.Artifact, error) { return proposeFixtureArtifact(), nil },
		staticCapability(true),
		panicCompile(t),
	)
	rp := validProposal(t)
	rp.Spec.Changes[0].Patch = mustActivationPatch(t, "codex", nil, []string{"some-mcp-server"})
	argsJSON, _ := json.Marshal(StageArguments{Proposal: rp})
	_, err := entry.handler(argsJSON)
	if err == nil {
		t.Fatal("handler: want an error for a CONFIRM_REQUIRED proposal, got nil")
	}
}
