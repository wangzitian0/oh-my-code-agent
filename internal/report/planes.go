package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
)

// ParsePlane maps a CLI-facing plane name (case-insensitive: "native",
// "observed", "desired", "effective"/"host_effective", "current",
// "pending") to a Plane, for `omca compare`/`omca diff`'s argument parsing.
func ParsePlane(s string) (Plane, error) {
	switch normalizePlaneArg(s) {
	case "native":
		return PlaneNative, nil
	case "observed":
		return PlaneObserved, nil
	case "desired":
		return PlaneDesired, nil
	case "effective", "host_effective", "hosteffective":
		return PlaneEffective, nil
	case "current":
		return PlaneCurrent, nil
	case "pending":
		return PlanePending, nil
	default:
		return "", fmt.Errorf("unknown plane %q (want one of native, observed, desired, effective, current, pending)", s)
	}
}

func normalizePlaneArg(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c == '-' {
			c = '_'
		}
		out = append(out, c)
	}
	return string(out)
}

// planeKey is one entity's identity within a plane row map: (concept, id).
type planeKey struct{ concept, id string }

// planeRows extracts one host's plane view from hd, an already-resolved
// HostDebug — the "host has no Debug entry at all" case is ComparePlanes'
// own concern (its ok return value), decided before planeRows is ever
// called; by the time hd reaches here, the host is known to have been
// built. A plane with zero entities (a genuinely empty CURRENT generation,
// for instance) is simply an empty map, indistinguishable at this layer
// from "this plane kind doesn't apply" — that distinction, if needed,
// belongs to the caller too.
//
// NATIVE and OBSERVED project identically: this package's Debug data has no
// separate raw-vs-parsed representation (see types.go's Plane doc comment) —
// both read hd.Candidates, keyed by (concept, ref).
//
// ID-scheme reconciliation (this doc comment is the map every other plane's
// keying choice below should be read against): NATIVE/OBSERVED/HOST_EFFECTIVE/
// CURRENT/PENDING each learned their entity-identity scheme from a different
// package, and for the mcp_server concept those three schemes essentially
// never intersect on their own —
//
//   - NATIVE/OBSERVED (hd.Candidates, internal/effective.ExtractCandidates)
//     key by Candidate.Ref, a *physical* reference: one Candidate per server
//     definition found inside a registration file (e.g.
//     "codex-home/config.toml#mcp_servers.shared-tools" — one file can
//     define several servers).
//   - HOST_EFFECTIVE (hd.Graph.Entries/.Conflicts, internal/effective's
//     Identity Matcher) keys by LogicalID, a *logical* identity (e.g.
//     "stdio|shared-tools") that has no necessary textual relationship to
//     any Candidate.Ref at all.
//   - CURRENT/PENDING (hd.CurrentSources/PendingSources,
//     domain.GenerationSourceEntry) key by Source, a bare *file path* with
//     no per-server fragment — internal/runtime's compiler records one
//     entry per Observation (one whole file), not per server, because it
//     copies/excludes whole files rather than individual server
//     definitions.
//
// Left alone, every mcp_server row would carry a different, un-correlatable
// ID depending on which two planes are being compared, and `omca compare`/
// `omca diff` would report every mcp_server entity as "different" between
// any two planes regardless of whether the underlying state actually
// agrees. This function closes that gap using data each side already
// computes, without touching any frozen domain schema:
//
//   - HOST_EFFECTIVE emits one row per physical ref named in
//     EffectiveEntry.Provenance.ActiveSources (falling back to LogicalID only
//     when ActiveSources is empty — an unresolved Conflict, which has no
//     ActiveSources to decompose) — this aligns EFFECTIVE with NATIVE/
//     OBSERVED's Candidate.Ref keying directly, since ActiveSources is
//     itself a list of Candidate.Refs (types.go's Provenance doc comment).
//     For every concept this is a lossless refinement: instruction/skill/
//     hook/policy/plugin entries typically resolve to exactly one active
//     ref (or, for instruction's CONCAT_ORDERED composition, decompose into
//     one row per composed file — still a Candidate.Ref each), so this
//     applies uniformly rather than special-casing mcp_server.
//   - CURRENT/PENDING's addSourceRows cross-references hd.Candidates for a
//     mcp_server-concept source entry: since GenerationSourceEntry.Source is
//     always the bare file path a Candidate.Ref is built from (Ref =
//     Source + "#mcp_servers.<id>"), every Candidate whose Ref names that
//     file is a fragment the file-level Source entry actually decided
//     Included/Reason for, and gets its own row inheriting that entry's
//     outcome. This is scoped to mcp_server specifically: every other
//     concept is already 1:1 file-to-Candidate (no fragmentation), so a
//     bare-Source row is already correctly correlated with NATIVE/OBSERVED
//     and this cross-reference is a no-op for them (addSourceRows falls
//     back to the plain bare-Source row whenever no Candidate fragment
//     matches).
func planeRows(hd HostDebug, plane Plane) map[planeKey]PlaneRow {
	rows := map[planeKey]PlaneRow{}
	switch plane {
	case PlaneNative, PlaneObserved:
		for _, c := range hd.Candidates {
			k := planeKey{c.Concept, c.Ref}
			rows[k] = PlaneRow{
				Concept: c.Concept,
				ID:      c.Ref,
				Present: true,
				Active:  c.Disposition == domain.DispositionActive,
				Detail:  string(c.Disposition),
			}
		}
	case PlaneEffective:
		for _, e := range hd.Graph.Entries {
			active := e.Provenance.SelectedSource != "" || len(e.Provenance.ActiveSources) > 0
			if len(e.Provenance.ActiveSources) > 0 {
				// Decompose into one row per physical ref this entry
				// actually resolved from, so it correlates by the same
				// Candidate.Ref key NATIVE/OBSERVED use (see doc comment
				// above) instead of only ever matching by LogicalID.
				for _, ref := range e.Provenance.ActiveSources {
					k := planeKey{e.Concept, ref}
					rows[k] = PlaneRow{
						Concept: e.Concept,
						ID:      ref,
						Present: true,
						Active:  active,
						Detail:  e.Reason,
					}
				}
				continue
			}
			// No physical ref to key by (e.g. an entry whose resolution
			// named no active source) — LogicalID is the only identity
			// left, same as before this fix.
			k := planeKey{e.Concept, e.LogicalID}
			rows[k] = PlaneRow{
				Concept: e.Concept,
				ID:      e.LogicalID,
				Present: true,
				Active:  active,
				Detail:  e.Reason,
			}
		}
		for _, c := range hd.Graph.Conflicts {
			// A Conflict has no Provenance/ActiveSources (types.go's
			// Conflict struct) — it never resolved to any active source, so
			// LogicalID is its only identity. It still legitimately fails
			// to correlate against a NATIVE/OBSERVED Candidate.Ref, but that
			// is an honest "still unresolved" outcome, not the ID-scheme
			// bug this fix targets.
			k := planeKey{c.Concept, c.LogicalID}
			rows[k] = PlaneRow{
				Concept: c.Concept,
				ID:      c.LogicalID,
				Present: true,
				Active:  false,
				Detail:  "CONFLICT: " + c.Reason,
			}
		}
	case PlaneDesired:
		for _, asset := range hd.Desired.Assets {
			k := planeKey{string(asset.Kind), asset.ID}
			rows[k] = PlaneRow{
				Concept: string(asset.Kind),
				ID:      asset.ID,
				Present: true,
				Active:  asset.Active,
				Detail:  asset.Reason,
			}
		}
	case PlaneCurrent:
		addSourceRows(rows, hd.CurrentSources, hd.Candidates)
	case PlanePending:
		addSourceRows(rows, hd.PendingSources, hd.Candidates)
	}
	return rows
}

