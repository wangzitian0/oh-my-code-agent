package main

import (
	"fmt"
	"io"

	"github.com/wangzitian0/oh-my-code-agent/internal/statereport"
)

// runState implements `omca state [--json]`: a read-only measurement of what
// host-written state this installation is holding, by worktree and by the
// sharing class internal/auth assigns it.
//
// It is its own entry rather than part of `omca report` because it is the
// one question that is not per-worktree. The defect it exists to surface --
// a class that promises sharing while N worktrees each keep their own copy
// -- is invisible from inside any single worktree, which is exactly why it
// went unnoticed until a native home reached 126 MB.
//
// Read-only: it stats and walks, never opening a file, so no session
// content, credential or log line can reach this output.
func runState(stdout, stderr io.Writer, args []string) int {
	jsonOut, extra, err := parseJSONOnlyFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "omca: state: %v\n", err)
		return 2
	}
	if len(extra) > 0 {
		fmt.Fprintf(stderr, "omca: state: unrecognized argument %q\n", extra[0])
		return 2
	}

	stateRoot, err := realStateRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: state: %v\n", err)
		return 1
	}
	result, err := statereport.Measure(stateRoot)
	if err != nil {
		fmt.Fprintf(stderr, "omca: state: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, result)
	}

	fmt.Fprintf(stdout, "omca state: %s\n\n", result.StateRoot)
	fmt.Fprintf(stdout, "%d worktree(s), %s of host-written state\n\n", result.Worktrees, humanBytes(result.TotalBytes))

	fmt.Fprintln(stdout, "By sharing class:")
	for _, c := range result.ByClass {
		label := string(c.Class)
		if !c.Classified {
			label = "(unclassified — no row in internal/auth's table)"
		}
		fmt.Fprintf(stdout, "  %-52s %8s  %d entries\n", label, humanBytes(c.Bytes), c.Entries)
	}

	if len(result.Duplication) > 0 {
		fmt.Fprintln(stdout, "\nShared in name only — a class that promises sharing, stored per worktree:")
		var wasted int64
		for _, d := range result.Duplication {
			wasted += d.WastedBytes
			fmt.Fprintf(stdout, "  %-30s %-18s %d copies, %8s wasted beyond the first\n",
				d.Host+"/"+d.Name, d.Class, d.Copies, humanBytes(d.WastedBytes))
		}
		fmt.Fprintf(stdout, "\n  %s is held in copies the classification says are unnecessary.\n", humanBytes(wasted))
		fmt.Fprintln(stdout, "  Nothing applies the sharing allowlist yet (internal/auth has no production")
		fmt.Fprintln(stdout, "  caller), so these classes are true on paper and untrue on disk.")
	}

	fmt.Fprintln(stdout, "\nLargest entries:")
	for i, e := range result.Entries {
		if i >= 10 {
			break
		}
		class := string(e.Class)
		if !e.Classified {
			class = "unclassified"
		}
		fmt.Fprintf(stdout, "  %8s  %-30s %-18s %s\n", humanBytes(e.Bytes), e.Host+"/"+e.Name, class, shortWorktree(e.Worktree))
	}
	return 0
}

// humanBytes renders a byte count the way a person reads a disk figure.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGT"[exp])
}

// shortWorktree trims a worktree-sha256-<64 hex> directory name to something
// a human can tell apart at a glance without it dominating the line.
func shortWorktree(name string) string {
	const prefix = "worktree-sha256-"
	if len(name) > len(prefix)+12 {
		return name[:len(prefix)+12] + "…"
	}
	return name
}
