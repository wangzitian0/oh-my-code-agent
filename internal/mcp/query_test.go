package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/drift"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// --- fixture builders ---------------------------------------------------

// smallFixtureArtifact builds a small, hand-constructed report.Artifact
// covering every kind ComputeQuery answers: one resolved entity, one
// Conflict, two drift cards (one with a multi-row Matrix), two Knowledge
// evidence citations, a current AND a pending generation's sources, and one
// duplicate capability -- everything query_test.go's happy-path tests
// exercise. Built directly as struct literals (never via report.Build/a
// real host binary), matching internal/mcp/status_test.go's existing
// "fixture-shaped internal/report.Artifact values" testing style for this
// package.
func smallFixtureArtifact() report.Artifact {
	codexDebug := report.HostDebug{
		Graph: effective.EffectiveGraph{
			Host: "codex",
			Entries: []effective.EffectiveEntry{
				{
					Concept:       "instruction",
					LogicalID:     "company-baseline",
					EvidenceLevel: domain.EvidenceLevelResolved,
					Guarantee:     domain.GuaranteeReconciled,
					Confirmed:     true,
					Reason:        "resolved from company Profile",
					Provenance:    effective.Provenance{ActiveSources: []string{"ref:company-baseline"}},
				},
			},
			Conflicts: []effective.Conflict{
				{Concept: "mcp_server", LogicalID: "shared-tools", Reason: "two sources tie", EvidenceLevel: domain.EvidenceLevelParsed},
			},
		},
		KnowledgeEvidence: []domain.KnowledgeEvidenceRef{
			{ID: "ev1", Kind: "official-doc", URL: "https://example.test/docs/codex"},
			{ID: "ev2", Kind: "changelog", Path: "CHANGELOG.md"},
		},
		CurrentSources: []domain.GenerationSourceEntry{
			{Concept: "mcp_server", Source: "/native/config.toml", Scope: "user", Included: false, Reason: "excluded: native user-global source"},
		},
		PendingSources: []domain.GenerationSourceEntry{
			{Concept: "instruction", Source: "/workspace/AGENTS.md", Scope: "workspace", Included: true, Reason: "included: repository-scope Instructions"},
		},
		CurrentGenerationID: "generation:sha256:current-aaa",
		PendingGenerationID: "generation:sha256:pending-bbb",
	}

	cardSmall := report.DriftCard{
		ID: "DR-00000001",
		ActionCard: drift.ActionCard{
			RootCause:   "company baseline missing",
			Remediation: "rebuild artifacts",
			Category:    domain.DriftConfigDrift,
			Impact:      drift.Impact{Projects: 1, Hosts: 1, Artifacts: 1},
			Guarantee:   domain.GuaranteeReconciled,
			EvidenceCounts: map[domain.EvidenceLevel]int{
				domain.EvidenceLevelHostReported: 1,
			},
			Samples: []drift.Assertion{
				{DriftAssertion: domain.DriftAssertion{EntityID: "instruction:company-baseline", Field: "content", Category: domain.DriftConfigDrift}, Host: "codex"},
			},
			Matrix: []drift.Assertion{
				{DriftAssertion: domain.DriftAssertion{EntityID: "instruction:company-baseline", Field: "content", Category: domain.DriftConfigDrift}, Host: "codex", Project: "infra2"},
			},
		},
	}
	cardMulti := report.DriftCard{
		ID: "DR-00000002",
		ActionCard: drift.ActionCard{
			RootCause:   "shared-tools duplicated across transports",
			Remediation: "dedupe mcp_server registration",
			Category:    domain.DriftSourceDrift,
			Impact:      drift.Impact{Projects: 2, Hosts: 2, Artifacts: 3},
			Matrix: []drift.Assertion{
				{DriftAssertion: domain.DriftAssertion{EntityID: "mcp_server:shared-tools", Field: "source", Category: domain.DriftSourceDrift}, Host: "codex", Project: "infra2"},
				{DriftAssertion: domain.DriftAssertion{EntityID: "mcp_server:shared-tools", Field: "source", Category: domain.DriftSourceDrift}, Host: "claude-code", Project: "infra2"},
				{DriftAssertion: domain.DriftAssertion{EntityID: "mcp_server:shared-tools", Field: "source", Category: domain.DriftSourceDrift}, Host: "codex", Project: "finance"},
			},
		},
	}

	return report.Artifact{
		Report: domain.Report{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       "Report",
			Metadata:   domain.ReportMetadata{ID: "report:worktree:sha256:fixture:2026-07-18T00:00:00Z", Worktree: "worktree:sha256:fixture", GeneratedAt: "2026-07-18T00:00:00Z"},
			Spec: domain.ReportSpec{
				Fingerprint: "sha256:fixture-fingerprint",
				Planes:      domain.ReportPlanes{Native: 3, Observed: 3, HostEffective: 1, Current: 1, Pending: 1},
			},
		},
		ActionCards: []report.DriftCard{cardSmall, cardMulti},
		Hosts: []report.HostSummary{
			{Host: "codex", Knowledge: report.HostKnowledge{Qualified: true, PackID: "codex-pack", Status: domain.KnowledgeFresh}, Planes: report.HostPlaneCounts{Observed: 3, Effective: 1, Current: 1, Pending: 1}},
		},
		DuplicateCapabilities: []report.DuplicateCapabilityEntry{
			{Fingerprint: "stdio|shared-tools", ContextCost: report.ContextCostAttribution{RedundantSources: 1, EstimatedTokens: 120}},
		},
		Debug: map[string]report.HostDebug{"codex": codexDebug},
	}
}

