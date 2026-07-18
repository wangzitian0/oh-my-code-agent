package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
)

// --- fixture builders -----------------------------------------------------

// proposeFixtureFingerprint is a fixed, valid "sha256:<64 hex>" digest
// (domain.IsCanonicalDigest's own shape) used as the bound report's
// fingerprint across this file's tests -- computed once via
// domain.CanonicalDigest rather than hand-typing 64 hex characters, and
// reused everywhere a "matches the bound report" fingerprint is needed.
var proposeFixtureFingerprint = mustDigest("propose_test.go fixture fingerprint")

// anotherFingerprint is a second, equally valid but DIFFERENT digest --
// used by the fingerprint-mismatch rejection test.
var anotherFingerprint = mustDigest("propose_test.go a different fingerprint")

func mustDigest(seed string) string {
	d, err := domain.CanonicalDigest(seed)
	if err != nil {
		panic(err)
	}
	return d
}

// proposeFixtureArtifact builds a small report.Artifact for propose_test.go:
// Report.Spec.Fingerprint set to proposeFixtureFingerprint, and one host
// ("codex") whose already-resolved Desired Graph (report.HostDebug.Desired)
// marks "code-review" active/DEFAULT and "denied-skill" DENIED -- exactly
// the resolve.ResolvedState shape ComputePropose's policy gate reads.
func proposeFixtureArtifact() report.Artifact {
	return report.Artifact{
		Report: domain.Report{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       "Report",
			Metadata:   domain.ReportMetadata{ID: "report:worktree:sha256:fixture", Worktree: "worktree:sha256:fixture"},
			Spec:       domain.ReportSpec{Fingerprint: proposeFixtureFingerprint},
		},
		Debug: map[string]report.HostDebug{
			"codex": {
				Desired: resolve.ResolvedState{
					Host: "codex",
					Assets: []resolve.ResolvedAsset{
						{Kind: resolve.KindSkill, ID: "code-review", Active: true, Intent: domain.IntentDefault, Reason: "resolved from company Profile"},
						{Kind: resolve.KindSkill, ID: "denied-skill", Active: false, Intent: domain.IntentDenied, Reason: "DENIED by company policy"},
					},
				},
			},
		},
	}
}

// staticCapability returns a CapabilityFunc that always answers the same
// (ops, ok) pair -- enough for propose_test.go's gate-isolation tests, which
// are not re-testing knowledge.Repository.Resolve itself.
func staticCapability(ok bool) CapabilityFunc {
	return func(host, concept string) (domain.CapabilityOps, bool) {
		if !ok {
			return domain.CapabilityOps{}, false
		}
		return domain.CapabilityOps{Resolve: domain.CapabilityExact, Compile: domain.CapabilityExact}, true
	}
}

func defaultProposeContext() ProposeContext {
	return ProposeContext{Artifact: proposeFixtureArtifact(), CapabilityFor: staticCapability(true)}
}

// activationPatchJSON builds a RepairChange.Patch map -- as it would
// actually arrive over the wire, decoded by encoding/json into
// map[string]any -- for an Activation change enabling skill on host.
func activationPatchJSON(t *testing.T, host, skill string) map[string]any {
	t.Helper()
	raw := []byte(`{"spec":{"hosts":{"` + host + `":{"enable":{"skills":["` + skill + `"]}}}}}`)
	var patch map[string]any
	if err := json.Unmarshal(raw, &patch); err != nil {
		t.Fatalf("building activation patch fixture: %v", err)
	}
	return patch
}

// validProposal is a fully-gate-passing RepairProposal: matches
// proposeFixtureArtifact's fingerprint, targets Activation, enables
// "code-review" on "codex" (AUTO_STAGE-reachable per classifyChange).
func validProposal(t *testing.T) domain.RepairProposal {
	t.Helper()
	return domain.RepairProposal{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "RepairProposal",
		Metadata:   domain.Metadata{ID: "repair:test:enable-code-review"},
		Spec: domain.RepairProposalSpec{
			ReportFingerprint: proposeFixtureFingerprint,
			Author:            domain.RepairAuthor{Kind: "llm", Model: "claude-sonnet-5"},
			Rationale:         "enable the already-reviewed code-review skill",
			Ownership:         domain.OwnershipManaged,
			Changes: []domain.RepairChange{
				{TargetKind: "Activation", TargetID: "worktree:sha256:fixture", Patch: activationPatchJSON(t, "codex", "code-review")},
			},
			Confirmation: domain.RepairAutoStage,
		},
	}
}

func rejectedGate(t *testing.T, err error) string {
	t.Helper()
	var rejErr *ProposeRejectedError
	if !errors.As(err, &rejErr) {
		t.Fatalf("error is %T (%v), want *ProposeRejectedError", err, err)
	}
	return rejErr.Gate
}

