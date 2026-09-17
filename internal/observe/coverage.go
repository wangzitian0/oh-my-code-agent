package observe

import (
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// ConceptCoverage is this package's own honest self-report of what it can
// observe for one host x concept pair, expressed per-dimension
// (docs/architecture/reporting.md §7's "root-cause aggregation" and issue
// #20's own acceptance criterion: "Coverage is reported per dimension
// (discovery/parse/normalize/resolve), never as one blended percentage").
// It reuses domain.CapabilityOps — the exact same discover/parse/normalize/
// resolve/compile/verify vocabulary docs/knowledge/README.md §5's Capability
// Vocabulary and a Knowledge Pack's own capabilities map already use — rather
// than inventing a parallel one, so a caller comparing this package's actual
// coverage against a Knowledge Pack's claimed capability for the same
// concept is comparing like with like.
type ConceptCoverage struct {
	Host    string               `json:"host"`
	Concept string               `json:"concept"`
	Ops     domain.CapabilityOps `json:"ops"`
}

// coverageEntries is Coverage's static backing table. It is hand-maintained,
// not computed from rules.go's sourceRule tables at runtime: the two are
// expected to be reviewed together (a new sourceRule for an existing
// concept/host pair should prompt reconsidering whether that cell's
// Discover level is still accurate), the same relationship
// knowledge/hosts/*/manifest.json's hand-maintained capabilities map has to
// the adapter code it describes — this table is this package's own
// analogous self-declaration, not a knowledge pack (it never claims Compile/
// Verify, which are later-phase concerns entirely outside this package).
//
// Normalize and Resolve are UNSUPPORTED for every single cell below, on
// purpose: this package proves nothing about precedence, duplicate
// detection, or host-reported/behavior-probed state (doc.go's safety
// properties point 5 — every Observation this package emits is E0 or E1,
// never higher), so claiming any Normalize/Resolve capability here would
// contradict that ceiling. This is not an omission to fix later within this
// package's stated scope; a resolver is a different, later-milestone
// component (docs/ontology/README.md §3.2's "Native" pipeline, "normalize
// entities and scopes -> apply host-specific merge operator").
var coverageEntries = []ConceptCoverage{
	// codex
	{"codex", conceptInstruction, domain.CapabilityOps{Discover: domain.CapabilityExact, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"codex", conceptSkill, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"codex", conceptMCPServer, domain.CapabilityOps{Discover: domain.CapabilityExact, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"codex", conceptHook, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"codex", conceptPolicy, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"codex", conceptPlugin, domain.CapabilityOps{Discover: domain.CapabilityUnknown, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},

	// claude-code
	{"claude-code", conceptInstruction, domain.CapabilityOps{Discover: domain.CapabilityExact, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"claude-code", conceptSkill, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"claude-code", conceptMCPServer, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityExact, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"claude-code", conceptHook, domain.CapabilityOps{Discover: domain.CapabilityExact, Parse: domain.CapabilityExact, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"claude-code", conceptPolicy, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityExact, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"claude-code", conceptPlugin, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityExact, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},

	// pi — first-party observation tier only (no runtime activation).
	// instruction: every documented candidate file (AGENTS.md/AGENTS.override.md/
	// CLAUDE.md/SYSTEM.md/APPEND_SYSTEM.md) is walked, so discovery is EXACT;
	// precedence between same-directory candidates is a later resolver's job.
	// skill: PARTIAL — the SKILL.md-marker walk misses root-level .md skills
	// that ~/.pi/agent/skills and .pi/skills natively discover.
	// mcp_server/hook: UNSUPPORTED in every dimension this package could
	// claim — Pi has no declarative native registry for either; both are
	// implemented through extensions, which are executable code this
	// package deliberately does not translate (ontology §6.7's "PARTIAL;
	// do not translate a native MCP or agent definition into arbitrary
	// extension code automatically").
	// policy: PARTIAL — settings.json and trust.json are walked, but package
	// and session-level policy surfaces are not.
	// plugin: PARTIAL — the extensions/ walk is deliberately over-inclusive
	// (every regular file, not just loadable entrypoints) and npm/ package
	// roots are keyed on package.json presence, not on a pi.skills/
	// pi.extensions declaration.
	{"pi", conceptInstruction, domain.CapabilityOps{Discover: domain.CapabilityExact, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"pi", conceptSkill, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"pi", conceptMCPServer, domain.CapabilityOps{Discover: domain.CapabilityUnsupported, Parse: domain.CapabilityUnsupported, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"pi", conceptHook, domain.CapabilityOps{Discover: domain.CapabilityUnsupported, Parse: domain.CapabilityUnsupported, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"pi", conceptPolicy, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
	{"pi", conceptPlugin, domain.CapabilityOps{Discover: domain.CapabilityPartial, Parse: domain.CapabilityOpaque, Normalize: domain.CapabilityUnsupported, Resolve: domain.CapabilityUnsupported}},
}

// Coverage returns this package's per-host, per-concept, per-dimension
// capability self-report: every (host, concept) pair this package's own
// supportedHosts x knownConcepts cross product names, each with an explicit
// domain.CapabilityOps rather than a blended score — issue #20's "Concept
// coverage is explicit and complete for both hosts" acceptance criterion,
// satisfied structurally (coverage_test.go asserts the full 2-host x
// 6-concept cross product is present) rather than left to whichever cells
// happen to have a sourceRule today. The result is sorted by (Host,
// Concept) for the same determinism reason every other exported function in
// this package sorts its output.
func Coverage() []ConceptCoverage {
	out := make([]ConceptCoverage, len(coverageEntries))
	copy(out, coverageEntries)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Concept < out[j].Concept
	})
	return out
}