// --- kind == "entity" -----------------------------------------------------

func TestComputeQuery_Entity_Found(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindEntity, Host: "codex", Concept: "instruction", LogicalID: "company-baseline"})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if result.Kind != QueryKindEntity {
		t.Errorf("Kind = %q, want %q", result.Kind, QueryKindEntity)
	}
	if result.Entity == nil || !result.Entity.Found {
		t.Fatalf("Entity = %+v, want Found:true", result.Entity)
	}
	if result.Entity.EvidenceLevel != domain.EvidenceLevelResolved {
		t.Errorf("Entity.EvidenceLevel = %q, want %q", result.Entity.EvidenceLevel, domain.EvidenceLevelResolved)
	}
	if result.Entity.Trace != nil {
		t.Error("Entity.Trace is populated without Trace:true in the request")
	}
}

func TestComputeQuery_Entity_Conflict_WithTrace(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindEntity, Host: "codex", Concept: "mcp_server", LogicalID: "shared-tools", Trace: true})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if !result.Entity.Found || !result.Entity.Conflict {
		t.Fatalf("Entity = %+v, want Found:true Conflict:true", result.Entity)
	}
	if result.Entity.Trace == nil {
		t.Error("Entity.Trace is nil despite Trace:true in the request")
	}
}

func TestComputeQuery_Entity_DefaultsToFirstHost(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindEntity, Concept: "instruction", LogicalID: "company-baseline"})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if result.Entity.Host != "codex" {
		t.Errorf("Entity.Host = %q, want %q (the only/first built host)", result.Entity.Host, "codex")
	}
}

func TestComputeQuery_Entity_RequiresConceptAndLogicalID(t *testing.T) {
	a := smallFixtureArtifact()
	if _, err := ComputeQuery(a, QueryArguments{Kind: QueryKindEntity, Host: "codex"}); err == nil {
		t.Fatal("ComputeQuery with kind=entity and no concept/logicalId: want error, got nil")
	}
}

func TestComputeQuery_Entity_UnknownHost(t *testing.T) {
	a := smallFixtureArtifact()
	if _, err := ComputeQuery(a, QueryArguments{Kind: QueryKindEntity, Host: "nope", Concept: "instruction", LogicalID: "company-baseline"}); err == nil {
		t.Fatal("ComputeQuery with an unbuilt host: want error, got nil")
	}
}

// --- kind == "drift" --------------------------------------------------

func TestComputeQuery_Drift_ListsSummariesInArtifactOrder(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindDrift})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if len(result.DriftCards) != 2 {
		t.Fatalf("DriftCards has %d entries, want 2", len(result.DriftCards))
	}
	if result.DriftCards[0].ID != "DR-00000001" || result.DriftCards[1].ID != "DR-00000002" {
		t.Errorf("DriftCards IDs = [%q, %q], want [DR-00000001, DR-00000002]", result.DriftCards[0].ID, result.DriftCards[1].ID)
	}
	// A card-list row (DriftSummary) has no Matrix field at all -- a
	// compile-time guarantee, not something a runtime assertion could add
	// to. That is exactly the size-bounding cut DriftSummary's own doc
	// comment documents: a card-list response is always small regardless of
	// how large any one card's Matrix is.
	if result.Page.Total != 2 || result.Page.Returned != 2 || result.Page.HasMore {
		t.Errorf("Page = %+v, want Total:2 Returned:2 HasMore:false", result.Page)
	}
}

