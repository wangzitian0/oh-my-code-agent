package tui

import (
	"fmt"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// RenderOverview projects a's "Context and identities", "Runtime status",
// "Coverage and Knowledge status", and "Context-cost summary" sections
// (docs/architecture/reporting.md §9's Overview) — this package's own
// content-equivalent of report.RenderReportHuman's Overview half, laid out
// for a navigable view rather than a scroll of CLI text. Every line here
// is already present, verbatim, in a's JSON projection: this function
// computes nothing report.Build did not already compute.
func RenderOverview(a report.Artifact) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Report %s\n", a.Report.Metadata.ID)
	fmt.Fprintf(&b, "Worktree %s, generated %s\n", a.Report.Metadata.Worktree, a.Report.Metadata.GeneratedAt)
	fmt.Fprintf(&b, "Fingerprint %s\n", a.Report.Spec.Fingerprint)

	p := a.Report.Spec.Planes
	fmt.Fprintf(&b, "\nPlanes  native=%d observed=%d desired=%d hostEffective=%d current=%d pending=%d\n",
		p.Native, p.Observed, p.Desired, p.HostEffective, p.Current, p.Pending)

	if len(a.Hosts) == 0 {
		fmt.Fprintln(&b, "\nNo host was observed for this report.")
		return b.String()
	}

	fmt.Fprintf(&b, "\nHosts (%d)\n", len(a.Hosts))
	for _, h := range a.Hosts {
		fmt.Fprintf(&b, "  %s", h.Host)
		if h.HostVersion != "" {
			fmt.Fprintf(&b, " (%s)", h.HostVersion)
		}
		fmt.Fprintln(&b)

		if h.Knowledge.Qualified {
			fmt.Fprintf(&b, "    knowledge   %s (%s)\n", h.Knowledge.Status, h.Knowledge.PackID)
		} else {
			fmt.Fprintf(&b, "    knowledge   unqualified (%s)\n", h.Knowledge.Reason)
		}

		if h.ContextCost != nil {
			fmt.Fprintf(&b, "    context-cost ~%d tokens (%s) [%s]\n", h.ContextCost.EstimatedTokensExcluded, h.ContextCost.Method, h.ContextCost.Confidence)
		} else {
			fmt.Fprintln(&b, "    context-cost unknown (no current generation yet)")
		}

		fmt.Fprintf(&b, "    coverage    observed=%d effective=%d conflicts=%d desired=%d current=%d pending=%d\n",
			h.Planes.Observed, h.Planes.Effective, h.Planes.Conflicts, h.Planes.Desired, h.Planes.Current, h.Planes.Pending)
	}

	fmt.Fprintf(&b, "\nDrift: %d root cause(s) — see the Drift view\n", len(a.ActionCards))
	if len(a.DuplicateCapabilities) > 0 {
		fmt.Fprintf(&b, "Duplicate capabilities: %d\n", len(a.DuplicateCapabilities))
	}

	return b.String()
}
