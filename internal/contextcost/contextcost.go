package contextcost

import (
	"fmt"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// EstimatedTokensPerExcludedMCPServer / EstimatedTokensPerExcludedSkill are
// fixed, documented, rough per-item token-cost averages this package
// multiplies an exclusion count by to produce ContextCostEstimate
// (issue #15's own suggested method: "N excluded MCP server tool-schema
// definitions x an estimated average token cost per schema, M excluded
// Skill descriptions x an estimated average token cost"). These are NOT
// measured from any real schema or description text — see
// ConfidenceEstimateNotMeasured's doc comment for why this stays an
// explicit estimate rather than borrowing domain.EvidenceLevel's E0-E5
// vocabulary, and docs/evidence/perf-v0.1.0.md for how these two constants
// were chosen (a small manual sample of real MCP tool-schema JSON and
// Skill-description frontmatter, not a rigorous corpus study).
//
// Exported (unlike the rest of this package's original internal/mcp home,
// where they were unexported constants only internal/mcp's own tests
// referenced): internal/mcp/status_test.go still exercises the exact
// arithmetic these feed, now from across the package boundary.
const (
	EstimatedTokensPerExcludedMCPServer = 200
	EstimatedTokensPerExcludedSkill     = 150
)

// ConfidenceEstimateNotMeasured is the fixed confidence label every
// ContextCostEstimate carries. domain.EvidenceLevel (E0-E5) is this
// project's vocabulary for how strongly a claim about NATIVE HOST BEHAVIOR
// was established (docs/architecture/reporting.md §4) — a category
// mismatch for "how many tokens does an average excluded tool schema cost,"
// which is a modeling assumption about token economics, not a claim this
// package could ever raise to E2 (RESOLVED) by observing the host harder.
// Issue #15's own AC text explicitly allows either vocabulary ("domain.
// EvidenceLevel E0-E5, or a simpler explicit 'estimate, not measured'
// caveat — your call, but it must be honest about being an estimate"); this
// package picks the plain-language caveat as the documented, defensible
// choice, reserved as a named constant (not a literal repeated at every call
// site) so a future PR that replaces the fixed per-item averages with a
// real measurement (actually tokenizing excluded schemas/descriptions) has
// exactly one place to change the label.
const ConfidenceEstimateNotMeasured = "estimate, not measured -- no real MCP tool-schema or Skill description text was tokenized; this multiplies exclusion counts by fixed, documented per-item token averages (see docs/evidence/perf-v0.1.0.md)"

// ContextCostEstimate is the omca_status tool's (and internal/report's own
// context-cost projections') answer to issue #15's "estimated context-cost
// delta with method + confidence" acceptance criterion, attached to one
// host's exclusion counts.
type ContextCostEstimate struct {
	// EstimatedTokensExcluded is excludedMCPServers*EstimatedTokensPer
	// ExcludedMCPServer + excludedSkills*EstimatedTokensPerExcludedSkill:
	// the rough token count a native, unmanaged launch would have spent on
	// tool-schema/description text this managed session never loads.
	EstimatedTokensExcluded int `json:"estimatedTokensExcluded"`
	// Method is a human-readable description of exactly how
	// EstimatedTokensExcluded was computed, so a report reader never has to
	// take the number on faith.
	Method string `json:"method"`
	// Confidence is always ConfidenceEstimateNotMeasured today.
	Confidence string `json:"confidence"`
}

// CountUserExclusions counts gen.Spec.Sources entries the M1 bootstrap
// policy excluded at user scope, split by concept -- issue #15's literal
// "N MCP servers and M Skills excluded versus native," computed directly
// from internal/runtime/compile.go's own recorded decisions (never
// re-derived by walking the real native homes again: Sources already IS
// the authoritative record of what this generation's compiler saw and
// decided, exactly like cmd/omca/doctor.go's checkStaleGeneration re-derives
// only when it needs to detect drift, not for a plain status read). Exported
// so internal/perf's real-environment measurement (M1's "record native vs
// managed... context cost before/after" round-2 AC line) can reuse the
// identical count rather than a second, driftable copy.
//
// Only Scope == "user" entries count, and a CapabilityGap entry (internal/
// runtime/compile.go's claudeConfigDirExclusionGapSources) is explicitly
// excluded even though it also carries Scope == "user" (Copilot review
// finding on the original PR-11 version of this function: an earlier
// version of this doc comment claimed a capability-gap entry "carries no
// Scope at all," which was never true of the actual
// claudeConfigDirExclusionGapSources implementation -- the filter below is
// what actually enforces the distinction the comment only asserted). A
// capability-gap entry describes an unproven exclusion *class* ("we don't
// yet behaviorally know whether every native user-global MCP server was
// really excluded"), not one discovered physical source this generation
// counted and excluded; counting it the same as a real, observed-and-
// excluded source would silently inflate N/M by one per gap class and
// overstate confidence in a number that is, for that class, genuinely
// unknown rather than confirmed.
func CountUserExclusions(gen domain.Generation) (mcpServers, skills int) {
	for _, s := range gen.Spec.Sources {
		if s.Included || s.Scope != "user" || s.CapabilityGap {
			continue
		}
		switch s.Concept {
		case "mcp_server":
			mcpServers++
		case "skill":
			skills++
		}
	}
	return mcpServers, skills
}

// EstimateContextCost is CountUserExclusions's companion: the estimated
// context-cost delta issue #15's AC requires alongside the exclusion
// counts, exported so internal/perf's real-environment measurement (M1's
// "record native vs managed startup time and context cost before/after"
// round-2 AC line) can reuse the identical method rather than a second,
// driftable copy.
func EstimateContextCost(excludedMCPServers, excludedSkills int) ContextCostEstimate {
	tokens := excludedMCPServers*EstimatedTokensPerExcludedMCPServer + excludedSkills*EstimatedTokensPerExcludedSkill
	return ContextCostEstimate{
		EstimatedTokensExcluded: tokens,
		Method: fmt.Sprintf(
			"%d excluded native MCP configuration source(s) x ~%d tokens/source (each source may register multiple individual servers; see HostStatus.ExcludedMCPServers' doc comment) + %d excluded Skill(s) x ~%d tokens/description (fixed, documented per-item averages, not measured from this session's actual excluded schemas/descriptions)",
			excludedMCPServers, EstimatedTokensPerExcludedMCPServer, excludedSkills, EstimatedTokensPerExcludedSkill,
		),
		Confidence: ConfidenceEstimateNotMeasured,
	}
}
