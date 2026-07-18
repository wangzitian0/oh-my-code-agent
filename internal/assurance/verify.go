package assurance

import (
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
)

// VerifyGraph re-derives every EffectiveEntry's and Conflict's honestly
// verified domain.EvidenceLevel for host, using the committed [Ceilings]
// table. It never touches Guarantee, Confirmed's own gate, Provenance,
// Reason, or any other field -- see [VerifyGraphWithCeilings]'s doc comment
// for the full contract this function is a convenience wrapper for.
func VerifyGraph(host string, graph effective.EffectiveGraph, hk domain.HostKnowledge) effective.EffectiveGraph {
	return VerifyGraphWithCeilings(host, graph, hk, Ceilings)
}

// VerifyGraphWithCeilings is [VerifyGraph] with an explicit ceilings table
// rather than the package-level [Ceilings] default, mirroring
// internal/effective.Options' own "explicit inputs, nothing implicit"
// discipline: production code (internal/report/build.go) calls [VerifyGraph]
// against the one committed, reviewed table; a test proving the upgrade or
// clamp mechanism in isolation supplies its own synthetic ceilings without
// needing to mutate (or fork) the real one.
//
// For every EffectiveEntry, the returned graph's EvidenceLevel is:
//
//  1. upgraded to E2 (RESOLVED) -- but never above it -- when hk's
//     capabilities[entry.Concept].resolve is EXACT or COMPATIBLE AND the
//     entry's own Provenance shows a real merge/composition operator
//     actually ran (Program and Operator both set): exactly
//     docs/architecture/reporting.md §4's E2 definition, "a qualified
//     resolver computed the effective result." An entry with no
//     Provenance.Program (internal/effective/merge.go's ResolveGroup
//     trivial "every candidate already agrees, or there is only one
//     source" fast path) is never upgraded: nothing was actually resolved
//     there beyond parsing agreement, so it stays at whatever evidence its
//     Candidates already carried.
//  2. clamped to ceilings' declared cap for (host, entry.Concept) --
//     regardless of step 1 -- with an undeclared cell honestly clamped to
//     E1 (EvidenceLevelParsed), never left uncapped
//     (docs/architecture/evidence-ceiling.md §1).
//
// Guarantee, Reason, Provenance, and every other EffectiveEntry field pass
// through byte-for-byte unchanged: Evidence and Guarantee are independent
// dimensions (docs/architecture/reporting.md §5), and this function's whole
// job is re-deriving evidence, never guarantee -- "verification never
// upgrades ADVISORY behavior to enforcement" (issue #26's own acceptance
// criterion) is a structural property of this function, not something a
// caller must remember to preserve. Confirmed is the one exception: it is
// ANDed against the final (post-clamp) level meeting the E3+ bar it already
// documents (EffectiveEntry.Confirmed's own doc comment) -- this can only
// ever turn an already-true Confirmed to false when a clamp lowers evidence
// below E3, never turn a false Confirmed true, so it never contradicts an
// already-computed "this was proven" claim with weaker verified evidence
// while also never manufacturing a stronger one.
//
// Conflicts are clamped identically (step 2 only -- a Conflict was, by
// definition, never resolved by anything, so step 1's upgrade never
// applies to one).
func VerifyGraphWithCeilings(host string, graph effective.EffectiveGraph, hk domain.HostKnowledge, ceilings []CeilingEntry) effective.EffectiveGraph {
	out := graph

	if len(graph.Entries) > 0 {
		out.Entries = make([]effective.EffectiveEntry, len(graph.Entries))
		for i, e := range graph.Entries {
			out.Entries[i] = verifyEntry(host, e, hk.Capabilities[e.Concept], ceilings)
		}
	}

	if len(graph.Conflicts) > 0 {
		out.Conflicts = make([]effective.Conflict, len(graph.Conflicts))
		for i, c := range graph.Conflicts {
			out.Conflicts[i] = verifyConflict(host, c, ceilings)
		}
	}

	return out
}

