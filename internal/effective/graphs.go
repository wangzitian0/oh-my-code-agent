package effective

import (
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
)

// ObservedGraph is a thin, queryable wrapper over the Observed Graph
// (docs/architecture/README.md §5.1): physical reality exactly as
// internal/observe (or internal/qualify, for fixture cases) already
// produced it. This package does not recompute anything about it.
type ObservedGraph struct {
	Observations []domain.Observation
}

// ByConcept returns every Observation for concept.
func (g ObservedGraph) ByConcept(concept string) []domain.Observation {
	var out []domain.Observation
	for _, o := range g.Observations {
		if o.Spec.Concept == concept {
			out = append(out, o)
		}
	}
	return out
}

// Find returns the Observation whose source path matches path for concept,
// if any (Observed Graph identity is purely physical: one record per
// discovered source, docs/architecture/README.md §5.1 — no logical
// grouping, that is the Effective Graph's job).
func (g ObservedGraph) Find(concept, path string) (domain.Observation, bool) {
	for _, o := range g.Observations {
		if o.Spec.Concept == concept && o.Spec.Source.Path == path {
			return o, true
		}
	}
	return domain.Observation{}, false
}

// DesiredGraph is a thin wrapper over internal/resolve's ResolvedState
// (docs/architecture/README.md §5.3): the host-neutral result of Profiles,
// Bindings, Activation, policy, and exceptions, already computed by
// internal/resolve (PR-13). This package does not recompute it, only
// exposes it alongside Observed and Effective through one consistent
// [Graphs] type.
type DesiredGraph struct {
	resolve.ResolvedState
}

// Plane names one of the three core graphs for [Graphs.Query].
type Plane string

const (
	PlaneObserved  Plane = "OBSERVED"
	PlaneEffective Plane = "EFFECTIVE"
	PlaneDesired   Plane = "DESIRED"
)

// QueryResult is one plane's answer to a (concept, id) query, normalized
// across all three graphs so a caller (a future report/MCP projection) does
// not need three different lookup shapes. Provenance is the zero value for
// a plane that does not carry one (Observed is raw physical fact; Desired
// is host-neutral intent, not evidence about physical sources).
type QueryResult struct {
	Plane         Plane
	Concept       string
	ID            string
	Provenance    Provenance
	EvidenceLevel domain.EvidenceLevel
	Active        bool
	Reason        string
}

// Graphs bundles the Observed, Effective, and Desired graphs for one host
// invocation behind one consistent Query method (docs/architecture/
// README.md §5's three core graphs; issue #21 AC "All three graphs are
// queryable").
type Graphs struct {
	Host        string
	HostVersion string

	Observed  ObservedGraph
	Effective EffectiveGraph
	Desired   DesiredGraph
}

// conceptToAssetKind maps this package's ontology concept IDs (snake_case,
// e.g. "mcp_server") to internal/resolve's AssetKind vocabulary (camelCase,
// e.g. resolve.KindMCPServer) — the two packages independently named their
// own concept identifiers before this package existed to bridge them.
func conceptToAssetKind(concept string) resolve.AssetKind {
	switch concept {
	case "mcp_server":
		return resolve.KindMCPServer
	case "skill":
		return resolve.KindSkill
	case "instruction":
		return resolve.KindInstruction
	default:
		return resolve.AssetKind(concept)
	}
}

// Query answers one (plane, concept, id) lookup uniformly across all three
// graphs. id is a source path for PlaneObserved, a LogicalID for
// PlaneEffective, and an asset ID for PlaneDesired.
func (g Graphs) Query(plane Plane, concept, id string) (QueryResult, bool) {
	switch plane {
	case PlaneObserved:
		obs, ok := g.Observed.Find(concept, id)
		if !ok {
			return QueryResult{}, false
		}
		return QueryResult{
			Plane: plane, Concept: concept, ID: id,
			EvidenceLevel: obs.Spec.EvidenceLevel,
			Active:        obs.Spec.Disposition == domain.DispositionActive,
			Reason:        string(obs.Spec.Disposition),
		}, true

	case PlaneEffective:
		entry, ok := g.Effective.Find(concept, id)
		if !ok {
			return QueryResult{}, false
		}
		return QueryResult{
			Plane: plane, Concept: concept, ID: id,
			Provenance:    entry.Provenance,
			EvidenceLevel: entry.EvidenceLevel,
			Active:        entry.Provenance.SelectedSource != "" || len(entry.Provenance.ActiveSources) > 0,
			Reason:        entry.Reason,
		}, true

	case PlaneDesired:
		asset, ok := g.Desired.Find(conceptToAssetKind(concept), id)
		if !ok {
			return QueryResult{}, false
		}
		return QueryResult{
			Plane: plane, Concept: concept, ID: id,
			Active: asset.Active,
			Reason: asset.Reason,
		}, true

	default:
		return QueryResult{}, false
	}
}