func TestComputeQuery_Drift_ListPaging(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindDrift, Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if len(result.DriftCards) != 1 || result.DriftCards[0].ID != "DR-00000002" {
		t.Fatalf("DriftCards = %+v, want exactly [DR-00000002]", result.DriftCards)
	}
	if result.Page != (PageInfo{Offset: 1, Limit: 1, Total: 2, Returned: 1, HasMore: false}) {
		t.Errorf("Page = %+v, want {Offset:1 Limit:1 Total:2 Returned:1 HasMore:false}", result.Page)
	}
}

func TestComputeQuery_Drift_ByID_ReturnsDetailWithFullSummaryAndMatrix(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindDrift, DriftID: "DR-00000002"})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if result.Drift == nil {
		t.Fatal("Drift is nil")
	}
	if result.Drift.ID != "DR-00000002" || result.Drift.RootCause != "shared-tools duplicated across transports" {
		t.Errorf("Drift summary = %+v, unexpected", result.Drift.DriftSummary)
	}
	if len(result.Drift.Matrix) != 3 {
		t.Fatalf("Drift.Matrix has %d entries, want all 3 (well under the default page limit)", len(result.Drift.Matrix))
	}
	if result.DriftCards != nil {
		t.Error("DriftCards is populated alongside a DriftID-scoped Drift result, want nil")
	}
}

func TestComputeQuery_Drift_ByID_MatrixIsPaged(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindDrift, DriftID: "DR-00000002", Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if len(result.Drift.Matrix) != 1 || result.Drift.Matrix[0].Host != "claude-code" {
		t.Fatalf("Drift.Matrix = %+v, want exactly the second row (host claude-code)", result.Drift.Matrix)
	}
	if result.Page.Total != 3 || !result.Page.HasMore {
		t.Errorf("Page = %+v, want Total:3 HasMore:true", result.Page)
	}
}

func TestComputeQuery_Drift_UnknownID(t *testing.T) {
	a := smallFixtureArtifact()
	if _, err := ComputeQuery(a, QueryArguments{Kind: QueryKindDrift, DriftID: "DR-nonexistent"}); err == nil {
		t.Fatal("ComputeQuery with an unknown driftId: want error, got nil")
	}
}

// --- kind == "evidence" -------------------------------------------------

func TestComputeQuery_Evidence_ListsPaged(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindEvidence, Host: "codex"})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if len(result.Evidence) != 2 {
		t.Fatalf("Evidence has %d entries, want 2", len(result.Evidence))
	}
	if result.Evidence[0].ID != "ev1" || result.Evidence[1].ID != "ev2" {
		t.Errorf("Evidence IDs = [%q, %q], want [ev1, ev2]", result.Evidence[0].ID, result.Evidence[1].ID)
	}
}

func TestComputeQuery_Evidence_UnknownHost(t *testing.T) {
	a := smallFixtureArtifact()
	if _, err := ComputeQuery(a, QueryArguments{Kind: QueryKindEvidence, Host: "nope"}); err == nil {
		t.Fatal("ComputeQuery with an unbuilt host: want error, got nil")
	}
}

// --- kind == "generation" ------------------------------------------------

func TestComputeQuery_Generation_DefaultsToCurrent(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindGeneration, Host: "codex"})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if result.Generation == nil {
		t.Fatal("Generation is nil")
	}
	if result.Generation.Plane != "current" {
		t.Errorf("Generation.Plane = %q, want %q", result.Generation.Plane, "current")
	}
	if result.Generation.GenerationID != "generation:sha256:current-aaa" {
		t.Errorf("Generation.GenerationID = %q, want the current generation's ID", result.Generation.GenerationID)
	}
	if len(result.Generation.Sources) != 1 || result.Generation.Sources[0].Source != "/native/config.toml" {
		t.Errorf("Generation.Sources = %+v, unexpected", result.Generation.Sources)
	}
}

