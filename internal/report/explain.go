package report

import (
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
)

// Explain answers `omca explain <concept> <logical-id> [--trace]`: the
// resolved (or conflicted, or unknown) state of one logical entity on one
// host, projected from a's Debug data. trace controls whether the full
// expansion chain docs/architecture/reporting.md §10 names — "effective
// value -> resolver trace -> physical sources -> Knowledge evidence" — is
// built; a caller that only wants the summary line (no --trace flag) is
// never charged for assembling it.
func Explain(a Artifact, host, concept, logicalID string, trace bool) ExplainResult {
	result := ExplainResult{Host: host, Concept: concept, LogicalID: logicalID}

	hd, ok := a.Debug[host]
	if !ok {
		return result
	}

	if entry, found := hd.Graph.Find(concept, logicalID); found {
		result.Found = true
		result.EvidenceLevel = entry.EvidenceLevel
		result.Guarantee = entry.Guarantee
		result.Confirmed = entry.Confirmed
		result.Reason = entry.Reason
		if trace {
			result.Trace = buildExplainTrace(hd, entry.Provenance)
		}
		return result
	}

	for _, c := range hd.Graph.Conflicts {
		if c.Concept != concept || c.LogicalID != logicalID {
			continue
		}
		result.Found = true
		result.Conflict = true
		result.EvidenceLevel = c.EvidenceLevel
		result.Reason = c.Reason
		if trace {
			refs := candidateRefs(c.Candidates)
			result.Trace = buildExplainTrace(hd, effective.Provenance{
				Program:        c.Program,
				IgnoredSources: refs,
				Constraints:    []string{"UNRESOLVED: " + c.Reason},
			})
		}
		return result
	}

	return result
}

// buildExplainTrace expands prov's ActiveSources/IgnoredSources refs into
// full PhysicalSource records looked up from hd.Candidates, plus hd's
// Knowledge evidence — docs/architecture/reporting.md §14's "every resolver
// trace expands to physical sources and Knowledge evidence."
func buildExplainTrace(hd HostDebug, prov effective.Provenance) *ExplainTrace {
	seen := map[string]bool{}
	var sources []PhysicalSource
	addRef := func(ref string) {
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		if cand, ok := findCandidateByRef(hd.Candidates, ref); ok {
			sources = append(sources, PhysicalSource{
				Ref:           cand.Ref,
				Path:          cand.Source.Path,
				Kind:          cand.Source.Kind,
				Disposition:   cand.Disposition,
				EvidenceLevel: cand.EvidenceLevel,
				ContentDigest: cand.ContentDigest,
			})
			return
		}
		sources = append(sources, PhysicalSource{Ref: ref})
	}
	for _, ref := range prov.ActiveSources {
		addRef(ref)
	}
	for _, ref := range prov.IgnoredSources {
		addRef(ref)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Ref < sources[j].Ref })

	return &ExplainTrace{
		ResolverTrace:     prov,
		PhysicalSources:   sources,
		KnowledgeEvidence: hd.KnowledgeEvidence,
	}
}

// findCandidateByRef linear-searches candidates for ref — hd.Candidates is
// one host's worth of Candidates (a handful to a few hundred in practice),
// small enough that a map index would be premature optimization for a
// lookup that only ever runs inside `omca explain`, not a hot path.
func findCandidateByRef(candidates []effective.Candidate, ref string) (effective.Candidate, bool) {
	for _, c := range candidates {
		if c.Ref == ref {
			return c, true
		}
	}
	return effective.Candidate{}, false
}
