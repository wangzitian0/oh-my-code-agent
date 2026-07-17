// human.go projects [Artifact] and its query results (ExplainResult,
// CompareResult) to human-readable text. Every function here reads only
// exported fields already present in the JSON projection of the same
// value — docs/architecture/reporting.md §10's "Human output is a
// projection of the same immutable report artifact" applies literally: none
// of these functions compute anything a JSON consumer of the same value
// could not also read.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// RenderReportHuman writes a's Overview projection (docs/architecture/
// reporting.md §9's "Overview": context/identities, runtime status,
// coverage and Knowledge status, context-cost summary) plus a one-line-per-
// card Drift summary.
func RenderReportHuman(w io.Writer, a Artifact) {
	fmt.Fprintf(w, "Report %s (worktree %s, generated %s)\n", a.Report.Metadata.ID, a.Report.Metadata.Worktree, a.Report.Metadata.GeneratedAt)
	fmt.Fprintf(w, "fingerprint: %s\n\n", a.Report.Spec.Fingerprint)

	fmt.Fprintln(w, "Overview")
	fmt.Fprintf(w, "  planes: native=%d observed=%d desired=%d hostEffective=%d current=%d pending=%d\n",
		a.Report.Spec.Planes.Native, a.Report.Spec.Planes.Observed, a.Report.Spec.Planes.Desired, a.Report.Spec.Planes.HostEffective, a.Report.Spec.Planes.Current, a.Report.Spec.Planes.Pending)

	for _, h := range a.Hosts {
		fmt.Fprintf(w, "  host %s", h.Host)
		if h.HostVersion != "" {
			fmt.Fprintf(w, " (%s)", h.HostVersion)
		}
		fmt.Fprintln(w, ":")
		if h.Knowledge.Qualified {
			fmt.Fprintf(w, "    knowledge: %s (%s)\n", h.Knowledge.Status, h.Knowledge.PackID)
		} else {
			fmt.Fprintf(w, "    knowledge: unqualified (%s)\n", h.Knowledge.Reason)
		}
		if h.ContextCost != nil {
			fmt.Fprintf(w, "    context-cost: ~%d tokens (%s) [%s]\n", h.ContextCost.EstimatedTokensExcluded, h.ContextCost.Method, h.ContextCost.Confidence)
		} else {
			fmt.Fprintln(w, "    context-cost: unknown (no current generation yet)")
		}
		fmt.Fprintf(w, "    coverage: observed=%d effective=%d conflicts=%d desired=%d current=%d pending=%d\n",
			h.Planes.Observed, h.Planes.Effective, h.Planes.Conflicts, h.Planes.Desired, h.Planes.Current, h.Planes.Pending)
	}

	fmt.Fprintf(w, "\nDrift (%d root cause(s)):\n", len(a.ActionCards))
	for _, c := range a.ActionCards {
		fmt.Fprintln(w, "  "+driftCardSummaryLine(c))
	}

	if len(a.DuplicateCapabilities) > 0 {
		fmt.Fprintf(w, "\nDuplicate capabilities (%d):\n", len(a.DuplicateCapabilities))
		for _, d := range a.DuplicateCapabilities {
			fmt.Fprintln(w, "  "+duplicateCapabilitySummaryLine(d))
		}
	}
}

// RenderDriftListHuman writes one summary line per ActionCard —
// `omca drift`'s human projection.
func RenderDriftListHuman(w io.Writer, a Artifact) {
	if len(a.ActionCards) == 0 {
		fmt.Fprintln(w, "no drift")
		return
	}
	for _, c := range a.ActionCards {
		fmt.Fprintln(w, driftCardSummaryLine(c))
	}
}

// driftCardSummaryLine is the DR-017-style one-liner docs/architecture/
// reporting.md §7's worked example format condenses to for a list view.
func driftCardSummaryLine(c DriftCard) string {
	guarantee := string(c.Guarantee)
	if guarantee == "" {
		guarantee = "MIXED"
	}
	return fmt.Sprintf("%s  %-14s  %s  impact=%d projects/%d hosts/%d artifacts  guarantee=%s",
		c.ID, c.Category, c.RootCause, c.Impact.Projects, c.Impact.Hosts, c.Impact.Artifacts, guarantee)
}

// RenderDriftShowHuman writes one ActionCard's full detail (root cause,
// remediation, impact, evidence counts, guarantee, samples, and — unlike the
// list view — every Matrix row) — `omca drift show <id>`'s human
// projection, and the "every action card expands to all affected cells"
// debug invariant (reporting.md §14) made concrete.
func RenderDriftShowHuman(w io.Writer, c DriftCard) {
	fmt.Fprintf(w, "%s  %s\n\n", c.ID, c.Category)
	fmt.Fprintf(w, "Root cause    %s\n", c.RootCause)
	if c.Remediation != "" {
		fmt.Fprintf(w, "Remediation   %s\n", c.Remediation)
	}
	fmt.Fprintf(w, "Impact        %d projects x %d hosts x %d artifacts\n", c.Impact.Projects, c.Impact.Hosts, c.Impact.Artifacts)
	if len(c.EvidenceCounts) > 0 {
		levels := make([]string, 0, len(c.EvidenceCounts))
		for lvl := range c.EvidenceCounts {
			levels = append(levels, string(lvl))
		}
		sort.Strings(levels)
		parts := make([]string, 0, len(levels))
		for _, lvl := range levels {
			parts = append(parts, fmt.Sprintf("%d x %s", c.EvidenceCounts[domain.EvidenceLevel(lvl)], lvl))
		}
		fmt.Fprintf(w, "Evidence      %s\n", strings.Join(parts, ", "))
	}
	if c.Guarantee != "" {
		fmt.Fprintf(w, "Guarantee     %s\n", c.Guarantee)
	}

	fmt.Fprintf(w, "\nMatrix (%d row(s)):\n", len(c.Matrix))
	for _, m := range c.Matrix {
		fmt.Fprintf(w, "  %-20s %-20s %-10s -> %-10v  %s\n", m.ContextCell, m.EntityID, m.Field, m.Observed, m.EvidenceLevel)
	}
}

