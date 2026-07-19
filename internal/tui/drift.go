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
		fmt.Fprintf(&b, "  Reason      %s\n", sanitizeDefaultTierText(c.RootCause))
		if c.Remediation != "" {
			fmt.Fprintf(&b, "  Action      %s\n", sanitizeDefaultTierText(c.Remediation))
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

// sanitizeDefaultTierText strips native precedence-program/merge-operator
// detail from a RootCause/Remediation string before it reaches this
// package's default Drift view (a real Copilot review finding on this PR):
// internal/effective/merge.go's own Conflict.Reason can read, verbatim,
// "precedence program %q declares operator %s for concept %q ..." or "...
// its operator (%q) is not one of the nine closed docs/ontology/README.md
// §3.1 operators ..." -- exactly the "precedence ranks" this view's own AC
// (and docs/architecture/reporting.md §9) says belongs to Debug/Explain
// (`omca drift show <id>`/`omca matrix <id>`, issue #36), not this default
// view. Detected by keyword rather than matching the two known message
// templates verbatim, so a future wording change to those templates in
// internal/effective/merge.go does not silently start leaking through this
// check again undetected.
func sanitizeDefaultTierText(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "precedence program") || strings.Contains(lower, "operator") {
		return "sources disagree and no resolver could select a single winner (see the Debug view for the full precedence trace)"
	}
	return s
}
