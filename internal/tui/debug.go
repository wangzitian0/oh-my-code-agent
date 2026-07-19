// debug.go implements issue #36's Debug drill-down: from the Drift view's
// currently selected action card, open its complete Host Matrix, then drill
// into one Matrix row (cell/entity) to see that entity's Effective State,
// Precedence Trace, Evidence, and Native Artifacts — docs/architecture/
// reporting.md §9's Debug tree ("Native vs Current vs Pending", "Effective
// State", "Host Matrix", "Precedence Trace", "Evidence", "Native
// Artifacts") made navigable, and §14's debug invariants
// ("every action card expands to all affected cells," "every cell expands
// to desired and observed values," "every effective value expands to its
// resolver trace," "every resolver trace expands to physical sources and
// Knowledge evidence") proven rather than merely rendered once and
// forgotten.
//
// Every pane here is a thin wrapper around a function/field that already
// exists in internal/report — nothing in this file recomputes drift,
// resolves precedence, or re-derives evidence:
//
//   - Host Matrix:              report.RenderMatrixHuman (DriftCard.Matrix)
//   - Effective State + Precedence Trace: report.Explain(..., trace=true) +
//     report.RenderExplainHuman
//   - Native vs Current vs Pending:       report.ComparePlanes +
//     report.RenderCompareHuman
//   - Evidence:                 a.Debug[host].Evidence, filtered to the
//     entity currently drilled into (evidenceForEntity)
//   - Native Artifacts:         the trace's own PhysicalSources
//     (already a's Debug[host].Candidates, looked up by report.Explain),
//     joined against a.Debug[host].Observations by source path
//     (observationsForPath) — a plain filter/join for display, not a second
//     computation of any drift/effective/resolver logic.
package tui