// --- happy path -------------------------------------------------------

func TestComputePropose_Valid_AcceptedAsAutoStage(t *testing.T) {
	result, err := ComputePropose(defaultProposeContext(), validProposal(t))
	if err != nil {
		t.Fatalf("ComputePropose: %v", err)
	}
	if !result.Accepted {
		t.Fatal("Accepted = false, want true")
	}
	if result.Confirmation != domain.RepairAutoStage {
		t.Errorf("Confirmation = %q, want %q", result.Confirmation, domain.RepairAutoStage)
	}
	if result.Explanation == "" {
		t.Error("Explanation is empty")
	}
}

func TestComputePropose_EnablingMCPServer_ClassifiesConfirmRequired_NotRejected(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Changes[0].Patch = mustActivationPatch(t, "codex", nil, []string{"some-mcp-server"})
	result, err := ComputePropose(defaultProposeContext(), rp)
	if err != nil {
		t.Fatalf("ComputePropose: %v (an MCP-server-enabling proposal is a valid, ACCEPTED proposal classified CONFIRM_REQUIRED -- not a rejection)", err)
	}
	if result.Confirmation != domain.RepairConfirmRequired {
		t.Errorf("Confirmation = %q, want %q", result.Confirmation, domain.RepairConfirmRequired)
	}
}

func TestComputePropose_ProfileTarget_ClassifiesReviewableDiff_NotRejected(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Changes = []domain.RepairChange{
		{TargetKind: "Profile", TargetID: "company:baseline", Patch: mustProfilePatch(t, `{"assets":{"skills":[{"id":"code-review","intent":"DEFAULT"}]}}`)},
	}
	result, err := ComputePropose(defaultProposeContext(), rp)
	if err != nil {
		t.Fatalf("ComputePropose: %v", err)
	}
	if result.Confirmation != domain.RepairReviewableDiff {
		t.Errorf("Confirmation = %q, want %q", result.Confirmation, domain.RepairReviewableDiff)
	}
}

func TestComputePropose_ProfilePermissionExpansion_ClassifiesConfirmRequired(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Changes = []domain.RepairChange{
		{TargetKind: "Profile", TargetID: "company:baseline", Patch: mustProfilePatch(t, `{"policy":{"permissions":{"network":{"intent":"REQUIRED","value":"allow"}}}}`)},
	}
	result, err := ComputePropose(defaultProposeContext(), rp)
	if err != nil {
		t.Fatalf("ComputePropose: %v", err)
	}
	if result.Confirmation != domain.RepairConfirmRequired {
		t.Errorf("Confirmation = %q, want %q (permission expansion always confirms, docs/product/requirements.md §7)", result.Confirmation, domain.RepairConfirmRequired)
	}
}

// mustActivationPatch builds an Activation patch naming host with the given
// enable skill/mcpServer IDs (either may be nil).
func mustActivationPatch(t *testing.T, host string, skills, mcpServers []string) map[string]any {
	t.Helper()
	spec := domain.ActivationSpec{
		Hosts: map[string]domain.HostActivation{
			host: {Enable: domain.ActivationSelection{Skills: skills, MCPServers: mcpServers}},
		},
	}
	raw, err := json.Marshal(specPatch[domain.ActivationSpec]{Spec: spec})
	if err != nil {
		t.Fatalf("marshaling activation patch fixture: %v", err)
	}
	var patch map[string]any
	if err := json.Unmarshal(raw, &patch); err != nil {
		t.Fatalf("decoding activation patch fixture: %v", err)
	}
	return patch
}

func mustProfilePatch(t *testing.T, specJSON string) map[string]any {
	t.Helper()
	raw := []byte(`{"spec":` + specJSON + `}`)
	var patch map[string]any
	if err := json.Unmarshal(raw, &patch); err != nil {
		t.Fatalf("building profile patch fixture: %v", err)
	}
	return patch
}

// --- gate 1: schema -----------------------------------------------------

func TestComputePropose_RejectsAt_Schema_BadTargetKind(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Changes[0].TargetKind = "NotARealTargetKind"
	_, err := ComputePropose(defaultProposeContext(), rp)
	if err == nil {
		t.Fatal("ComputePropose: want an error for an invalid targetKind, got nil")
	}
	if gate := rejectedGate(t, err); gate != "schema" {
		t.Errorf("Gate = %q, want %q", gate, "schema")
	}
}

func TestComputePropose_RejectsAt_Schema_UnknownPatchField(t *testing.T) {
	rp := validProposal(t)
	// A patch field validateChangeShape's strict decode does not recognize --
	// domain.ActivationSpec has no "unknownField".
	rp.Spec.Changes[0].Patch = map[string]any{"spec": map[string]any{"unknownField": true}}
	_, err := ComputePropose(defaultProposeContext(), rp)
	if err == nil {
		t.Fatal("ComputePropose: want an error for a patch with an unrecognized field, got nil")
	}
	if gate := rejectedGate(t, err); gate != "schema" {
		t.Errorf("Gate = %q, want %q", gate, "schema")
	}
}

