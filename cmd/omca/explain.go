package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// runExplain implements `omca explain <concept> <logical-id> [--host <host>]
// [--trace] [--json]` (docs/architecture/reporting.md §10). Without
// --host, every built host is searched in order (report.Build's own
// deterministic sort) for the first one that knows this (concept,
// logical-id); --trace expands the full chain "effective value -> resolver
// trace -> physical sources -> Knowledge evidence."
func runExplain(stdout, stderr io.Writer, args []string) int {
	trace := false
	host := ""
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--trace":
			trace = true
		case a == "--host":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "omca: explain: --host requires a value")
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
		fmt.Fprintln(stderr, "omca: explain: usage: omca explain <concept> <logical-id> [--host <host>] [--trace] [--json]")
		return 2
	}
	concept, logicalID := rest[0], rest[1]

	artifact, err := buildArtifactForCLI(stderr, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "omca: explain: %v\n", err)
		return 1
	}

	result, err := explainAcrossHosts(artifact, host, concept, logicalID, trace)
	if err != nil {
		fmt.Fprintf(stderr, "omca: explain: %v\n", err)
		return 1
	}

	if jsonOut {
		return writeJSON(stdout, stderr, result)
	}
	report.RenderExplainHuman(stdout, result)
	if !result.Found {
		return 1
	}
	return 0
}

// explainAcrossHosts resolves the target host (host, if non-empty,
// otherwise every built host in order) and returns the first Found
// ExplainResult, or a not-found result for the requested/first host if none
// match.
func explainAcrossHosts(a report.Artifact, host, concept, logicalID string, trace bool) (report.ExplainResult, error) {
	if host != "" {
		if _, ok := a.Debug[host]; !ok {
			return report.ExplainResult{}, fmt.Errorf("host %q was not built into this report (no observation data)", host)
		}
		return report.Explain(a, host, concept, logicalID, trace), nil
	}

	if len(a.Hosts) == 0 {
		return report.ExplainResult{}, fmt.Errorf("no host was built into this report (no installed/observed host)")
	}
	var fallback report.ExplainResult
	for i, h := range a.Hosts {
		result := report.Explain(a, h.Host, concept, logicalID, trace)
		if i == 0 {
			fallback = result
		}
		if result.Found {
			return result, nil
		}
	}
	return fallback, nil
}