import (
	"fmt"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// debugLevel names Model's own Debug drill-down stack depth (issue #36):
// debugLevelMatrix shows one ActionCard's complete Host Matrix with a row
// cursor; debugLevelEntity — reached by drilling into a selected row — shows
// that row's Effective State, Precedence Trace, Evidence, and Native
// Artifacts. This is a genuine two-level stack, not a single flat screen:
// 'esc'/'b' at debugLevelEntity steps back to debugLevelMatrix, and only a
// second 'esc'/'b' from there returns to the Drift view (Model.updateDebug).
type debugLevel int

const (
	debugLevelMatrix debugLevel = iota
	debugLevelEntity
)

// splitEntityID recovers the (concept, logicalID) pair a DriftAssertion's
// EntityID encodes — internal/report/adapter.go's own stable
// "<concept>/<logicalID>" convention (conflictSignals' own doc comment).
// Splitting on the FIRST "/" is safe even though logicalID itself can
// legitimately contain "/" (e.g. an instruction's "scope|source.path"
// identity, where source.path is a real file path with slashes of its
// own): every ontology concept name (mcp_server, skill, instruction,
// permission, hook, policy, plugin) is a bare snake_case token that never
// itself contains a slash, so the first "/" is always the boundary between
// them.
func splitEntityID(entityID string) (concept, logicalID string, ok bool) {
	i := strings.Index(entityID, "/")
	if i < 0 {
		return "", "", false
	}
	return entityID[:i], entityID[i+1:], true
}

// findCardByID returns the ActionCard in a.ActionCards carrying the given
// content-addressed ID — the same lookup cmd/omca/reportjson.go's
// findDriftCard performs for `omca drift show`/`omca matrix`. That helper
// is unexported in cmd/omca (package main), which internal/tui can never
// import (see doc.go's "Action layer" section for the same constraint on
// actions.go's own mirrored helpers); this is a two-line loop over
// Artifact's own exported ActionCards/DriftCard.ID fields, not a
// reimplementation of any drift-engine logic.
func findCardByID(a report.Artifact, id string) (report.DriftCard, bool) {
	for _, c := range a.ActionCards {
		if c.ID == id {
			return c, true
		}
	}
	return report.DriftCard{}, false
}

// evidenceForEntity returns hd.Evidence's records whose Subject names
// exactly (concept, logicalID) — the Debug tree's own distinct "Evidence"
// pane (docs/architecture/reporting.md §9's per-claim domain.Evidence
// records, internal/domain/evidence.go's frozen contract: EvidenceLevel,
// Guarantee, Method, Source, KnowledgeRef). This is a different, richer
// dataset than the Knowledge Pack citation list report.RenderExplainHuman's
// own "Knowledge evidence:" section already prints from
// ExplainTrace.KnowledgeEvidence (domain.KnowledgeEvidenceRef, a host-level
// Knowledge Pack citation, not a per-claim record) — both are shown, never
// conflated.
func evidenceForEntity(hd report.HostDebug, concept, logicalID string) []domain.Evidence {
	var out []domain.Evidence
	for _, e := range hd.Evidence {
		if e.Spec.Subject.Concept == concept && e.Spec.Subject.LogicalID == logicalID {
			out = append(out, e)
		}
	}
	return out
}

// evidenceSourceString renders one domain.EvidenceSource as a single,
// unambiguous string: URL is preferred (a citation to an external/pinned
// official source is the more useful thing to show a human than a local
// path), Path is the fallback when no URL is recorded, and the two are never
// concatenated without a separator -- printing "%s%s" directly (an earlier
// version of this function) could silently glue a URL and a Path together
// into one indistinguishable, ambiguous token when both fields happened to
// be set (a real Copilot review finding on this PR).
func evidenceSourceString(src domain.EvidenceSource) string {
	switch {
	case src.URL != "" && src.Path != "":
		return src.URL + " (" + src.Path + ")"
	case src.URL != "":
		return src.URL
	default:
		return src.Path
	}
}

// observationsForPath returns observations whose own Source.Path matches
// path — the raw Adapter Record (domain.Observation) a physical Candidate
// (and therefore an ExplainTrace PhysicalSource) was extracted from. This
// closes docs/architecture/reporting.md §14's "every resolver trace expands
// to physical sources and Knowledge evidence" chain down to the literal
// observed file record, for the Debug tree's own "Native Artifacts" pane —
// report.Artifact.Debug[host].Observations, joined by path, not derived.
func observationsForPath(observations []domain.Observation, path string) []domain.Observation {
	if path == "" {
		return nil
	}
	var out []domain.Observation
	for _, o := range observations {
		if o.Spec.Source.Path == path {
			out = append(out, o)
		}
	}
	return out
}

// renderPlanesComparison renders host's "Native vs Current vs Pending" pane
// (docs/architecture/reporting.md §9) as two side-by-side plane
// comparisons — report.ComparePlanes/report.RenderCompareHuman verbatim,
// the exact engine `omca compare --native --current`/`omca diff current
// pending` already use. NATIVE and OBSERVED project identically in this
// package's data (types.go's Plane doc comment), so NATIVE vs CURRENT is
// the honest "what's on disk vs what the active runtime generation used"
// comparison; CURRENT vs PENDING is the restart-workflow comparison
// docs/architecture/reporting.md §2 describes.
func renderPlanesComparison(a report.Artifact, host string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Native vs Current vs Pending — %s\n", host)

	if r, ok := report.ComparePlanes(a, host, report.PlaneNative, report.PlaneCurrent); ok {
		fmt.Fprintln(&b)
		report.RenderCompareHuman(&b, r)
	}
	if r, ok := report.ComparePlanes(a, host, report.PlaneCurrent, report.PlanePending); ok {
		fmt.Fprintln(&b)
		report.RenderCompareHuman(&b, r)
	}
	return b.String()
}

// renderDebugMatrix renders debugLevelMatrix: card's complete Host Matrix
// (report.RenderMatrixHuman verbatim — "every action card expands to all
// affected cells," reporting.md §14), a cursor marking the row `enter`
// would drill into, and — when showPlanes is toggled on — that row's host
// Native vs Current vs Pending comparison.
func renderDebugMatrix(a report.Artifact, card report.DriftCard, cursor int, showPlanes bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Debug > %s  %s\n", card.ID, card.Category)
	fmt.Fprintf(&b, "  Root cause   %s\n", card.RootCause)
	if card.Remediation != "" {
		fmt.Fprintf(&b, "  Remediation  %s\n", card.Remediation)
	}

	fmt.Fprintln(&b, "\nHost Matrix")
	report.RenderMatrixHuman(&b, card)

	if len(card.Matrix) == 0 {
		fmt.Fprintln(&b, "\n(no cells to drill into)")
		fmt.Fprintln(&b, "\nesc/b: back to Drift   q: quit")
		return b.String()
	}

	i := cursor % len(card.Matrix)
	if i < 0 {
		i += len(card.Matrix)
	}
	row := card.Matrix[i]
	fmt.Fprintf(&b, "\nSelected [%d/%d]  %s  host=%s\n", i+1, len(card.Matrix), row.EntityID, row.Host)

	if showPlanes {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, renderPlanesComparison(a, row.Host))
	}

	fmt.Fprintln(&b, "\nup/down: select cell   enter: resolver trace   p: toggle Native vs Current vs Pending   esc/b: back to Drift   q: quit")
	return b.String()
}

