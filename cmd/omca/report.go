package main

import (
	"fmt"
	"io"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// runReport implements `omca report [--json]` (docs/architecture/
// reporting.md §10, issue #23/PR-19): builds this worktree's one immutable
// report.Artifact and projects it either as stable JSON or as a human
// Overview + Drift + Duplicate-capabilities summary.
func runReport(stdout, stderr io.Writer, args []string) int {
	jsonOut, extra, err := parseJSONOnlyFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "omca: report: %v\n", err)
		return 2
	}
	if len(extra) > 0 {
		fmt.Fprintf(stderr, "omca: report: unrecognized argument %q\n", extra[0])
		return 2
	}

	artifact, err := buildArtifactForCLI(stderr, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "omca: report: %v\n", err)
		return 1
	}

	if jsonOut {
		return writeJSON(stdout, stderr, artifact)
	}
	report.RenderReportHuman(stdout, artifact)
	return 0
}

// parseJSONOnlyFlags recognizes a bare "--json" flag anywhere in args and
// returns every other argument unchanged, in order — the shared parser for
// every one of this PR's new commands that accepts nothing but --json plus
// its own positional/flag arguments.
func parseJSONOnlyFlags(args []string) (jsonOut bool, rest []string, err error) {
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
			continue
		}
		rest = append(rest, a)
	}
	return jsonOut, rest, nil
}
