package report

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
)

func testArtifactWithDebug() Artifact {
	return Artifact{
		Debug: map[string]HostDebug{
			"codex": {
				Graph: effective.EffectiveGraph{
					Entries: []effective.EffectiveEntry{
						{
							Concept:   "mcp_server",
							LogicalID: "stdio|solo",
							Provenance: effective.Provenance{
								Program:        "codex.mcp_server.precedence",
								Operator:       "UNION_BY_ID",
								SelectedSource: "codex-home/config.toml#mcp_servers.solo",
								ActiveSources:  []string{"codex-home/config.toml#mcp_servers.solo"},
							},
							EvidenceLevel: domain.EvidenceLevelParsed,
							Guarantee:     domain.GuaranteeObserved,
							Reason:        "only one source defines this ID",
						},
					},
					Conflicts: []effective.Conflict{
						{
							Concept:   "mcp_server",
							LogicalID: "stdio|shared",
							Program:   "codex.mcp_server.precedence",
							Candidates: []effective.Candidate{
								{Ref: "codex-home/config.toml#mcp_servers.shared"},
								{Ref: "project/.codex/config.toml#mcp_servers.shared"},
							},
							EvidenceLevel: domain.EvidenceLevelParsed,
							Reason:        "collision, no qualified resolver",
						},
					},
				},
				Candidates: []effective.Candidate{
					{
						Ref:           "codex-home/config.toml#mcp_servers.solo",
						Concept:       "mcp_server",
						Source:        domain.ObservationSource{Kind: "config", Path: "codex-home/config.toml"},
						Disposition:   domain.DispositionActive,
						EvidenceLevel: domain.EvidenceLevelParsed,
						ContentDigest: "sha256:abc",
					},
					{
						Ref:           "codex-home/config.toml#mcp_servers.shared",
						Concept:       "mcp_server",
						Source:        domain.ObservationSource{Kind: "config", Path: "codex-home/config.toml"},
						Disposition:   domain.DispositionDiscovered,
						EvidenceLevel: domain.EvidenceLevelParsed,
						ContentDigest: "sha256:def",
					},
					{
						Ref:           "project/.codex/config.toml#mcp_servers.shared",
						Concept:       "mcp_server",
						Source:        domain.ObservationSource{Kind: "config", Path: "project/.codex/config.toml"},
						Disposition:   domain.DispositionDiscovered,
						EvidenceLevel: domain.EvidenceLevelParsed,
						ContentDigest: "sha256:ghi",
					},
				},
				KnowledgeEvidence: []domain.KnowledgeEvidenceRef{
					{ID: "ev-1", Kind: "doc", URL: "https://example.test/docs"},
				},
			},
		},
	}
}

func TestExplain_ResolvedEntry_NoTrace(t *testing.T) {
	a := testArtifactWithDebug()
	r := Explain(a, "codex", "mcp_server", "stdio|solo", false)
	if !r.Found || r.Conflict {
		t.Fatalf("Found=%v Conflict=%v, want Found=true Conflict=false", r.Found, r.Conflict)
	}
	if r.EvidenceLevel != domain.EvidenceLevelParsed {
		t.Errorf("EvidenceLevel = %q", r.EvidenceLevel)
	}
	if r.Trace != nil {
		t.Error("Trace is set even though trace=false was requested")
	}
}

func TestExplain_ResolvedEntry_WithTrace_ExpandsPhysicalSourcesAndEvidence(t *testing.T) {
	a := testArtifactWithDebug()
	r := Explain(a, "codex", "mcp_server", "stdio|solo", true)
	if r.Trace == nil {
		t.Fatal("Trace is nil even though trace=true was requested")
	}
	if r.Trace.ResolverTrace.SelectedSource != "codex-home/config.toml#mcp_servers.solo" {
		t.Errorf("ResolverTrace.SelectedSource = %q", r.Trace.ResolverTrace.SelectedSource)
	}
	if len(r.Trace.PhysicalSources) != 1 {
		t.Fatalf("len(PhysicalSources) = %d, want 1", len(r.Trace.PhysicalSources))
	}
	src := r.Trace.PhysicalSources[0]
	if src.ContentDigest != "sha256:abc" || src.Disposition != domain.DispositionActive {
		t.Errorf("PhysicalSources[0] = %+v", src)
	}
	if len(r.Trace.KnowledgeEvidence) != 1 || r.Trace.KnowledgeEvidence[0].ID != "ev-1" {
		t.Errorf("KnowledgeEvidence = %+v", r.Trace.KnowledgeEvidence)
	}
}

func TestExplain_Conflict_WithTrace_ListsBothCandidates(t *testing.T) {
	a := testArtifactWithDebug()
	r := Explain(a, "codex", "mcp_server", "stdio|shared", true)
	if !r.Found || !r.Conflict {
		t.Fatalf("Found=%v Conflict=%v, want both true", r.Found, r.Conflict)
	}
	if r.Reason != "collision, no qualified resolver" {
		t.Errorf("Reason = %q", r.Reason)
	}
	if r.Trace == nil {
		t.Fatal("Trace is nil")
	}
	if len(r.Trace.PhysicalSources) != 2 {
		t.Fatalf("len(PhysicalSources) = %d, want 2", len(r.Trace.PhysicalSources))
	}
}

func TestExplain_NotFound(t *testing.T) {
	a := testArtifactWithDebug()
	r := Explain(a, "codex", "mcp_server", "does-not-exist", true)
	if r.Found {
		t.Error("Found = true for a logical ID that does not exist")
	}
	if r.Trace != nil {
		t.Error("Trace is set for a not-found result")
	}
}

func TestExplain_UnknownHost(t *testing.T) {
	a := testArtifactWithDebug()
	r := Explain(a, "claude-code", "mcp_server", "stdio|solo", true)
	if r.Found {
		t.Error("Found = true for a host with no Debug data")
	}
}