// --- gate 2: fingerprint --------------------------------------------------

func TestComputePropose_RejectsAt_Fingerprint_Mismatch(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.ReportFingerprint = anotherFingerprint
	_, err := ComputePropose(defaultProposeContext(), rp)
	if err == nil {
		t.Fatal("ComputePropose: want an error for a mismatched reportFingerprint, got nil")
	}
	if gate := rejectedGate(t, err); gate != "fingerprint" {
		t.Errorf("Gate = %q, want %q", gate, "fingerprint")
	}
}

// --- gate 3: ownership -----------------------------------------------------

func TestComputePropose_RejectsAt_Ownership_NotManaged(t *testing.T) {
	for _, owner := range []domain.Ownership{domain.OwnershipObserved, domain.OwnershipExternal, domain.OwnershipPassthrough, domain.OwnershipPatched} {
		t.Run(string(owner), func(t *testing.T) {
			rp := validProposal(t)
			rp.Spec.Ownership = owner
			_, err := ComputePropose(defaultProposeContext(), rp)
			if err == nil {
				t.Fatalf("ComputePropose: want an error for ownership %q, got nil", owner)
			}
			if gate := rejectedGate(t, err); gate != "ownership" {
				t.Errorf("Gate = %q, want %q", gate, "ownership")
			}
		})
	}
}

// --- gate 4: capability -----------------------------------------------------

func TestComputePropose_RejectsAt_Capability_UnqualifiedHost(t *testing.T) {
	pc := ProposeContext{Artifact: proposeFixtureArtifact(), CapabilityFor: staticCapability(false)}
	_, err := ComputePropose(pc, validProposal(t))
	if err == nil {
		t.Fatal("ComputePropose: want an error when CapabilityFor proves nothing, got nil")
	}
	if gate := rejectedGate(t, err); gate != "capability" {
		t.Errorf("Gate = %q, want %q", gate, "capability")
	}
}

func TestComputePropose_RejectsAt_Capability_NilCapabilityFunc(t *testing.T) {
	pc := ProposeContext{Artifact: proposeFixtureArtifact(), CapabilityFor: nil}
	_, err := ComputePropose(pc, validProposal(t))
	if err == nil {
		t.Fatal("ComputePropose: want an error when no CapabilityFunc is wired, got nil")
	}
	if gate := rejectedGate(t, err); gate != "capability" {
		t.Errorf("Gate = %q, want %q", gate, "capability")
	}
}

// --- gate 5: policy -----------------------------------------------------

func TestComputePropose_RejectsAt_Policy_ContradictsDenied(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Changes[0].Patch = activationPatchJSON(t, "codex", "denied-skill")
	_, err := ComputePropose(defaultProposeContext(), rp)
	if err == nil {
		t.Fatal("ComputePropose: want an error for a proposal that contradicts an already-resolved DENIED outcome, got nil")
	}
	if gate := rejectedGate(t, err); gate != "policy" {
		t.Errorf("Gate = %q, want %q", gate, "policy")
	}
}

// TestComputePropose_RejectsAt_Policy_NoDesiredStateForHost proves the
// Copilot-review fix: the policy gate previously only ran its DENIED-
// contradiction check inside `if hd, ok := pc.Artifact.Debug[host]; ok`,
// silently skipping the check entirely (fail-open) whenever the bound
// report had no Debug entry for the target host at all -- a proposal could
// enable an asset on a host the policy gate never actually examined.
// "claude-code" is absent from proposeFixtureArtifact's Debug map (only
// "codex" is populated), so this must now fail closed instead of silently
// passing.
func TestComputePropose_RejectsAt_Policy_NoDesiredStateForHost(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Changes[0].Patch = activationPatchJSON(t, "claude-code", "code-review")
	_, err := ComputePropose(defaultProposeContext(), rp)
	if err == nil {
		t.Fatal("ComputePropose: want an error when the bound report has no Desired state for the target host, got nil")
	}
	if gate := rejectedGate(t, err); gate != "policy" {
		t.Errorf("Gate = %q, want %q", gate, "policy")
	}
}

// --- gate 6: risk (PROHIBITED short-circuit) -------------------------------

func TestComputePropose_RejectsAt_Risk_ProhibitedSelfDeclaration(t *testing.T) {
	rp := validProposal(t)
	rp.Spec.Confirmation = domain.RepairProhibited
	_, err := ComputePropose(defaultProposeContext(), rp)
	if err == nil {
		t.Fatal("ComputePropose: want an error for spec.confirmation=PROHIBITED, got nil")
	}
	if gate := rejectedGate(t, err); gate != "risk" {
		t.Errorf("Gate = %q, want %q", gate, "risk")
	}
}