func TestComputeQuery_Generation_Pending(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindGeneration, Host: "codex", Plane: "pending"})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if result.Generation.GenerationID != "generation:sha256:pending-bbb" {
		t.Errorf("Generation.GenerationID = %q, want the pending generation's ID", result.Generation.GenerationID)
	}
	if len(result.Generation.Sources) != 1 || result.Generation.Sources[0].Source != "/workspace/AGENTS.md" {
		t.Errorf("Generation.Sources = %+v, unexpected", result.Generation.Sources)
	}
}

func TestComputeQuery_Generation_UnknownPlane(t *testing.T) {
	a := smallFixtureArtifact()
	if _, err := ComputeQuery(a, QueryArguments{Kind: QueryKindGeneration, Host: "codex", Plane: "future"}); err == nil {
		t.Fatal("ComputeQuery with an unknown plane: want error, got nil")
	}
}

// --- kind == "artifact" ---------------------------------------------------

func TestComputeQuery_Artifact_ReturnsBoundedSummary(t *testing.T) {
	a := smallFixtureArtifact()
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindArtifact})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if result.Artifact == nil {
		t.Fatal("Artifact is nil")
	}
	if result.Artifact.Worktree != "worktree:sha256:fixture" {
		t.Errorf("Artifact.Worktree = %q, want %q", result.Artifact.Worktree, "worktree:sha256:fixture")
	}
	if result.Artifact.DriftCardCount != 2 {
		t.Errorf("Artifact.DriftCardCount = %d, want 2", result.Artifact.DriftCardCount)
	}
	if result.Artifact.DuplicateCapabilityCount != 1 {
		t.Errorf("Artifact.DuplicateCapabilityCount = %d, want 1", result.Artifact.DuplicateCapabilityCount)
	}
	if len(result.Artifact.Hosts) != 1 || result.Artifact.Hosts[0].Host != "codex" {
		t.Errorf("Artifact.Hosts = %+v, want exactly one codex entry", result.Artifact.Hosts)
	}
}

// --- kind validation -------------------------------------------------

func TestComputeQuery_EmptyKind_Errors(t *testing.T) {
	if _, err := ComputeQuery(smallFixtureArtifact(), QueryArguments{}); err == nil {
		t.Fatal("ComputeQuery with no kind: want error, got nil")
	}
}

func TestComputeQuery_UnknownKind_Errors(t *testing.T) {
	if _, err := ComputeQuery(smallFixtureArtifact(), QueryArguments{Kind: "bogus"}); err == nil {
		t.Fatal("ComputeQuery with an unknown kind: want error, got nil")
	}
}

// --- paging primitives -------------------------------------------------

func TestNormalizePage(t *testing.T) {
	cases := []struct {
		name             string
		offset, limit    int
		wantOff, wantLim int
	}{
		{"defaults", 0, 0, 0, defaultQueryPageLimit},
		{"negative offset clamped to zero", -5, 10, 0, 10},
		{"negative limit uses default", 3, -1, 3, defaultQueryPageLimit},
		{"limit above max clamps down", 0, maxQueryPageLimit + 500, 0, maxQueryPageLimit},
		{"limit at max is unchanged", 0, maxQueryPageLimit, 0, maxQueryPageLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotOff, gotLim := normalizePage(c.offset, c.limit)
			if gotOff != c.wantOff || gotLim != c.wantLim {
				t.Errorf("normalizePage(%d, %d) = (%d, %d), want (%d, %d)", c.offset, c.limit, gotOff, gotLim, c.wantOff, c.wantLim)
			}
		})
	}
}

func TestPageSlice_OffsetPastEnd_ReturnsEmptyNotNilSlice(t *testing.T) {
	items := []int{1, 2, 3}
	out, page := pageSlice(items, 10, 5)
	if out == nil {
		t.Error("pageSlice past the end returned a nil slice, want non-nil empty")
	}
	if len(out) != 0 {
		t.Errorf("pageSlice past the end returned %d items, want 0", len(out))
	}
	if page.Total != 3 || page.Returned != 0 || page.HasMore {
		t.Errorf("page = %+v, want Total:3 Returned:0 HasMore:false", page)
	}
}

// --- issue #24 AC: worktree-retargeting security property ----------------

