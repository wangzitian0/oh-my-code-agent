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

	sorted := make([]domain.GenerationSourceEntry, len(sources))
	copy(sorted, sources)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Included != sorted[j].Included {
			return sorted[i].Included // included entries first
		}
		return sorted[i].Concept < sorted[j].Concept
	})
	for _, s := range sorted {
		status := "included"
		if !s.Included {
			status = "excluded"
		}
		fmt.Fprintf(b, "      %-12s %-8s %s\n", s.Concept, status, s.Reason)
	}
}
