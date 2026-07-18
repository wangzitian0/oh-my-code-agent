package assurance

import "github.com/wangzitian0/oh-my-code-agent/internal/domain"

// HostConceptClaim is the pseudo-concept this package uses for a claim about
// the host binary itself (identity/installed version) rather than about one
// of the three ontology concepts (docs/ontology/README.md §1.1) a Knowledge
// Pack declares capabilities for. It is not a concept ontology/concepts/*.json
// registers or a Knowledge Pack capabilities key -- it exists only so
// [Ceilings] can name the one E3 row this repository can actually back today
// (docs/architecture/evidence-ceiling.md §3's two "host" rows) alongside the
// concept rows, in the same table, without inventing a fourth ontology
// concept for a claim ("which version is this binary") no adapter resolves.
const HostConceptClaim = "host"

// CeilingEntry is one row of the per-host, per-concept evidence-ceiling
// table (docs/architecture/evidence-ceiling.md, added by issue #26's round-2
// audit): the strongest domain.EvidenceLevel any OMCA-reported conclusion
// about (Host, Concept) may honestly carry today, grounded in a specific,
// already-committed finding elsewhere in this repository.
type CeilingEntry struct {
	Host    string
	Concept string

	// Ceiling is the reachable-today cap: max(what a qualified resolve
	// capability can prove, what a documented native introspection surface
	// can prove). [VerifyGraph] never lets a claim about (Host, Concept)
	// exceed this, regardless of what internal/effective computed.
	Ceiling domain.EvidenceLevel

	// IntrospectionSurface names the safe, non-interactive, no-network,
	// no-model-call native command or interface this repo has documented
	// for this cell (docs/architecture/reporting.md §4's E3 definition), or
	// the literal "none documented" when fixtures/README.md already
	// establishes none exists.
	IntrospectionSurface string

	// ResolveCapability mirrors the concept's committed Knowledge Pack
	// capabilities.<concept>.resolve value (docs/knowledge/README.md §5) --
	// the E2 gate internal/effective/merge.go's capabilityQualified checks.
	// Empty for the HostConceptClaim rows, which are not a Knowledge Pack
	// concept at all.
	ResolveCapability domain.CapabilityLevel

	// Reason is the specific, citable finding this Ceiling is grounded in
	// (docs/architecture/evidence-ceiling.md §3's "Why" column, reproduced
	// here so a caller inspecting this table in code sees the same
	// justification a human reviewer sees in the doc).
	Reason string

	// Citation names the committed source(s) Reason draws on, so a future
	// change to those sources is discoverable as "this ceiling row may need
	// re-review" rather than silently going stale.
	Citation string
}

