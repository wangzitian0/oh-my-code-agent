package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// RenderGenerations projects each host's Current/Pending generation
// (docs/architecture/reporting.md §9's Generations sub-sections; this
// Artifact shape has no History list yet, so that third sub-section is
// honestly omitted rather than faked). Each source line shows Concept +
// included/excluded + reason (the "reason" and "action" the default view
// discipline asks for) — never GenerationSourceEntry.Source, which is a
// bare native file path.
func RenderGenerations(a report.Artifact) string {
	var b strings.Builder

	hosts := make([]string, 0, len(a.Debug))
	for host := range a.Debug {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	if len(hosts) == 0 {
		fmt.Fprintln(&b, "No host debug data available for this report.")
		return b.String()
	}

	for i, host := range hosts {
		if i > 0 {
			fmt.Fprintln(&b)
		}
		hd := a.Debug[host]
		fmt.Fprintf(&b, "Generations — %s\n", host)
		renderGenerationPointer(&b, "Current", hd.CurrentGenerationID, hd.CurrentSources)
		renderGenerationPointer(&b, "Pending", hd.PendingGenerationID, hd.PendingSources)
	}

	return b.String()
}

func renderGenerationPointer(b *strings.Builder, label, generationID string, sources []domain.GenerationSourceEntry) {
	if generationID == "" {
		fmt.Fprintf(b, "  %s: none\n", label)
		return
	}
	fmt.Fprintf(b, "  %s: %s\n", label, generationID)

	included, excluded := 0, 0
	for _, s := range sources {
		if s.Included {
			included++
		} else {
			excluded++
		}
	}
	fmt.Fprintf(b, "    %d source(s): %d included, %d excluded\n", len(sources), included, excluded)

	// A regression test (Copilot review finding on this PR): Included+Concept
	// alone is not a total order -- two entries with the same Concept and
	// Included status (common in real data, e.g. two excluded skills) tie,
	// and sort.Slice gives no guarantee about tie order (unlike
	// sort.SliceStable, and even that would only preserve input order, not a
	// deterministic one). An undefined tie order risks golden-file churn
	// across Go versions/runs. Scope/Host/Source/Reason chain in as
	// tiebreakers to produce a full deterministic order -- Source is a
	// native path and is used here only as a sort key, never displayed
	// (this function's own printed columns stay Concept/status/Reason only,
	// unchanged).
	sorted := make([]domain.GenerationSourceEntry, len(sources))
	copy(sorted, sources)
	sort.Slice(sorted, func(i, j int) bool {
		a, c := sorted[i], sorted[j]
		if a.Included != c.Included {
			return a.Included // included entries first
		}
		if a.Concept != c.Concept {
			return a.Concept < c.Concept
		}
		if a.Scope != c.Scope {
			return a.Scope < c.Scope
		}
		if a.Host != c.Host {
			return a.Host < c.Host
		}
		if a.Source != c.Source {
			return a.Source < c.Source
		}
		return a.Reason < c.Reason
	})
	for _, s := range sorted {
		status := "included"
		if !s.Included {
			status = "excluded"
		}
		fmt.Fprintf(b, "      %-12s %-8s %s\n", s.Concept, status, s.Reason)
	}
}
