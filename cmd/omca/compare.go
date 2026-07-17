package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// planeFlagNames are the `--<plane>` boolean flags `omca compare` accepts,
// matching docs/architecture/reporting.md §10's literal example
// ("omca compare --native --current") and report.ParsePlane's own
// vocabulary.
var planeFlagNames = map[string]bool{
	"--native": true, "--observed": true, "--desired": true,
	"--effective": true, "--current": true, "--pending": true,
}

// runCompare implements `omca compare --<planeA> --<planeB> [--host <host>]
// [--json]`.
func runCompare(stdout, stderr io.Writer, args []string) int {
	var planeArgs []string
	host := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case planeFlagNames[a]:
			planeArgs = append(planeArgs, strings.TrimPrefix(a, "--"))
		case a == "--host":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "omca: compare: --host requires a value")
				return 2
			}
			host = args[i+1]
			i++
		case strings.HasPrefix(a, "--host="):
			host = strings.TrimPrefix(a, "--host=")
		default:
			rest = append(rest, a)
		}
	}
	if len(planeArgs) != 2 {
		fmt.Fprintln(stderr, "omca: compare: exactly two plane flags are required, e.g. omca compare --native --current (want two of --native, --observed, --desired, --effective, --current, --pending)")
		return 2
	}

	jsonOut, extra, _ := parseJSONOnlyFlags(rest)
	if len(extra) > 0 {
		fmt.Fprintf(stderr, "omca: compare: unrecognized argument %q\n", extra[0])
		return 2
	}

	return runPlaneComparison(stdout, stderr, "compare", planeArgs[0], planeArgs[1], host, jsonOut)
}

// runPlaneComparison is the shared engine `omca compare` and `omca diff`
// both call once their two plane names and options are parsed: build the
// Artifact, resolve the target host (host, or the first built host when
// empty), and project report.ComparePlanes as JSON or human text.
func runPlaneComparison(stdout, stderr io.Writer, cmdName, planeAArg, planeBArg, host string, jsonOut bool) int {
	planeA, err := report.ParsePlane(planeAArg)
	if err != nil {
		fmt.Fprintf(stderr, "omca: %s: %v\n", cmdName, err)
		return 2
	}
	planeB, err := report.ParsePlane(planeBArg)
	if err != nil {
		fmt.Fprintf(stderr, "omca: %s: %v\n", cmdName, err)
		return 2
	}

	artifact, err := buildArtifactForCLI(stderr, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "omca: %s: %v\n", cmdName, err)
		return 1
	}

	if host == "" {
		h, ok := firstHost(artifact)
		if !ok {
			fmt.Fprintf(stderr, "omca: %s: no host was built into this report (no installed/observed host)\n", cmdName)
			return 1
		}
		host = h
	}

	result, ok := report.ComparePlanes(artifact, host, planeA, planeB)
	if !ok {
		fmt.Fprintf(stderr, "omca: %s: host %q was not built into this report (no observation data)\n", cmdName, host)
		return 1
	}

	if jsonOut {
		return writeJSON(stdout, stderr, result)
	}
	report.RenderCompareHuman(stdout, result)
	return 0
}
