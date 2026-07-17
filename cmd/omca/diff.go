package main

import (
	"fmt"
	"io"
	"strings"
)

// runDiff implements `omca diff <planeA> <planeB> [--host <host>] [--json]`
// (docs/architecture/reporting.md §10's literal example: "omca diff current
// pending") — the positional-argument sibling of `omca compare`'s flag
// form; both project the exact same report.ComparePlanes engine
// (runPlaneComparison, compare.go).
func runDiff(stdout, stderr io.Writer, args []string) int {
	host := ""
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--host":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "omca: diff: --host requires a value")
				return 2
			}
			host = args[i+1]
			i++
		case strings.HasPrefix(a, "--host="):
			host = strings.TrimPrefix(a, "--host=")
		default:
			positional = append(positional, a)
		}
	}

	jsonOut, rest, _ := parseJSONOnlyFlags(positional)
	if len(rest) != 2 {
		fmt.Fprintln(stderr, "omca: diff: usage: omca diff <planeA> <planeB> [--host <host>] [--json] (e.g. omca diff current pending)")
		return 2
	}

	return runPlaneComparison(stdout, stderr, "diff", rest[0], rest[1], host, jsonOut)
}
