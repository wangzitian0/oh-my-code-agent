package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// writeJSON encodes v as indented JSON to stdout — the shared "stable JSON"
// half of every `omca report/drift/explain/matrix/compare/diff --json`
// projection (docs/architecture/reporting.md §10: "All read commands
// support stable JSON output"). encoding/json marshals struct fields in
// their declared order and sorts map[string]V keys automatically, so two
// calls over an equal value byte-for-byte agree — the same
// determinism domain.CanonicalDigest itself relies on.
func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "omca: encoding JSON: %v\n", err)
		return 1
	}
	return 0
}

// findDriftCard returns the ActionCard in a whose content-addressed ID
// equals id (cardid.go's computeCardID), used by `omca drift show <id>` and
// `omca matrix <id>`.
func findDriftCard(a report.Artifact, id string) (report.DriftCard, bool) {
	for _, c := range a.ActionCards {
		if c.ID == id {
			return c, true
		}
	}
	return report.DriftCard{}, false
}

// firstHost returns the first host (in Build's own sorted order) a's Debug
// data was built for — the default target `omca explain`/`omca compare`/
// `omca diff` use when the caller does not pass --host.
func firstHost(a report.Artifact) (string, bool) {
	if len(a.Hosts) == 0 {
		return "", false
	}
	return a.Hosts[0].Host, true
}
