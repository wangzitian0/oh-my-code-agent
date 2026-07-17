package main

import (
	"fmt"
	"io"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// runMatrix implements `omca matrix <drift-id> [--json]`: the complete,
// queryable Matrix for one ActionCard (docs/architecture/reporting.md §7:
// "The report always exposes the complete matrix count and query").
func runMatrix(stdout, stderr io.Writer, args []string) int {
	jsonOut, rest, _ := parseJSONOnlyFlags(args)
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "omca: matrix: usage: omca matrix <drift-id> [--json]")
		return 2
	}
	id := rest[0]

	artifact, err := buildArtifactForCLI(stderr, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "omca: matrix: %v\n", err)
		return 1
	}

	card, ok := findDriftCard(artifact, id)
	if !ok {
		fmt.Fprintf(stderr, "omca: matrix: no drift card with ID %q (run `omca drift` to list current IDs)\n", id)
		return 1
	}

	if jsonOut {
		return writeJSON(stdout, stderr, card.Matrix)
	}
	report.RenderMatrixHuman(stdout, card)
	return 0
}
