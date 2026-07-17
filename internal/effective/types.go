package effective

import (
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/ontology"
)

// Candidate is one physical representation of a concept entity, extracted
// from a domain.Observation (extract.go). Most concepts are one Candidate
// per Observation (instruction, skill); mcp_server is the exception — one
// Observation over a whole registration file (e.g. .claude.json,
// config.toml) can carry several server definitions, so extraction produces
// one Candidate per server entry found inside it.
type Candidate struct {
	Concept string

	// LogicalID is the concept-specific identity key the Identity Matcher
	// groups on (docs/ontology/README.md §3.2's x-logicalIdentity per
	// concept): the MCP server ID for mcp_server, the skill name for
	// skill, and "scope.root|source.path" for instruction (each distinct
	// file is its own logical instruction — see doc.go and compose.go).
	LogicalID string

	// Ref is this Candidate's addressable source reference, matching
	// fixtures/*/*/*/expected-effective.json's SourceRef.Path convention:
	// the file path, plus a "#<key>.<id>" fragment when several Candidates
	// were extracted from one Observation (e.g.
	// "claude-config/.claude.json#mcpServers.shared-tools").
	Ref string

	Scope         domain.ObservationScope
	Source        domain.ObservationSource
	Disposition   domain.SourceDisposition
	EvidenceLevel domain.EvidenceLevel

	// Fields is this Candidate's structured content, when the source was
	// parseable into one (an MCP server's JSON definition object). It is
	// nil for content this package only has as opaque text (e.g. an
	// instruction file's raw markdown) — DEEP_MERGE and content-equality
	// checks that need structure fall back to ContentDigest-only equality
	// for those.
	Fields map[string]any

	// ContentDigest is the canonical digest of Fields (if set) or of the
	// Candidate's raw text/identity otherwise: two Candidates with equal
	// ContentDigest are the same value observed twice, not a real
	// collision (see merge.go's distinct-content-digest check).
	ContentDigest string

	// Tools is the set of tool names this Candidate exposes, when known
	// (an mcp_server Candidate's "tools" field, docs/ontology/README.md's
	// mcp_server schema) — feeds duplicate.go's fingerprint detection.
	Tools []string
}

// LogicalGroup is every Candidate the Identity Matcher decided represents
// one logical entity for one concept.
type LogicalGroup struct {
	Concept    string
	LogicalID  string
	Candidates []Candidate
}

// AmbiguousIdentity is a pair of Candidates the Identity Matcher found
// suspicious enough to flag — plausibly the same physical entity registered
// twice under different logical IDs, or plausibly two independent entities
// that merely share incidental content — without enough signal to decide
// either way. This mirrors internal/resolve's Conflict: a normal, reportable
// outcome that must stay visible rather than being silently merged or
// silently kept apart.
type AmbiguousIdentity struct {
	Concept string
	A, B    Candidate
	Reason  string
}

// Provenance is what produced one EffectiveEntry's outcome
// (docs/architecture/README.md §5.2: "the resolver program, selected
// source, ignored sources, constraints, and evidence").
type Provenance struct {
	// Program is the PrecedenceProgram.ID consulted, empty if none was
	// declared for this concept.
	Program string
	// Operator is the merge operator actually applied, empty when no
	// program/operator was usable (Conflict) or for a pure composition
	// entry that has no single "applied to a group" operator of its own.
	Operator ontology.MergeOperator
	// SelectedSource is the winning Candidate.Ref, empty when unresolved
	// or when every source is simultaneously active (composition, or a
	// NAMESPACE/keep-both resolution).
	SelectedSource string
	// ActiveSources are every Candidate.Ref this entry treats as
	// in-effect (a single-winner resolution's ActiveSources is just
	// [SelectedSource]).
	ActiveSources []string
	// IgnoredSources are Candidate.Refs this entry excluded (shadowed,
	// denied, or otherwise not selected).
	IgnoredSources []string
	// Constraints records non-operator reasons content was excluded or
	// constrained (e.g. "MANAGED_GUARDRAIL: managed-scope source
	// constrains result", "DENY_WINS: <ref> denied").
	Constraints []string
}

// EffectiveEntry is one resolved (or composed) logical entity in the
// Effective Graph: what this host is expected/confirmed to load for one
// concept's logical entity, with full provenance and an evidence level
// (docs/architecture/README.md §5.2).
type EffectiveEntry struct {
	Concept   string
	LogicalID string

	// Composed is true for a CONCAT_ORDERED composition entry spanning
	// every logical entity of the concept (compose.go) rather than one
	// LogicalGroup's resolution.
	Composed bool

	Provenance Provenance

	EvidenceLevel domain.EvidenceLevel
	Guarantee     domain.GuaranteeLevel

	// Confirmed mirrors qualify.ExpectedEffectiveEntry.Confirmed: true
	// only when the winning outcome is backed by EvidenceLevel E3+ (host-
	// reported, behavior-probed, or externally proven), never merely by
	// this package's own mechanical computation.
	Confirmed bool

	Reason string
}

// Conflict is one LogicalGroup (or composition) this package deliberately
// did not resolve to a single winner: no usable precedence program, an
// UNKNOWN/invalid operator, an unqualified resolve capability, or a genuine
// tie/ambiguity even with a qualified operator. A Conflict is a normal,
// reportable resolution outcome (mirrors internal/resolve.Conflict), never a
// program failure.
type Conflict struct {
	Concept       string
	LogicalID     string
	Candidates    []Candidate
	Program       string
	Operator      string
	EvidenceLevel domain.EvidenceLevel
	Reason        string
}
