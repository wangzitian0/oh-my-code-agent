package main

import (
	"fmt"
	"io"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// runDrift implements `omca drift [--json]` (the root-cause-grouped
// ActionCard list) and `omca drift show <drift-id> [--json]` (one card's
// full detail, including its complete Matrix — docs/architecture/
// reporting.md §14's "every action card expands to all affected cells").
func runDrift(stdout, stderr io.Writer, args []string) int {
	var showID string
	if len(args) > 0 && args[0] == "show" {
		if len(args) < 2 {
			fmt.Fprintln(stderr, "omca: drift show: a drift ID is required: omca drift show <drift-id> [--json]")
			return 2
		}
		showID = args[1]
		args = args[2:]
	}

	jsonOut, extra, _ := parseJSONOnlyFlags(args)
	if len(extra) > 0 {
		fmt.Fprintf(stderr, "omca: drift: unrecognized argument %q\n", extra[0])
		return 2
	}

	artifact, err := buildArtifactForCLI(stderr, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "omca: drift: %v\n", err)
		return 1
	}

	if showID != "" {
		card, ok := findDriftCard(artifact, showID)
		if !ok {
			fmt.Fprintf(stderr, "omca: drift show: no drift card with ID %q (run `omca drift` to list current IDs)\n", showID)
			return 1
		}
		if jsonOut {
			return writeJSON(stdout, stderr, card)
		}
		report.RenderDriftShowHuman(stdout, card)
		return 0
	}

	if jsonOut {
		return writeJSON(stdout, stderr, artifact.ActionCards)
	}
	report.RenderDriftListHuman(stdout, artifact)
	return 0
}
