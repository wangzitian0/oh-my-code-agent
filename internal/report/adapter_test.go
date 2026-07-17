package report

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
)

func TestBuildDriftSignals_Conflict_SourceDrift(t *testing.T) {
	g := effective.Graphs{
		Host:        "codex",
		HostVersion: "0.144.5",
		Effective: effective.EffectiveGraph{
			Conflicts: []effective.Conflict{
				{
					Concept:   "mcp_server",
					LogicalID: "stdio|shared-tools",
					Candidates: []effective.Candidate{
						{Ref: "project/.codex/config.toml#mcp_servers.shared-tools"},
						{Ref: "home/.codex/config.toml#mcp_servers.shared-tools"},
					},
					EvidenceLevel: domain.EvidenceLevelDiscovered,
					Reason:        "no qualified precedence program",
				},
			},
		},
	}

	signals := BuildDriftSignals("infra2", g)
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1", len(signals))
	}
	sig := signals[0]
	if sig.Category != domain.DriftSourceDrift {
		t.Errorf("Category = %q, want SOURCE_DRIFT", sig.Category)
	}
	if sig.EntityID != "mcp_server/stdio|shared-tools" {
		t.Errorf("EntityID = %q, want mcp_server/stdio|shared-tools", sig.EntityID)
	}
	if sig.Project != "infra2" || sig.Host != "codex" || sig.HostVersion != "0.144.5" {
		t.Errorf("Project/Host/HostVersion = %q/%q/%q", sig.Project, sig.Host, sig.HostVersion)
	}
	if sig.RootCause != "no qualified precedence program" {
		t.Errorf("RootCause = %q", sig.RootCause)
	}
	if sig.EvidenceLevel != domain.EvidenceLevelDiscovered {
		t.Errorf("EvidenceLevel = %q", sig.EvidenceLevel)
	}
	observed, ok := sig.Observed.([]string)
	if !ok || len(observed) != 2 {
		t.Fatalf("Observed = %#v, want a 2-element []string", sig.Observed)
	}
}

func TestBuildDriftSignals_Conflict_DefaultRootCauseWhenReasonEmpty(t *testing.T) {
	g := effective.Graphs{
		Host: "codex",
		Effective: effective.EffectiveGraph{
			Conflicts: []effective.Conflict{
				{Concept: "skill", LogicalID: "review|project", Candidates: []effective.Candidate{{Ref: "a"}, {Ref: "b"}}},
			},
		},
	}
	signals := BuildDriftSignals("infra2", g)
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1", len(signals))
	}
	if signals[0].RootCause == "" {
		t.Error("RootCause is empty even though Conflict.Reason was empty -- adapter must fall back to a synthesized reason")
	}
}

func TestBuildDriftSignals_AmbiguousIdentity_Unknown(t *testing.T) {
	g := effective.Graphs{
		Host: "claude-code",
		Effective: effective.EffectiveGraph{
			AmbiguousIdentities: []effective.AmbiguousIdentity{
				{
					Concept: "skill",
					A:       effective.Candidate{LogicalID: "review|project", Ref: "project/.claude/skills/review/SKILL.md"},
					B:       effective.Candidate{LogicalID: "review-v2|project", Ref: "project/.claude/skills/review-v2/SKILL.md"},
					Reason:  "same content digest, different directory names",
				},
			},
		},
	}

	signals := BuildDriftSignals("infra2", g)
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1", len(signals))
	}
	sig := signals[0]
	if sig.Category != domain.DriftUnknown {
		t.Errorf("Category = %q, want UNKNOWN", sig.Category)
	}
	if sig.RootCause != "same content digest, different directory names" {
		t.Errorf("RootCause = %q", sig.RootCause)
	}
}

// TestBuildDriftSignals_NoDesiredCorrelation documents this adapter's own
// named scope gap (see adapter.go's "Known follow-up" doc comment): a
// resolved (non-conflicting) EffectiveEntry never produces a signal on its
// own, even when the Desired Graph disagrees, because ID correlation
// between the two graphs is not solved yet.
func TestBuildDriftSignals_NoDesiredCorrelation(t *testing.T) {
	g := effective.Graphs{
		Host: "codex",
		Effective: effective.EffectiveGraph{
			Entries: []effective.EffectiveEntry{
				{Concept: "skill", LogicalID: "review|project", Provenance: effective.Provenance{SelectedSource: "project/.codex/skills/review/SKILL.md"}},
			},
		},
	}
	signals := BuildDriftSignals("infra2", g)
	if len(signals) != 0 {
		t.Fatalf("len(signals) = %d, want 0 (a clean resolved entry produces no signal)", len(signals))
	}
}

func TestBuildDriftSignals_Empty(t *testing.T) {
	signals := BuildDriftSignals("infra2", effective.Graphs{Host: "codex"})
	if len(signals) != 0 {
		t.Fatalf("len(signals) = %d, want 0", len(signals))
	}
}
