package report

import (
	"fmt"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
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

// planeRows extracts one host's plane view from a's Debug data. ok is false
// when host has no Debug entry at all (never observed/built) — a plane that
// simply has zero entities (a genuinely empty CURRENT generation, for
// instance) still returns ok == true with an empty map, which
// ComparePlanes' caller distinguishes from "this host was never built."
//
// NATIVE and OBSERVED project identically: this package's Debug data has no
// separate raw-vs-parsed representation (see types.go's Plane doc comment) —
// both read hd.Candidates, keyed by (concept, ref).
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
			k := planeKey{e.Concept, e.LogicalID}
			rows[k] = PlaneRow{
				Concept: e.Concept,
				ID:      e.LogicalID,
				Present: true,
				Active:  e.Provenance.SelectedSource != "" || len(e.Provenance.ActiveSources) > 0,
				Detail:  e.Reason,
			}
		}
		for _, c := range hd.Graph.Conflicts {
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
		addSourceRows(rows, hd.CurrentSources)
	case PlanePending:
		addSourceRows(rows, hd.PendingSources)
	}
	return rows
}

func addSourceRows(rows map[planeKey]PlaneRow, sources []domain.GenerationSourceEntry) {
	for _, s := range sources {
		id := s.Source
		if id == "" {
			id = s.Concept
		}
		k := planeKey{s.Concept, id}
		rows[k] = PlaneRow{
			Concept: s.Concept,
			ID:      id,
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