// TestQueryArguments_UnknownWorktreeFields_AreSilentlyIgnored proves the
// structural half of QueryArguments' own doc comment: encoding/json's
// default Unmarshal behavior drops any JSON property with no matching
// struct field, so a crafted "worktreeId"/"runId"/"generationId" argument
// in a tools/call payload decodes into nothing -- QueryArguments has no
// field it could possibly bind to. This is proven by demonstrating the
// decoded value is byte-identical whether or not those keys are present.
func TestQueryArguments_UnknownWorktreeFields_AreSilentlyIgnored(t *testing.T) {
	clean := `{"kind":"artifact"}`
	poisoned := `{"kind":"artifact","worktreeId":"attacker-worktree","runId":"attacker-run","generationId":"attacker-generation"}`

	var cleanArgs, poisonedArgs QueryArguments
	if err := json.Unmarshal([]byte(clean), &cleanArgs); err != nil {
		t.Fatalf("Unmarshal(clean): %v", err)
	}
	if err := json.Unmarshal([]byte(poisoned), &poisonedArgs); err != nil {
		t.Fatalf("Unmarshal(poisoned): %v", err)
	}
	if cleanArgs != poisonedArgs {
		t.Fatalf("decoded QueryArguments differ depending on unrecognized worktree/run/generation keys: clean=%+v poisoned=%+v", cleanArgs, poisonedArgs)
	}
}

// TestQueryToolHandler_IgnoresWorktreeRetargetingArguments is the
// end-to-end proof issue #24's round-4 audit demands: an ArtifactFunc bound
// to one worktree ("bound-worktree") is exercised through queryToolHandler
// with a tools/call arguments payload that names a COMPLETELY DIFFERENT
// worktree/run/generation ("attacker-worktree" etc.) alongside a legitimate
// kind=artifact query. The result must reflect only the bound artifact --
// there is no code path by which the attacker-named identifiers could have
// been honored, since queryToolHandler resolves the artifact via
// artifactFn() alone, entirely independently of the decoded arguments.
func TestQueryToolHandler_IgnoresWorktreeRetargetingArguments(t *testing.T) {
	bound := smallFixtureArtifact()
	bound.Report.Metadata.Worktree = "worktree:sha256:bound-worktree"

	calls := 0
	artifactFn := ArtifactFunc(func() (report.Artifact, error) {
		calls++
		return bound, nil
	})
	handler := queryToolHandler(artifactFn)

	poisoned := json.RawMessage(`{"kind":"artifact","worktreeId":"worktree:sha256:attacker-worktree","runId":"run:attacker","generationId":"generation:attacker"}`)
	resultAny, err := handler(poisoned)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	result, ok := resultAny.(QueryResult)
	if !ok {
		t.Fatalf("handler returned %T, want QueryResult", resultAny)
	}
	if result.Artifact == nil {
		t.Fatal("Artifact is nil")
	}
	if result.Artifact.Worktree != "worktree:sha256:bound-worktree" {
		t.Fatalf("Artifact.Worktree = %q, want the bound worktree (attacker-supplied arguments must never retarget it)", result.Artifact.Worktree)
	}
	if calls != 1 {
		t.Errorf("artifactFn was called %d times, want exactly 1", calls)
	}
}

