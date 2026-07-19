package tui

import (
	"fmt"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// RenderDrift projects a's root-cause action cards (docs/architecture/
// reporting.md §7: "Human output groups by root cause + remediation +
// outcome class + adapter version. One company Profile issue affecting
// forty artifacts is one action card, not forty top-level alerts."). Each
// card is shown as logical ID + reason (RootCause) + action (Remediation)
// + impact + guarantee — never the underlying Matrix's native
// Candidate.Ref/resolver-operator detail, which belongs to `omca drift
// show <id>`/`omca matrix <id>` (Debug tier, issue #36), not this default
// view.
func RenderDrift(a report.Artifact) string {
	var b strings.Builder

	if len(a.ActionCards) == 0 {
		fmt.Fprintln(&b, "No drift: every observed asset matches its desired/effective state.")
		return b.String()
	}

	fmt.Fprintf(&b, "Drift: %d root cause(s)\n", len(a.ActionCards))
	for _, c := range a.ActionCards {
		fmt.Fprintf(&b, "\n%s  %s\n", c.ID, c.Category)
		fmt.Fprintf(&b, "  Reason      %s\n", c.RootCause)
		if c.Remediation != "" {
			fmt.Fprintf(&b, "  Action      %s\n", c.Remediation)
		}
		fmt.Fprintf(&b, "  Impact      %d project(s) x %d host(s) x %d artifact(s)\n", c.Impact.Projects, c.Impact.Hosts, c.Impact.Artifacts)
		if c.Guarantee != "" {
			fmt.Fprintf(&b, "  Guarantee   %s\n", c.Guarantee)
		}

		if len(c.Samples) == 0 {
			continue
		}
		fmt.Fprintln(&b, "  Samples")
		for _, s := range c.Samples {
			cell := s.ContextCell
			if cell == "" {
				cell = "-"
			}
			fmt.Fprintf(&b, "    %-30s %s\n", s.EntityID, cell)
		}
	}

	return b.String()
}
