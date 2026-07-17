package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/drift"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
)

func testRenderArtifact(t *testing.T) Artifact {
	t.Helper()
	card := drift.ActionCard{
		RootCause:   "company baseline not represented",
		Remediation: "rebuild pending runtimes",
		Category:    domain.DriftEffectiveDrift,
		Impact:      drift.Impact{Projects: 1, Hosts: 1, Artifacts: 1},
		Guarantee:   domain.GuaranteeReconciled,
		EvidenceCounts: map[domain.EvidenceLevel]int{
			domain.EvidenceLevelHostReported: 1,
		},
		Matrix: []drift.Assertion{
			{DriftAssertion: domain.DriftAssertion{
				EntityID: "mcp_server/solo", Field: "active", Category: domain.DriftEffectiveDrift,
				RootCause: "company baseline not represented", ContextCell: "infra2 / codex",
				Observed: false, Expected: true, EvidenceLevel: domain.EvidenceLevelHostReported,
			}},
		},
	}
	cards, err := buildDriftCards([]drift.ActionCard{card})
	if err != nil {
		t.Fatalf("buildDriftCards: %v", err)
	}

	return Artifact{
		Report: domain.Report{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       "Report",
			Metadata:   domain.ReportMetadata{ID: "report:test", Worktree: "worktree:test", GeneratedAt: "2026-01-01T00:00:00Z"},
			Spec: domain.ReportSpec{
				Fingerprint: "sha256:" + strings.Repeat("a", 64),
				Planes:      domain.ReportPlanes{Native: 3, Observed: 3, HostEffective: 2},
			},
		},
		ActionCards: cards,
		Hosts: []HostSummary{
			{
				Host: "codex", HostVersion: "0.144.5",
				Knowledge:   HostKnowledge{Qualified: true, PackID: "codex-cli-0.144", Status: domain.KnowledgeFresh},
				ContextCost: nil,
				Planes:      HostPlaneCounts{Observed: 3, Effective: 2, Conflicts: 1},
			},
		},
		DuplicateCapabilities: []DuplicateCapabilityEntry{
			{
				Fingerprint: "websearch",
				Sources: []effective.ToolSource{
					{Kind: effective.ToolSourceBuiltin, Owner: "codex", Tool: "web_search"},
					{Kind: effective.ToolSourceMCP, Owner: "shared-tools", Tool: "web-search"},
				},
				ContextCost: ContextCostAttribution{RedundantSources: 1, EstimatedTokens: 120, Method: "estimate", Confidence: "estimate, not measured"},
			},
		},
	}
}

func TestRenderReportHuman_ContainsKeySections(t *testing.T) {
	a := testRenderArtifact(t)
	var buf bytes.Buffer
	RenderReportHuman(&buf, a)
	out := buf.String()

	for _, want := range []string{"Overview", "Drift", "Duplicate capabilities", "codex", "FRESH", "DR-", "websearch"} {
		if !strings.Contains(out, want) {
			t.Errorf("human report output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderReportHuman_UnqualifiedKnowledgeAndUnknownContextCost(t *testing.T) {
	a := testRenderArtifact(t)
	a.Hosts[0].Knowledge = HostKnowledge{Qualified: false, Reason: "no qualified pack"}
	var buf bytes.Buffer
	RenderReportHuman(&buf, a)
	out := buf.String()
	if !strings.Contains(out, "unqualified") {
		t.Errorf("expected 'unqualified' in output:\n%s", out)
	}
	if !strings.Contains(out, "context-cost: unknown") {
		t.Errorf("expected honest 'unknown' context-cost line:\n%s", out)
	}
}

func TestRenderDriftListHuman_Empty(t *testing.T) {
	var buf bytes.Buffer
	RenderDriftListHuman(&buf, Artifact{})
	if !strings.Contains(buf.String(), "no drift") {
		t.Errorf("expected 'no drift' for an empty artifact, got:\n%s", buf.String())
	}
}

func TestRenderDriftShowHuman_ExpandsMatrix(t *testing.T) {
	a := testRenderArtifact(t)
	var buf bytes.Buffer
	RenderDriftShowHuman(&buf, a.ActionCards[0])
	out := buf.String()
	for _, want := range []string{"Root cause", "Remediation", "Impact", "Evidence", "Matrix", "mcp_server/solo"} {
		if !strings.Contains(out, want) {
			t.Errorf("drift show output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderMatrixHuman(t *testing.T) {
	a := testRenderArtifact(t)
	var buf bytes.Buffer
	RenderMatrixHuman(&buf, a.ActionCards[0])
	if !strings.Contains(buf.String(), "mcp_server/solo") {
		t.Errorf("matrix output missing entity ID:\n%s", buf.String())
	}
}

func TestRenderExplainHuman_NotFound(t *testing.T) {
	var buf bytes.Buffer
	RenderExplainHuman(&buf, ExplainResult{Host: "codex", Concept: "skill", LogicalID: "x", Found: false})
	if !strings.Contains(buf.String(), "not found") {
		t.Errorf("expected 'not found':\n%s", buf.String())
	}
}

func TestRenderExplainHuman_WithTrace(t *testing.T) {
	r := ExplainResult{
		Host: "codex", Concept: "mcp_server", LogicalID: "solo", Found: true,
		EvidenceLevel: domain.EvidenceLevelParsed, Guarantee: domain.GuaranteeObserved,
		Trace: &ExplainTrace{
			ResolverTrace:   effective.Provenance{Program: "p", Operator: "UNION_BY_ID", SelectedSource: "ref-1"},
			PhysicalSources: []PhysicalSource{{Ref: "ref-1", Kind: "config", Disposition: domain.DispositionActive, EvidenceLevel: domain.EvidenceLevelParsed}},
			KnowledgeEvidence: []domain.KnowledgeEvidenceRef{
				{ID: "ev-1", Kind: "doc", URL: "https://example.test"},
			},
		},
	}
	var buf bytes.Buffer
	RenderExplainHuman(&buf, r)
	out := buf.String()
	for _, want := range []string{"Resolver trace", "Physical sources", "Knowledge evidence", "ref-1", "ev-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("explain --trace output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderCompareHuman(t *testing.T) {
	active := PlaneRow{Concept: "mcp_server", ID: "solo", Present: true, Active: true}
	result := CompareResult{
		Host: "codex", PlaneA: PlaneNative, PlaneB: PlaneCurrent,
		Rows: []CompareRow{{Concept: "mcp_server", ID: "solo", A: &active, B: nil, Differs: true}},
	}
	var buf bytes.Buffer
	RenderCompareHuman(&buf, result)
	out := buf.String()
	for _, want := range []string{"NATIVE", "CURRENT", "solo", "!"} {
		if !strings.Contains(out, want) {
			t.Errorf("compare output missing %q:\n%s", want, out)
		}
	}
}