// --- multi-change severity -------------------------------------------------

func TestComputePropose_MultipleChanges_OverallIsMostRestrictive(t *testing.T) {
	rp := validProposal(t) // one AUTO_STAGE-reachable Activation change already
	rp.Spec.Changes = append(rp.Spec.Changes, domain.RepairChange{
		TargetKind: "Activation",
		TargetID:   "worktree:sha256:fixture",
		Patch:      mustActivationPatch(t, "codex", nil, []string{"some-mcp-server"}),
	})
	result, err := ComputePropose(defaultProposeContext(), rp)
	if err != nil {
		t.Fatalf("ComputePropose: %v", err)
	}
	if result.Confirmation != domain.RepairConfirmRequired {
		t.Errorf("Confirmation = %q, want %q (the riskier of the two changes wins)", result.Confirmation, domain.RepairConfirmRequired)
	}
}

// --- tool wiring -------------------------------------------------------

func TestProposeToolEntry_RegisteredAndDispatches(t *testing.T) {
	entry := ProposeToolEntry(
		func() (report.Artifact, error) { return proposeFixtureArtifact(), nil },
		staticCapability(true),
	)
	if entry.definition.Name != toolNamePropose {
		t.Fatalf("Name = %q, want %q", entry.definition.Name, toolNamePropose)
	}
	if entry.definition.Description == "" {
		t.Error("Description is empty")
	}

	rp := validProposal(t)
	argsJSON, err := json.Marshal(ProposeArguments{Proposal: rp})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	resultAny, err := entry.handler(argsJSON)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	result, ok := resultAny.(ProposeResult)
	if !ok {
		t.Fatalf("handler returned %T, want ProposeResult", resultAny)
	}
	if !result.Accepted || result.Confirmation != domain.RepairAutoStage {
		t.Errorf("result = %+v, want Accepted:true Confirmation:AUTO_STAGE", result)
	}
}

func TestProposeToolEntry_RejectedProposal_IsToolLevelError(t *testing.T) {
	entry := ProposeToolEntry(
		func() (report.Artifact, error) { return proposeFixtureArtifact(), nil },
		staticCapability(true),
	)
	rp := validProposal(t)
	rp.Spec.Ownership = domain.OwnershipObserved
	argsJSON, _ := json.Marshal(ProposeArguments{Proposal: rp})
	_, err := entry.handler(argsJSON)
	if err == nil {
		t.Fatal("handler: want an error for a rejected proposal, got nil")
	}
	if !strings.Contains(err.Error(), "ownership") {
		t.Errorf("error = %q, want it to mention the failing gate", err.Error())
	}
}

// TestToolsList_FullFourToolRegistry_StaysWithinSizeBudget extends
// query_test.go's TestToolsList_SchemasStayWithinSizeBudget (a two-tool
// registry) to the full four-tool M4 surface: docs/architecture/runtime.md
// §6's M4 exit-gate design goal ("tool schemas and default responses
// remain deliberately small") applies to the WHOLE registry a real
// `omca mcp serve` process exposes, not just any one tool considered alone.
func TestToolsList_FullFourToolRegistry_StaysWithinSizeBudget(t *testing.T) {
	registry := NewRegistry(
		StatusToolEntry(staticStatus(StatusResult{}, nil)),
		QueryToolEntry(staticQuery(report.Artifact{}, nil)),
		ProposeToolEntry(staticQuery(report.Artifact{}, nil), staticCapability(true)),
		StageToolEntry(staticQuery(report.Artifact{}, nil), staticCapability(true), nil),
	)
	data, err := json.Marshal(toolsListResult(registry))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	const maxToolsListBytes = 10 * 1024
	if len(data) > maxToolsListBytes {
		t.Errorf("tools/list result is %d bytes, want <= %d (tool schemas must stay deliberately small)", len(data), maxToolsListBytes)
	}
}

func TestServe_ToolsList_IncludesProposeAndStage(t *testing.T) {
	registry := NewRegistry(
		StatusToolEntry(staticStatus(StatusResult{}, nil)),
		QueryToolEntry(staticQuery(report.Artifact{}, nil)),
		ProposeToolEntry(staticQuery(report.Artifact{}, nil), staticCapability(true)),
		StageToolEntry(staticQuery(report.Artifact{}, nil), staticCapability(true), nil),
	)
	names := map[string]bool{}
	for _, e := range registry.entries {
		names[e.definition.Name] = true
	}
	for _, want := range []string{toolNameStatus, toolNameQuery, toolNamePropose, toolNameStage} {
		if !names[want] {
			t.Errorf("registry has no %q entry: %v", want, names)
		}
	}
}