// resolveQualified mirrors internal/effective/merge.go's own unexported
// capabilityQualified predicate. Duplicated deliberately -- a three-line
// predicate is not worth exporting a new symbol from an already-merged,
// heavily-tested package for, or reaching for one of its unexported
// internals across a package boundary; see internal/context/host.go's
// lookPathIn doc comment for this project's own precedent on when a small,
// intentional duplicate beats either option.
func resolveQualified(capOps domain.CapabilityOps) bool {
	return capOps.Resolve == domain.CapabilityExact || capOps.Resolve == domain.CapabilityCompatible
}

// qualifiedResolutionRan reports whether entry's own Provenance shows a real
// merge/composition operator actually executed against a qualified resolve
// capability -- the two-part signal [VerifyGraphWithCeilings] needs to tell
// a genuine E2 resolution apart from internal/effective's trivial
// "everything already agrees" fast path.
//
// Provenance.Program alone is NOT a sufficient signal: internal/effective/
// compose.go's ComposeConcept sets Provenance.Program for every
// CONCAT_ORDERED concept (e.g. instruction, on both real, committed
// Knowledge Packs today) unconditionally, regardless of whether capOps is
// actually qualified -- resolve.go's ComputeEffectiveGraph dispatches to
// ComposeConcept without checking capabilityQualified first, unlike every
// other operator path in merge.go's ResolveGroup, which only reaches
// applyOperator (and so only ever sets Provenance.Program) after
// capabilityQualified already passed. Both of today's real manifests
// (knowledge/hosts/{codex,claude-code}/*/manifest.json) declare
// capabilities.instruction.resolve: UNKNOWN, so checking Provenance.Program
// in isolation would wrongly upgrade every real instruction composition
// entry to E2 despite an unqualified capability -- exactly the "inferred
// level" issue #26 forbids. Re-checking capOps here, independent of what
// produced Provenance, closes that gap.
func qualifiedResolutionRan(entry effective.EffectiveEntry, capOps domain.CapabilityOps) bool {
	return resolveQualified(capOps) && entry.Provenance.Program != "" && entry.Provenance.Operator != ""
}

// verifyEntry applies [VerifyGraphWithCeilings]'s per-entry contract to one
// EffectiveEntry.
func verifyEntry(host string, e effective.EffectiveEntry, capOps domain.CapabilityOps, ceilings []CeilingEntry) effective.EffectiveEntry {
	level := e.EvidenceLevel

	if qualifiedResolutionRan(e, capOps) && domain.EvidenceLevelResolved.Rank() > level.Rank() {
		level = domain.EvidenceLevelResolved
	}

	level = clampToCeiling(host, e.Concept, level, ceilings)

	e.EvidenceLevel = level
	e.Confirmed = e.Confirmed && level.Rank() >= domain.EvidenceLevelHostReported.Rank()
	return e
}

// verifyConflict applies [VerifyGraphWithCeilings]'s clamp-only contract to
// one Conflict.
func verifyConflict(host string, c effective.Conflict, ceilings []CeilingEntry) effective.Conflict {
	c.EvidenceLevel = clampToCeiling(host, c.Concept, c.EvidenceLevel, ceilings)
	return c
}

// clampToCeiling caps level at ceilings' declared Ceiling for (host,
// concept), honestly defaulting an undeclared cell to E1 (EvidenceLevelParsed)
// rather than leaving it unclamped (docs/architecture/evidence-ceiling.md
// §1). It never raises level -- only [qualifiedResolutionRan]'s explicit,
// justified upgrade does that -- so a level already at or below the ceiling
// passes through unchanged.
func clampToCeiling(host, concept string, level domain.EvidenceLevel, ceilings []CeilingEntry) domain.EvidenceLevel {
	ceiling, ok := CeilingFor(ceilings, host, concept)
	if !ok {
		ceiling = domain.EvidenceLevelParsed
	}
	if level.Rank() > ceiling.Rank() {
		return ceiling
	}
	return level
}