// addSourceRows projects sources (a CURRENT or PENDING generation's flat,
// file-granular Sources list) into rows, cross-referencing candidates (the
// same host's NATIVE/OBSERVED Candidate inventory) to recover per-server
// identity for mcp_server entries — see planeRows' doc comment for why this
// reconciliation is needed and why it is scoped to mcp_server alone.
func addSourceRows(rows map[planeKey]PlaneRow, sources []domain.GenerationSourceEntry, candidates []effective.Candidate) {
	for _, s := range sources {
		if s.Source == "" {
			// A capability-gap placeholder entry (internal/runtime/
			// compile.go's claudeConfigDirExclusionGapSources, issue #47):
			// it describes a whole exclusion *class*, not one discovered
			// physical Source, so there is no file path to key by. Falling
			// back to the bare Concept string here would produce a row
			// literally ID'd "mcp_server" or "skill" — indistinguishable at
			// a glance from a real, oddly-path-named source. Use a
			// synthetic ID that is unambiguous in human output instead.
			id := "capability-gap:" + s.Concept
			if s.Scope != "" {
				id += ":" + s.Scope
			}
			k := planeKey{s.Concept, id}
			rows[k] = PlaneRow{
				Concept: s.Concept,
				ID:      id,
				Present: true,
				Active:  s.Included,
				Detail:  s.Reason,
			}
			continue
		}

		if s.Concept == "mcp_server" {
			matched := false
			for _, c := range candidates {
				if c.Concept != "mcp_server" {
					continue
				}
				// A Candidate extracted from this source's file is either
				// the file itself (no fragmentation happened, e.g. a parse
				// failure fell back to one Candidate for the whole file) or
				// carries a "#mcp_servers.<id>"-style fragment built from
				// exactly this path (extract.go's extractMCPServerCandidates).
				if c.Ref != s.Source && !strings.HasPrefix(c.Ref, s.Source+"#") {
					continue
				}
				matched = true
				k := planeKey{s.Concept, c.Ref}
				rows[k] = PlaneRow{
					Concept: s.Concept,
					ID:      c.Ref,
					Present: true,
					Active:  s.Included,
					Detail:  s.Reason,
				}
			}
			if matched {
				continue
			}
		}

		k := planeKey{s.Concept, s.Source}
		rows[k] = PlaneRow{
			Concept: s.Concept,
			ID:      s.Source,
			Present: true,
			Active:  s.Included,
			Detail:  s.Reason,
		}
	}
}

