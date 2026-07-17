package effective

import (
	"fmt"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/ontology"
)

// concatOrder returns candidates' Refs, ordered by scopeRank when scopeRank
// covers every candidate's scope (confirmed=true), or merely sorted by Ref
// for determinism otherwise (confirmed=false: an arbitrary-but-stable order,
// not a claim about which source actually renders first).
func concatOrder(candidates []Candidate, scopeRank map[string]int) (ordered []string, confirmed bool) {
	refs := candidateRefs(candidates)
	confirmed = len(scopeRank) > 0
	if confirmed {
		for _, c := range candidates {
			if _, ok := scopeRank[c.Scope.Kind]; !ok {
				confirmed = false
				break
			}
		}
	}
	if !confirmed {
		return refs, false
	}
	sorted := append([]Candidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, rj := scopeRank[sorted[i].Scope.Kind], scopeRank[sorted[j].Scope.Kind]
		if ri != rj {
			return ri < rj
		}
		return sorted[i].Ref < sorted[j].Ref
	})
	out := make([]string, 0, len(sorted))
	for _, c := range sorted {
		out = append(out, c.Ref)
	}
	return out, true
}

// ComposeConcept implements CONCAT_ORDERED at the concept level: unlike
// every other operator in merge.go, which resolves candidates that already
// share one LogicalGroup's identity, CONCAT_ORDERED composes across every
// distinct logical entity of the concept (docs/ontology/README.md §3.2:
// "Concatenation order across distinct instructions is composition
// (CONCAT_ORDERED), never identity" — ontology/concepts/instruction.json's
// own x-logicalIdentity comment). It never excludes a candidate (appending
// never shadows), so there is no Conflict return: the only open question is
// whether the composition ORDER is confirmed, which this package reports
// through EffectiveEntry.Confirmed/Reason rather than refusing to produce an
// entry at all — matching fixtures/*/*/instructions-collision/
// expected-effective.json, which still names both sources ACTIVE while
// recording selectedSource: UNKNOWN for the unconfirmed order.
func ComposeConcept(concept string, groups []LogicalGroup, program domain.PrecedenceProgram, capOps domain.CapabilityOps, scopeRank map[string]int) EffectiveEntry {
	var all []Candidate
	for _, g := range groups {
		all = append(all, g.Candidates...)
	}
	refs := candidateRefs(all)
	ordered, orderConfirmed := concatOrder(all, scopeRank)
	qualified := capabilityQualified(capOps)
	confirmed := orderConfirmed && qualified

	var selected string
	reason := fmt.Sprintf("CONCAT_ORDERED: %d logical %s entities composed; all sources remain simultaneously active (concatenation never shadows)", len(groups), concept)
	switch {
	case confirmed:
		selected = fmt.Sprintf("%v", ordered)
		reason += fmt.Sprintf("; confirmed order %v", ordered)
	case !qualified:
		reason += fmt.Sprintf("; exact composition order is unconfirmed (resolve capability is %q, not EXACT/COMPATIBLE)", capOps.Resolve)
	default:
		reason += "; exact composition order is unconfirmed (no scope precedence order supplied)"
	}

	return EffectiveEntry{
		Concept:   concept,
		LogicalID: concept + ".composition",
		Composed:  true,
		Provenance: Provenance{
			Program:        program.ID,
			Operator:       ontology.OpConcatOrdered,
			SelectedSource: selected,
			ActiveSources:  refs,
		},
		EvidenceLevel: highestEvidence(all),
		Guarantee:     domain.GuaranteeAdvisory,
		Confirmed:     confirmed && highestEvidence(all).Rank() >= domain.EvidenceLevelHostReported.Rank(),
		Reason:        reason,
	}
}
