package effective

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/ontology"
)

func instructionCandidate(ref, scopeKind string) Candidate {
	return Candidate{
		Concept:       "instruction",
		LogicalID:     scopeKind + "|" + ref,
		Ref:           ref,
		Scope:         domain.ObservationScope{Kind: scopeKind, Root: ref},
		Source:        domain.ObservationSource{Kind: "file", Path: ref},
		EvidenceLevel: domain.EvidenceLevelParsed,
		ContentDigest: "digest-" + ref,
	}
}

func TestComposeConcept_AllSourcesActive_NeverShadowed(t *testing.T) {
	groups := []LogicalGroup{
		{Concept: "instruction", LogicalID: "user|home/CLAUDE.md", Candidates: []Candidate{instructionCandidate("home/CLAUDE.md", "user")}},
		{Concept: "instruction", LogicalID: "workspace|project/CLAUDE.md", Candidates: []Candidate{instructionCandidate("project/CLAUDE.md", "workspace")}},
	}
	program := domain.PrecedenceProgram{ID: "instruction.concat-ordered", Operator: "CONCAT_ORDERED"}
	capOps := domain.CapabilityOps{Resolve: domain.CapabilityUnknown}

	entry := ComposeConcept("instruction", groups, program, capOps, nil)
	if !entry.Composed {
		t.Error("Composed = false, want true")
	}
	if entry.Provenance.Operator != ontology.OpConcatOrdered {
		t.Errorf("Operator = %q", entry.Provenance.Operator)
	}
	if len(entry.Provenance.ActiveSources) != 2 {
		t.Errorf("ActiveSources = %v, want both sources active (concatenation never shadows)", entry.Provenance.ActiveSources)
	}
	if entry.Confirmed {
		t.Error("Confirmed = true, want false: resolve capability is UNKNOWN and no scope order was supplied")
	}
	if entry.Provenance.SelectedSource != "" {
		t.Errorf("SelectedSource = %q, want empty (order unconfirmed)", entry.Provenance.SelectedSource)
	}
}

func TestComposeConcept_ConfirmedOrder_WithScopeRankAndQualifiedCapability(t *testing.T) {
	groups := []LogicalGroup{
		{Concept: "instruction", LogicalID: "user|home/CLAUDE.md", Candidates: []Candidate{instructionCandidate("home/CLAUDE.md", "user")}},
		{Concept: "instruction", LogicalID: "workspace|project/CLAUDE.md", Candidates: []Candidate{instructionCandidate("project/CLAUDE.md", "workspace")}},
	}
	program := domain.PrecedenceProgram{ID: "instruction.concat-ordered", Operator: "CONCAT_ORDERED"}
	capOps := domain.CapabilityOps{Resolve: domain.CapabilityExact}

	entry := ComposeConcept("instruction", groups, program, capOps, map[string]int{"user": 1, "workspace": 2})
	if entry.Provenance.SelectedSource == "" {
		t.Error("SelectedSource should be populated once the order is confirmed")
	}
}