// RenderExplainHuman writes r's summary line, and — when r.Trace is set —
// the full expansion chain (docs/architecture/reporting.md §10's
// "effective value -> resolver trace -> physical sources -> Knowledge
// evidence").
func RenderExplainHuman(w io.Writer, r ExplainResult) {
	if !r.Found {
		fmt.Fprintf(w, "%s/%s on %s: not found\n", r.Concept, r.LogicalID, r.Host)
		return
	}
	if r.Conflict {
		fmt.Fprintf(w, "%s/%s on %s: CONFLICT (%s)\n", r.Concept, r.LogicalID, r.Host, r.Reason)
	} else {
		fmt.Fprintf(w, "%s/%s on %s: resolved (evidence=%s guarantee=%s confirmed=%v)\n", r.Concept, r.LogicalID, r.Host, r.EvidenceLevel, r.Guarantee, r.Confirmed)
		if r.Reason != "" {
			fmt.Fprintf(w, "  reason: %s\n", r.Reason)
		}
	}
	if r.Trace == nil {
		return
	}
	fmt.Fprintln(w, "\nResolver trace:")
	if r.Trace.ResolverTrace.Program != "" {
		fmt.Fprintf(w, "  program:  %s\n", r.Trace.ResolverTrace.Program)
	}
	if r.Trace.ResolverTrace.Operator != "" {
		fmt.Fprintf(w, "  operator: %s\n", r.Trace.ResolverTrace.Operator)
	}
	if r.Trace.ResolverTrace.SelectedSource != "" {
		fmt.Fprintf(w, "  selected: %s\n", r.Trace.ResolverTrace.SelectedSource)
	}
	for _, c := range r.Trace.ResolverTrace.Constraints {
		fmt.Fprintf(w, "  constraint: %s\n", c)
	}

	fmt.Fprintln(w, "\nPhysical sources:")
	for _, s := range r.Trace.PhysicalSources {
		fmt.Fprintf(w, "  %-8s %-10s %s  (%s, digest=%s)\n", s.EvidenceLevel, s.Disposition, s.Ref, s.Kind, shortDigest(s.ContentDigest))
	}

	if len(r.Trace.KnowledgeEvidence) > 0 {
		fmt.Fprintln(w, "\nKnowledge evidence:")
		for _, e := range r.Trace.KnowledgeEvidence {
			fmt.Fprintf(w, "  %-8s %-10s %s\n", e.ID, e.Kind, e.URL+e.Path)
		}
	}
}

// RenderMatrixHuman writes every row of one ActionCard's Matrix —
// `omca matrix <drift-id>`'s human projection (a fuller sibling of
// RenderDriftShowHuman's own Matrix section, for a caller that only wants
// the matrix).
func RenderMatrixHuman(w io.Writer, c DriftCard) {
	fmt.Fprintf(w, "%s matrix (%d row(s)):\n", c.ID, len(c.Matrix))
	for _, m := range c.Matrix {
		fmt.Fprintf(w, "  %-20s %-20s %-10s expected=%v observed=%v evidence=%s guarantee=%s\n",
			m.ContextCell, m.EntityID, m.Field, m.Expected, m.Observed, m.EvidenceLevel, m.Guarantee)
	}
}

// RenderCompareHuman writes r's side-by-side plane comparison —
// `omca compare`/`omca diff`'s shared human projection.
func RenderCompareHuman(w io.Writer, r CompareResult) {
	fmt.Fprintf(w, "%s vs %s on %s (%d entit(y/ies)):\n", r.PlaneA, r.PlaneB, r.Host, len(r.Rows))
	for _, row := range r.Rows {
		marker := "  "
		if row.Differs {
			marker = "! "
		}
		fmt.Fprintf(w, "%s%-14s %-30s  %s=%s  %s=%s\n", marker, row.Concept, row.ID, r.PlaneA, planeRowLabel(row.A), r.PlaneB, planeRowLabel(row.B))
	}
}

func planeRowLabel(r *PlaneRow) string {
	if r == nil {
		return "absent"
	}
	if r.Active {
		return "active"
	}
	return "inactive"
}

func duplicateCapabilitySummaryLine(d DuplicateCapabilityEntry) string {
	kinds := make([]string, 0, len(d.Sources))
	for _, s := range d.Sources {
		kinds = append(kinds, fmt.Sprintf("%s:%s", s.Kind, s.Owner))
	}
	return fmt.Sprintf("%-24s  %s  ~%d redundant tokens (%s)", d.Fingerprint, strings.Join(kinds, ", "), d.ContextCost.EstimatedTokens, d.ContextCost.Confidence)
}

func shortDigest(d string) string {
	if len(d) > 19 {
		return d[:19]
	}
	return d
}