// renderDebugEntity renders debugLevelEntity: (host, concept, logicalID)'s
// Effective State + Precedence Trace (report.Explain with trace=true,
// report.RenderExplainHuman verbatim — "every effective value expands to
// its resolver trace, every resolver trace expands to physical sources and
// Knowledge evidence," reporting.md §14), plus this Debug tree's own
// distinct Evidence (evidenceForEntity) and Native Artifacts (the trace's
// own PhysicalSources, joined against raw Observations) panes, and —
// toggled — the host's Native vs Current vs Pending comparison.
func renderDebugEntity(a report.Artifact, cardID, host, concept, logicalID string, showPlanes bool) string {
	var b strings.Builder
	entityID := concept + "/" + logicalID
	fmt.Fprintf(&b, "Debug > %s > %s  (host %s)\n", cardID, entityID, host)

	result := report.Explain(a, host, concept, logicalID, true)

	fmt.Fprintln(&b, "\nEffective State & Precedence Trace")
	report.RenderExplainHuman(&b, result)

	hd := a.Debug[host]

	fmt.Fprintln(&b, "\nEvidence")
	records := evidenceForEntity(hd, concept, logicalID)
	if len(records) == 0 {
		fmt.Fprintln(&b, "  no Evidence record for this entity")
	}
	for _, e := range records {
		fmt.Fprintf(&b, "  %-46s level=%-3s guarantee=%-11s method=%s\n", e.Metadata.ID, e.Spec.Level, e.Spec.Guarantee, e.Spec.Method)
		if src := evidenceSourceString(e.Spec.Source); src != "" {
			fmt.Fprintf(&b, "      source: %s (%s)\n", src, e.Spec.Source.Kind)
		}
		if e.Spec.KnowledgeRef.ID != "" {
			fmt.Fprintf(&b, "      knowledgeRef: %s (%s)\n", e.Spec.KnowledgeRef.ID, e.Spec.KnowledgeRef.Digest)
		}
	}

	fmt.Fprintln(&b, "\nNative Artifacts")
	if result.Trace == nil || len(result.Trace.PhysicalSources) == 0 {
		fmt.Fprintln(&b, "  no physical source resolved for this entity")
	} else {
		for _, ps := range result.Trace.PhysicalSources {
			fmt.Fprintf(&b, "  %-8s %-10s %s\n", ps.EvidenceLevel, ps.Disposition, ps.Ref)
			for _, o := range observationsForPath(hd.Observations, ps.Path) {
				fmt.Fprintf(&b, "      observation %-46s disposition=%-10s evidence=%s\n", o.Metadata.ID, o.Spec.Disposition, o.Spec.EvidenceLevel)
			}
		}
	}

	if showPlanes {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, renderPlanesComparison(a, host))
	}

	fmt.Fprintln(&b, "\nesc/b: back to matrix   p: toggle Native vs Current vs Pending   q: quit")
	return b.String()
}