// TestServe_ToolsCall_OmcaQuery_WorktreeRetargetingArgumentIsIgnored is the
// same property one layer up, exercised through the full stdio JSON-RPC
// Serve() loop rather than calling queryToolHandler directly -- proving the
// protocol plumbing (handleToolsCall's json.Unmarshal into toolsCallParams,
// then into QueryArguments) carries no back door either. This is the literal
// "(test)" issue #24's AC #1 parenthetical asks for: "a prompt cannot
// retarget another worktree."
func TestServe_ToolsCall_OmcaQuery_WorktreeRetargetingArgumentIsIgnored(t *testing.T) {
	bound := smallFixtureArtifact()
	bound.Report.Metadata.Worktree = "worktree:sha256:bound-worktree"

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"omca_query","arguments":{"kind":"artifact","worktreeId":"worktree:sha256:attacker-worktree","generationId":"generation:attacker","runId":"run:attacker"}}}` + "\n"

	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, testRegistry(staticStatus(StatusResult{}, nil), staticQuery(bound, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	result := msgs[0]["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatalf("isError = true: %v", result)
	}
	structured := result["structuredContent"].(map[string]any)
	artifact := structured["artifact"].(map[string]any)
	if artifact["worktree"] != "worktree:sha256:bound-worktree" {
		t.Fatalf("artifact.worktree = %v, want the bound worktree (attacker-supplied worktreeId/runId/generationId arguments must never retarget it)", artifact["worktree"])
	}
}

// --- issue #24 AC: size-budget properties ---------------------------------

// largeArtifactFixture builds a synthetic, deliberately oversized
// report.Artifact -- nDriftCards drift cards each with matrixPerCard Matrix
// rows, nEvidence Knowledge evidence citations, and nSources current
// generation Sources entries for one host -- standing in for a realistic
// "many drift cards / many entities" worst case (issue #24's own size-budget
// AC language). Every list-shaped ComputeQuery kind is expected to stay
// bounded regardless of how large these counts are, because paging (not the
// underlying data volume) determines response size.
func largeArtifactFixture(nDriftCards, matrixPerCard, nEvidence, nSources int) report.Artifact {
	cards := make([]report.DriftCard, 0, nDriftCards)
	for i := 0; i < nDriftCards; i++ {
		matrix := make([]drift.Assertion, 0, matrixPerCard)
		for j := 0; j < matrixPerCard; j++ {
			matrix = append(matrix, drift.Assertion{
				DriftAssertion: domain.DriftAssertion{
					EntityID:    fmt.Sprintf("mcp_server:server-%04d-%04d", i, j),
					Field:       "command",
					Category:    domain.DriftConfigDrift,
					RootCause:   fmt.Sprintf("root cause number %04d, a moderately long human-readable sentence explaining exactly why this card exists and what a human should do about it", i),
					Remediation: "rebuild the affected artifacts and restart the affected host sessions",
				},
				Host:    "codex",
				Project: fmt.Sprintf("project-%04d", j%25),
			})
		}
		cards = append(cards, report.DriftCard{
			ID: fmt.Sprintf("DR-%08d", i),
			ActionCard: drift.ActionCard{
				RootCause:   fmt.Sprintf("root cause number %04d, a moderately long human-readable sentence explaining exactly why this card exists and what a human should do about it", i),
				Remediation: "rebuild the affected artifacts and restart the affected host sessions",
				Category:    domain.DriftConfigDrift,
				Impact:      drift.Impact{Projects: 25, Hosts: 1, Artifacts: matrixPerCard},
				Matrix:      matrix,
			},
		})
	}

	evidence := make([]domain.KnowledgeEvidenceRef, 0, nEvidence)
	for i := 0; i < nEvidence; i++ {
		evidence = append(evidence, domain.KnowledgeEvidenceRef{
			ID:   fmt.Sprintf("ev-%04d", i),
			Kind: "official-doc",
			URL:  fmt.Sprintf("https://example.test/docs/evidence-%04d-a-fairly-long-url-path-segment-to-be-realistic", i),
		})
	}

	sources := make([]domain.GenerationSourceEntry, 0, nSources)
	for i := 0; i < nSources; i++ {
		sources = append(sources, domain.GenerationSourceEntry{
			Concept:  "mcp_server",
			Source:   fmt.Sprintf("/native/codex-home/config-%04d.toml", i),
			Scope:    "user",
			Included: false,
			Reason:   "excluded: native user-global source, not yet activated in this worktree's Desired Graph",
		})
	}

	return report.Artifact{
		Report: domain.Report{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       "Report",
			Metadata:   domain.ReportMetadata{ID: "report:worktree:sha256:large-fixture", Worktree: "worktree:sha256:large-fixture", GeneratedAt: "2026-07-18T00:00:00Z"},
			Spec:       domain.ReportSpec{Fingerprint: "sha256:large-fixture-fingerprint"},
		},
		ActionCards: cards,
		Hosts:       []report.HostSummary{{Host: "codex", Knowledge: report.HostKnowledge{Qualified: true, Status: domain.KnowledgeFresh}}},
		Debug: map[string]report.HostDebug{
			"codex": {
				KnowledgeEvidence:   evidence,
				CurrentSources:      sources,
				CurrentGenerationID: "generation:sha256:large-fixture",
			},
		},
	}
}

// maxDefaultQueryResponseBytes is the size-budget ceiling this test enforces
// for a DEFAULT (no explicit limit) omca_query response, regardless of how
// large the underlying fixture is -- docs/architecture/runtime.md §6's M4
// exit-gate design goal, "tool schemas and default responses remain
// deliberately small." defaultQueryPageLimit rows of even a verbose
// Assertion/evidence/source entry stay well under this ceiling; this number
// is generous headroom above that, not a tight measured bound, so it flags
// an actual regression (someone removing the paging cut) rather than a
// harmless per-field size change.
const maxDefaultQueryResponseBytes = 32 * 1024

func TestComputeQuery_DefaultResponsesStayWithinSizeBudget(t *testing.T) {
	// 500 drift cards x 500 Matrix rows each, 500 evidence citations, 500
	// generation sources -- an order of magnitude beyond anything this
	// project's own worked examples describe (reporting.md §7's "8 projects
	// x 5 hosts x 40 artifacts"), specifically to prove size-boundedness
	// does not depend on the fixture staying small.
	a := largeArtifactFixture(500, 500, 500, 500)

	cases := []struct {
		name string
		args QueryArguments
	}{
		{"drift list, no limit", QueryArguments{Kind: QueryKindDrift}},
		{"drift detail matrix, no limit", QueryArguments{Kind: QueryKindDrift, DriftID: "DR-00000000"}},
		{"evidence list, no limit", QueryArguments{Kind: QueryKindEvidence, Host: "codex"}},
		{"generation sources, no limit", QueryArguments{Kind: QueryKindGeneration, Host: "codex"}},
		{"artifact overview", QueryArguments{Kind: QueryKindArtifact}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := ComputeQuery(a, c.args)
			if err != nil {
				t.Fatalf("ComputeQuery: %v", err)
			}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if len(data) > maxDefaultQueryResponseBytes {
				t.Errorf("response is %d bytes, want <= %d (default paging must bound response size regardless of fixture size)", len(data), maxDefaultQueryResponseBytes)
			}
			if result.Page.Total > 0 && result.Page.Returned > defaultQueryPageLimit {
				t.Errorf("Page.Returned = %d, want <= defaultQueryPageLimit (%d)", result.Page.Returned, defaultQueryPageLimit)
			}
		})
	}
}

// TestComputeQuery_MaxLimitStillStaysWithinAGenerousBudget proves the OTHER
// half of the size-budget property: even a caller that explicitly asks for
// the maximum allowed page (maxQueryPageLimit) against the same oversized
// fixture gets a response that is large but still finite and well short of
// pathological (e.g. no accidental quadratic blowup) -- maxQueryPageLimit
// itself, not the underlying fixture size, is what bounds it.
func TestComputeQuery_MaxLimitStillStaysWithinAGenerousBudget(t *testing.T) {
	a := largeArtifactFixture(500, 500, 500, 500)
	result, err := ComputeQuery(a, QueryArguments{Kind: QueryKindDrift, DriftID: "DR-00000000", Limit: maxQueryPageLimit})
	if err != nil {
		t.Fatalf("ComputeQuery: %v", err)
	}
	if len(result.Drift.Matrix) != maxQueryPageLimit {
		t.Fatalf("Matrix has %d entries, want exactly maxQueryPageLimit (%d)", len(result.Drift.Matrix), maxQueryPageLimit)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// A generous multiple of the default budget: maxQueryPageLimit is 4x
	// defaultQueryPageLimit, so a proportional ceiling with headroom is
	// 4x maxDefaultQueryResponseBytes.
	if maxBytes := 4 * maxDefaultQueryResponseBytes; len(data) > maxBytes {
		t.Errorf("response is %d bytes, want <= %d even at Limit:maxQueryPageLimit", len(data), maxBytes)
	}
}

// TestToolsList_SchemasStayWithinSizeBudget proves the OTHER half of issue
// #24's size-budget AC -- "a size-budget test bounds tool schemas... " --
// against the actual wire bytes tools/list would emit for this server's
// whole registered tool set, not just omca_query's own inputSchema in
// isolation.
func TestToolsList_SchemasStayWithinSizeBudget(t *testing.T) {
	registry := testRegistry(staticStatus(StatusResult{}, nil), staticQuery(report.Artifact{}, nil))
	data, err := json.Marshal(toolsListResult(registry))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	const maxToolsListBytes = 6 * 1024
	if len(data) > maxToolsListBytes {
		t.Errorf("tools/list result is %d bytes, want <= %d (tool schemas must stay deliberately small)", len(data), maxToolsListBytes)
	}
}

// TestQueryInputSchema_HasNoWorktreeOrGenerationProperty is the schema-level
// half of the worktree-retargeting security property: a schema-validating
// client should see, structurally, that no worktree/run/generation
// identifier is an accepted argument at all -- on top of QueryArguments'
// own inability to bind one (proven above).
func TestQueryInputSchema_HasNoWorktreeOrGenerationProperty(t *testing.T) {
	schema := queryInputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("queryInputSchema()[\"properties\"] is not an object")
	}
	// Exact-name match against the known retargeting-shaped identifiers,
	// not a substring check: "generation" as a Contains match would also
	// flag a legitimately-named non-identifier field merely containing that
	// word (e.g. a hypothetical "generationKind" plane-style selector, the
	// same shape this schema's actual "plane" field already is for
	// kind=generation) -- an exact match stays precise without being
	// fragile to that. Case-insensitive so "WorktreeID"/"RunId"/etc. still
	// get caught regardless of casing convention.
	dangerousNames := map[string]bool{
		"worktree": true, "worktreeid": true,
		"run": true, "runid": true,
		"generation": true, "generationid": true,
	}
	for name := range props {
		if dangerousNames[strings.ToLower(name)] {
			t.Errorf("queryInputSchema() has a property named %q, which looks like a worktree/run/generation identifier -- omca_query must never accept one", name)
		}
	}
	if schema["additionalProperties"] != false {
		t.Errorf("queryInputSchema()[\"additionalProperties\"] = %v, want false", schema["additionalProperties"])
	}
}

// --- fresh-per-call discipline -------------------------------------------

// TestQueryToolHandler_CallsArtifactFuncFreshEveryTime proves omca_query
// never caches a report at startup (issue #24's round-4 audit): two
// successive tools/call invocations through queryToolHandler each trigger
// their own artifactFn() call, and a change ArtifactFunc makes between the
// two calls (a distinct DriftCardCount, standing in for "a new drift card
// appeared between calls, e.g. after a restart-activated generation") is
// visible in the second call's result.
func TestQueryToolHandler_CallsArtifactFuncFreshEveryTime(t *testing.T) {
	calls := 0
	artifactFn := ArtifactFunc(func() (report.Artifact, error) {
		calls++
		a := smallFixtureArtifact()
		if calls == 1 {
			a.ActionCards = nil // first call: no drift cards yet
		}
		return a, nil // second call: the fixture's normal two cards
	})
	handler := queryToolHandler(artifactFn)

	first, err := handler(json.RawMessage(`{"kind":"artifact"}`))
	if err != nil {
		t.Fatalf("handler (first call): %v", err)
	}
	second, err := handler(json.RawMessage(`{"kind":"artifact"}`))
	if err != nil {
		t.Fatalf("handler (second call): %v", err)
	}
	firstCount := first.(QueryResult).Artifact.DriftCardCount
	secondCount := second.(QueryResult).Artifact.DriftCardCount
	if firstCount != 0 {
		t.Errorf("first call DriftCardCount = %d, want 0", firstCount)
	}
	if secondCount != 2 {
		t.Errorf("second call DriftCardCount = %d, want 2 (a value computed fresh on THIS call, not reused from the first)", secondCount)
	}
	if calls != 2 {
		t.Errorf("artifactFn was called %d times across two tools/call requests, want exactly 2 (once per call, never cached)", calls)
	}
}

// TestQueryToolHandler_ArtifactFuncError_IsReportedAsToolLevelError proves
// an ArtifactFunc failure (e.g. a real detect/observe/Build error) is a
// tool-level error, matching StatusFunc's own failure-handling contract, so
// a client can recover from it without treating the whole JSON-RPC exchange
// as broken.
func TestQueryToolHandler_ArtifactFuncError_IsReportedAsToolLevelError(t *testing.T) {
	handler := queryToolHandler(ArtifactFunc(func() (report.Artifact, error) {
		return report.Artifact{}, errors.New("boom: observing codex failed")
	}))
	_, err := handler(json.RawMessage(`{"kind":"artifact"}`))
	if err == nil {
		t.Fatal("handler with a failing ArtifactFunc: want error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %q, want it to mention the underlying failure", err.Error())
	}
}
