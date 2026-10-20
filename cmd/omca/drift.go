package main

import (
	stdcontext "context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/hub"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// runDrift implements `omca drift [--json]` (the root-cause-grouped
// ActionCard list), `omca drift show <drift-id> [--json]` (one card's full
// detail, including its complete Matrix — docs/architecture/reporting.md
// §14's "every action card expands to all affected cells"), and
// `omca drift restore <checkout-path>` (invoking `ws-render --checkout <path>`
// through the hub or directly to clear a render drift card, issue #125).
func runDrift(stdout, stderr io.Writer, args []string) int {
	var showID string
	if len(args) > 0 && args[0] == "restore" {
		if len(args) < 2 {
			fmt.Fprintln(stderr, "omca: drift restore: a checkout path is required: omca drift restore <checkout-path>")
			return 2
		}
		targetPath := args[1]
		if respBytes, err := sendHubAdminRequest("", "render_restore", targetPath); err == nil {
			var res struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			}
			if json.Unmarshal(respBytes, &res) == nil && res.Status == "ok" {
				fmt.Fprintf(stdout, "restored %s\n", targetPath)
				return 0
			} else if res.Message != "" {
				fmt.Fprintf(stderr, "omca: drift restore: %s\n", res.Message)
				return 1
			}
		}
		w := hub.NewRenderCheckWorker(nil)
		if err := w.Restore(stdcontext.Background(), targetPath); err != nil {
			fmt.Fprintf(stderr, "omca: drift restore: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "restored %s\n", targetPath)
		return 0
	}
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
