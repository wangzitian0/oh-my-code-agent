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
	cap := domain.DefaultHostCapability(host)
	if cap.Tier == domain.TierBridge {
		// ADR 0006 decision 3: a tier-2 line states the residual load. The
		// earlier wording ("native credentials and skills retained for
		// Keychain/OAuth integrity") named the reason but not the cost, so
		// it read as a feature rather than as the FR-7 gap it is.
		return fmt.Sprintf(
			"%s: Tier 2 (Bridge-Managed); HOME is NOT virtualized, so excluded 0 native sources -- "+
				"the real user-global configuration (Skills, MCP registrations, settings) still loads in full. "+
				"Keychain-bound credentials are why (ADR 0006); governed via MCP Hub Bridge",
			host,
		)
	}
	excludedMCP, excludedSkills := mcp.CountUserExclusions(gen)
	cost := mcp.EstimateContextCost(excludedMCP, excludedSkills)
	return fmt.Sprintf(
		"%s: excluded %d native MCP configuration source(s), %d native Skill(s) versus native; estimated context-cost delta ~%d tokens (%s)",
		host, excludedMCP, excludedSkills, cost.EstimatedTokensExcluded, cost.Confidence,
	)
}
