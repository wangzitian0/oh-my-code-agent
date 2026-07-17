package effective

import (
	"fmt"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/ontology"
)

// EffectiveGraph is the resolved Effective Graph for one host
// (docs/architecture/README.md §5.2): every logical entity this package
// reached a decision for (Entries), every one it deliberately did not
// (Conflicts), every identity-layer ambiguity the Identity Matcher
// preserved rather than guessed (AmbiguousIdentities), and every duplicate
// logical capability found across tool transports (DuplicateCapabilities).
type EffectiveGraph struct {
	Host        string
	HostVersion string

	Entries               []EffectiveEntry
	Conflicts             []Conflict
	AmbiguousIdentities   []AmbiguousIdentity
	DuplicateCapabilities []DuplicateCapability
}

// Find returns the resolved entry for (concept, logicalID), if this package
// reached a non-conflicting decision for it.
func (g EffectiveGraph) Find(concept, logicalID string) (EffectiveEntry, bool) {
	for _, e := range g.Entries {
		if e.Concept == concept && e.LogicalID == logicalID {
			return e, true
		}
	}
	return EffectiveEntry{}, false
}

// ByConcept returns every resolved entry for concept, in Entries' stable
// sorted order.
func (g EffectiveGraph) ByConcept(concept string) []EffectiveEntry {
	var out []EffectiveEntry
	for _, e := range g.Entries {
		if e.Concept == concept {
			out = append(out, e)
		}
	}
	return out
}

// HasConflict reports whether (concept, logicalID) is present in
// g.Conflicts.
func (g EffectiveGraph) HasConflict(concept, logicalID string) bool {
	for _, c := range g.Conflicts {
		if c.Concept == concept && c.LogicalID == logicalID {
			return true
		}
	}
	return false
}

// ComputeEffectiveGraph is this package's main entry point: it filters
// observations to host, extracts Candidates, runs the Identity Matcher, and
// resolves (or composes, or leaves as a Conflict) every resulting logical
// group per docs/ontology/README.md §3.2's Native resolution contract.
// extraToolSources lets a caller fold in built-in/plugin tool inventories
// this package has no observation source for yet (duplicate.go); pass nil
// when none are available.
func ComputeEffectiveGraph(host, hostVersion string, observations []domain.Observation, hk domain.HostKnowledge, opts Options, extraToolSources []ToolSource) (EffectiveGraph, error) {
	if err := domain.ValidateHostID(host); err != nil {
		return EffectiveGraph{}, fmt.Errorf("effective: ComputeEffectiveGraph: %w", err)
	}

	var relevant []domain.Observation
	for _, o := range observations {
		if o.Spec.Host.ID == host {
			relevant = append(relevant, o)
		}
	}

	candidates, err := ExtractCandidates(relevant)
	if err != nil {
		return EffectiveGraph{}, fmt.Errorf("effective: ComputeEffectiveGraph: %w", err)
	}

	groups, ambiguous := MatchIdentities(candidates)

	byConcept := map[string][]LogicalGroup{}
	var conceptOrder []string
	for _, g := range groups {
		if _, ok := byConcept[g.Concept]; !ok {
			conceptOrder = append(conceptOrder, g.Concept)
		}
		byConcept[g.Concept] = append(byConcept[g.Concept], g)
	}
	sort.Strings(conceptOrder)

	graph := EffectiveGraph{Host: host, HostVersion: hostVersion, AmbiguousIdentities: ambiguous}

	for _, concept := range conceptOrder {
		conceptGroups := byConcept[concept]
		capOps := hk.Capabilities[concept]

		program, ok := LookupProgram(hk, concept)
		operator := ontology.MergeOperator(program.Operator)

		// CONCAT_ORDERED is a composition operator spanning every logical
		// entity of the concept, not a per-group resolution (compose.go) —
		// dispatched once per concept rather than once per LogicalGroup.
		if ok && operator.Valid() && operator == ontology.OpConcatOrdered {
			graph.Entries = append(graph.Entries, ComposeConcept(concept, conceptGroups, program, capOps, opts.ScopeRank))
			continue
		}

		for _, g := range conceptGroups {
			entry, conflict := ResolveGroup(g, hk, capOps, opts)
			if conflict != nil {
				graph.Conflicts = append(graph.Conflicts, *conflict)
				continue
			}
			graph.Entries = append(graph.Entries, *entry)
		}
	}

	allTools := append(append([]ToolSource(nil), MCPToolSources(candidates)...), extraToolSources...)
	graph.DuplicateCapabilities = DetectDuplicateCapabilities(allTools)

	sort.Slice(graph.Entries, func(i, j int) bool {
		if graph.Entries[i].Concept != graph.Entries[j].Concept {
			return graph.Entries[i].Concept < graph.Entries[j].Concept
		}
		return graph.Entries[i].LogicalID < graph.Entries[j].LogicalID
	})
	sort.Slice(graph.Conflicts, func(i, j int) bool {
		if graph.Conflicts[i].Concept != graph.Conflicts[j].Concept {
			return graph.Conflicts[i].Concept < graph.Conflicts[j].Concept
		}
		return graph.Conflicts[i].LogicalID < graph.Conflicts[j].LogicalID
	})

	return graph, nil
}