// ComparePlanes builds a's compare/diff projection for host between planeA
// and planeB — `omca compare --native --current`/`omca diff current
// pending`'s shared engine. ok is false when host has no Debug data at all
// (never built by Build); a present-but-empty comparison (host built, but
// e.g. no PENDING generation exists yet) still returns ok == true.
func ComparePlanes(a Artifact, host string, planeA, planeB Plane) (CompareResult, bool) {
	hd, ok := a.Debug[host]
	if !ok {
		return CompareResult{}, false
	}

	rowsA := planeRows(hd, planeA)
	rowsB := planeRows(hd, planeB)

	keys := make(map[planeKey]bool, len(rowsA)+len(rowsB))
	for k := range rowsA {
		keys[k] = true
	}
	for k := range rowsB {
		keys[k] = true
	}
	ordered := make([]planeKey, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].concept != ordered[j].concept {
			return ordered[i].concept < ordered[j].concept
		}
		return ordered[i].id < ordered[j].id
	})

	rows := make([]CompareRow, 0, len(ordered))
	for _, k := range ordered {
		rowA, hasA := rowsA[k]
		rowB, hasB := rowsB[k]
		row := CompareRow{Concept: k.concept, ID: k.id}
		if hasA {
			ra := rowA
			row.A = &ra
		}
		if hasB {
			rb := rowB
			row.B = &rb
		}
		row.Differs = hasA != hasB || rowA.Active != rowB.Active
		rows = append(rows, row)
	}

	return CompareResult{Host: host, PlaneA: planeA, PlaneB: planeB, Rows: rows}, true
}
