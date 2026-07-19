package tui

import (
	"fmt"
	"strings"
)

// renderConfirmScreen renders review as the one-screen "reviewed Change
// Set" issue #35's AC describes: every runtime.ProposedChange DiffProposed
// Changes found, each paired with its runtime.ClassifyChange verdict
// (Class, whether it RequiresConfirmation, and its human-readable
// Explanation) -- docs/product/requirements.md §7's risk-based confirmation
// table, rendered faithfully rather than re-derived. A single approval key
// (Model's approveReview) applies every one of these at once; this screen
// is what the operator reviews before pressing it.
func renderConfirmScreen(review changeReview) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review Change Set — %s\n\n", review.Host)

	if len(review.Changes) == 0 {
		fmt.Fprintln(&b, "No changes require review for this pending generation.")
		fmt.Fprintln(&b, "\nPress y to activate; n/esc to cancel (the pending generation stays staged).")
		return b.String()
	}

	for i, c := range review.Changes {
		req := review.Requirements[i]
		id := c.AssetID
		if id == "" {
			id = "(none)"
		}
		confirmWord := "auto-stages, no confirmation required"
		if req.RequiresConfirmation {
			confirmWord = "REQUIRES CONFIRMATION"
		}
		fmt.Fprintf(&b, "  [%d] %s %s\n", i+1, c.Kind, id)
		fmt.Fprintf(&b, "      class=%s -- %s\n", req.Class, confirmWord)
		fmt.Fprintf(&b, "      %s\n", req.Explanation)
	}

	fmt.Fprintln(&b, "\nPress y to approve this ENTIRE reviewed Change Set and activate; n/esc to cancel (the pending generation stays staged).")
	return b.String()
}