// Ceilings is the committed per-host, per-concept evidence-ceiling table:
// the machine-readable mirror of docs/architecture/evidence-ceiling.md §3.
// TestCeilings_MatchThisDoc (ceiling_test.go) fails if this table and that
// document ever name a different Ceiling for the same (Host, Concept) cell
// -- edit both together. Every row here is grounded in a finding already
// committed elsewhere (fixtures/README.md, the two knowledge/hosts/*/*/*/
// manifest.json files), never in what an introspection surface might
// plausibly offer: docs/architecture/evidence-ceiling.md §1.
var Ceilings = []CeilingEntry{
	{
		Host: "codex", Concept: "instruction",
		Ceiling:              domain.EvidenceLevelParsed,
		IntrospectionSurface: "none documented",
		ResolveCapability:    domain.CapabilityUnknown,
		Reason:               "knowledge/hosts/codex/cli/0.144/manifest.json declares capabilities.instruction.resolve: UNKNOWN, so the E2 gate never opens; fixtures/README.md's full codex --help review found no safe, non-interactive, no-network, no-model-call flag that dumps effective/merged configuration. The documented claim \"closer instructions appear later\" (docs/ontology/README.md §6.2) stays a documentedClaim at E1, never promoted to selectedSource.",
		Citation:             "knowledge/hosts/codex/cli/0.144/manifest.json; fixtures/README.md",
	},
	{
		Host: "codex", Concept: "skill",
		Ceiling:              domain.EvidenceLevelParsed,
		IntrospectionSurface: "none documented",
		ResolveCapability:    domain.CapabilityUnknown,
		Reason:               "knowledge/hosts/codex/cli/0.144/manifest.json declares capabilities.skill.resolve: UNKNOWN. Its knownUnknowns[0] records that $CODEX_HOME/skills as a fourth discovery root was found only by read-only strings inspection (E1), never behaviorally confirmed -- that finding adds a known unknown, it does not raise this cell.",
		Citation:             "knowledge/hosts/codex/cli/0.144/manifest.json; fixtures/README.md",
	},
	{
		Host: "codex", Concept: "mcp_server",
		Ceiling:              domain.EvidenceLevelParsed,
		IntrospectionSurface: "none documented",
		ResolveCapability:    domain.CapabilityUnknown,
		Reason:               "knowledge/hosts/codex/cli/0.144/manifest.json declares capabilities.mcp_server.resolve: UNKNOWN, and fixtures/README.md's safety-boundary review found no safe flag that dumps merged MCP registration state.",
		Citation:             "knowledge/hosts/codex/cli/0.144/manifest.json; fixtures/README.md",
	},
	{
		Host: "claude-code", Concept: "instruction",
		Ceiling:              domain.EvidenceLevelParsed,
		IntrospectionSurface: "none documented",
		ResolveCapability:    domain.CapabilityUnknown,
		Reason:               "knowledge/hosts/claude-code/cli/2.1/manifest.json declares capabilities.instruction.resolve: UNKNOWN; fixtures/README.md's full claude --help review found no safe merged-configuration-dump flag. The documented claim \"enterprise > personal > project > bundled\" stays a documentedClaim at E1.",
		Citation:             "knowledge/hosts/claude-code/cli/2.1/manifest.json; fixtures/README.md",
	},
	{
		Host: "claude-code", Concept: "skill",
		Ceiling:              domain.EvidenceLevelParsed,
		IntrospectionSurface: "none documented",
		ResolveCapability:    domain.CapabilityUnknown,
		Reason:               "knowledge/hosts/claude-code/cli/2.1/manifest.json declares capabilities.skill.resolve: UNKNOWN. Cross-reference issue #47 (open): whether CLAUDE_CONFIG_DIR fully relocates the Skill discovery root is E1 static-inspection evidence only (fixtures/README.md, read-only strings extraction, never a live launch) -- this cell's E1 ceiling is exactly what issue #47 needs to close before it can rise.",
		Citation:             "knowledge/hosts/claude-code/cli/2.1/manifest.json; fixtures/README.md; issue #47",
	},
	{
		Host: "claude-code", Concept: "mcp_server",
		Ceiling:              domain.EvidenceLevelParsed,
		IntrospectionSurface: "none documented",
		ResolveCapability:    domain.CapabilityUnknown,
		Reason:               "knowledge/hosts/claude-code/cli/2.1/manifest.json declares capabilities.mcp_server.resolve: UNKNOWN. Cross-reference issue #47 (open): CLAUDE_CONFIG_DIR's relocation of the ~/.claude.json-equivalent MCP/trust state file is the same E1 static-inspection finding, not behaviorally confirmed.",
		Citation:             "knowledge/hosts/claude-code/cli/2.1/manifest.json; fixtures/README.md; issue #47",
	},
	{
		Host: "codex", Concept: HostConceptClaim,
		Ceiling:              domain.EvidenceLevelHostReported,
		IntrospectionSurface: "codex --version",
		Reason:               "codex --version is safe (non-interactive, no network, no model call -- verified against codex --help, fixtures/README.md's \"Safety boundary\" section), is the only invocation internal/context/host.go's probeVersion ever makes, and its output is a native, host-reported answer to \"which version is installed\": docs/architecture/reporting.md §4's E3 definition exactly. This is a claim about the host binary itself, not about instruction/skill/mcp_server, so it does not raise those rows.",
		Citation:             "fixtures/README.md; internal/context/host.go",
	},
	{
		Host: "claude-code", Concept: HostConceptClaim,
		Ceiling:              domain.EvidenceLevelHostReported,
		IntrospectionSurface: "claude --version",
		Reason:               "claude --version is safe (non-interactive, no network, no model call) and is the only invocation internal/context/host.go's probeVersion ever makes for Claude Code; its output is a native, host-reported version string, matching the codex / host row's reasoning.",
		Citation:             "fixtures/README.md; internal/context/host.go",
	},
}

// CeilingFor returns the committed Ceiling for (host, concept) among
// ceilings, and whether one is declared. No declared row means this table
// has not evaluated that cell -- callers (see [VerifyGraph]) must treat a
// missing row as "undocumented, so E1" rather than leaving evidence
// unclamped: docs/architecture/evidence-ceiling.md §1's "if a host+concept
// combination has no real native introspection interface documented
// anywhere in this repo, its ceiling is E1, full stop."
func CeilingFor(ceilings []CeilingEntry, host, concept string) (domain.EvidenceLevel, bool) {
	for _, c := range ceilings {
		if c.Host == host && c.Concept == concept {
			return c.Ceiling, true
		}
	}
	return "", false
}
