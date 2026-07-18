// Package contextcost computes the estimated context-cost delta a native
// user-global MCP server/Skill exclusion (or a duplicate-capability
// redundancy) represents, plus the fixed, honestly-labeled "estimate, not
// measured" confidence caveat every such number carries
// (docs/architecture/reporting.md §8: "Every estimate includes method,
// hostVersion, and confidence").
//
// # Why this is its own package
//
// This logic originally lived in internal/mcp (issue #15's omca_status
// stub, PR-11): ComputeStatus needed it, and no other package did yet.
// internal/report/PR-19 then needed the identical computation for its own
// context-cost projections and imported internal/mcp to reuse it rather
// than duplicating it — exactly the right call at the time (this package's
// own reuse-not-reinvent instruction predates it).
//
// PR-20 (issue #24) breaks that arrangement: omca_query's implementation
// belongs in internal/mcp (matching status.go's own precedent — see
// internal/mcp/doc.go's package-shape section) and needs to read
// internal/report.Artifact values, which would make internal/mcp import
// internal/report — while internal/report already imports internal/mcp for
// this exact context-cost logic. That is an import cycle. This package
// breaks it: the estimate is genuinely shared, low-level computation with
// no dependency on either the MCP protocol or the report/Artifact shape, so
// it belongs below both, not inside either. internal/mcp and internal/report
// both depend on internal/contextcost; neither depends on the other.
package contextcost
