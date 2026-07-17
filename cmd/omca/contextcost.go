package main

import (
	"fmt"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/mcp"
)

// contextCostSummaryLine renders one host's exclusion counts and estimated
// context-cost delta as a single human-readable line, reusing internal/mcp.
// CountUserExclusions/EstimateContextCost — the identical computation
// omca_status itself performs (internal/mcp/status.go's hostStatus) — so
// `omca env`'s own stderr diagnostics and `omca doctor`'s findings never
// drift from what a model querying omca_status over MCP would see for the
// same generation. This is issue #15's own instruction: "Surface this in
// both the omca_status MCP response and wherever omca doctor/omca env/a
// report-producing command already prints diagnostic output."
func contextCostSummaryLine(host string, gen domain.Generation) string {
	excludedMCP, excludedSkills := mcp.CountUserExclusions(gen)
	cost := mcp.EstimateContextCost(excludedMCP, excludedSkills)
	return fmt.Sprintf(
		"%s: excluded %d native MCP configuration source(s), %d native Skill(s) versus native; estimated context-cost delta ~%d tokens (%s)",
		host, excludedMCP, excludedSkills, cost.EstimatedTokensExcluded, cost.Confidence,
	)
}
