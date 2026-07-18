package report

import (
	"github.com/wangzitian0/oh-my-code-agent/internal/contextcost"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/drift"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
)

// Artifact is the one immutable report every projection in this package
// derives from (docs/architecture/reporting.md). Report nests the frozen
// protocol envelope (schemas/protocol/report.v1alpha1.schema.json) every
// already-shipped schema/protocol consumer expects, byte-for-byte —
// deliberately a named field, not an anonymously embedded/flattened one:
// that schema declares "additionalProperties": false at BOTH its top level
// and its spec level, so a consumer validating artifact.Report alone
// against it must see exactly apiVersion/kind/metadata/spec and nothing
// else. The M3-specific additions (ActionCards, Hosts,
// DuplicateCapabilities, Debug) live as siblings of Report at this
// Artifact's own top level instead, which — being a strict superset, not a
// modification of the frozen document — never needs the frozen schema
// itself to change.
type Artifact struct {
	Report domain.Report `json:"report"`

	// ActionCards is the root-cause-grouped drift view (docs/architecture/
	// reporting.md §7): every DriftCard the same content the underlying
	// domain.Report.Spec.Drift flat assertion list carries, but grouped by
	// (root cause, remediation, outcome class, adapter version) with a
	// stable, content-addressed ID `omca drift show <id>`/`omca matrix <id>`
	// resolve against.
	ActionCards []DriftCard `json:"actionCards,omitempty"`

	// Hosts is one entry per host this Artifact was built for, carrying the
	// per-host detail (Knowledge status, context-cost, graph summaries) that
	// domain.Report's own frozen shape has no field for.
	Hosts []HostSummary `json:"hosts,omitempty"`

	// DuplicateCapabilities is the cross-host, cross-transport duplicate
	// logical capability section the round-2 audit added to issue #23:
	// every internal/effective.DuplicateCapability this Artifact's hosts
	// found, each carrying a context-cost attribution (docs/architecture/
	// reporting.md §8).
	DuplicateCapabilities []DuplicateCapabilityEntry `json:"duplicateCapabilities,omitempty"`

	// Debug carries the raw, per-host Effective Graph, physical Candidate
	// inventory, Desired Graph, and Knowledge evidence — docs/architecture/
	// reporting.md §9's Debug section ("Effective State", "Precedence
	// Trace", "Evidence", "Native Artifacts") and §14's debug invariant
	// "every resolver trace expands to physical sources and Knowledge
	// evidence." [Explain] and [ComparePlanes] both project this rather
	// than recomputing anything: it is part of the one immutable Artifact,
	// not a second, separately-computed view.
	Debug map[string]HostDebug `json:"debug,omitempty"`
}

// HostDebug is one host's raw graph/evidence data, keyed by host ID in
// Artifact.Debug.
type HostDebug struct {
	Graph             effective.EffectiveGraph      `json:"graph"`
	Candidates        []effective.Candidate         `json:"candidates,omitempty"`
	Observations      []domain.Observation          `json:"observations,omitempty"`
	Desired           resolve.ResolvedState         `json:"desired"`
	KnowledgeEvidence []domain.KnowledgeEvidenceRef `json:"knowledgeEvidence,omitempty"`

	// CurrentSources/PendingSources are the host's current/pending
	// Generation.Spec.Sources lists (empty when no such generation exists
	// yet for this host in this worktree) — the CURRENT/PENDING plane data
	// [ComparePlanes] projects (docs/architecture/reporting.md §2's CURRENT/
	// PENDING planes).
	CurrentSources []domain.GenerationSourceEntry `json:"currentSources,omitempty"`
	PendingSources []domain.GenerationSourceEntry `json:"pendingSources,omitempty"`

	// CurrentGenerationID/PendingGenerationID are the current/pending
	// generation's own Metadata.ID (empty exactly when the corresponding
	// Sources list is empty — no such generation exists yet for this host).
	// build.go's generationSources already reads each generation's full
	// manifest.json to compute CurrentSources/PendingSources and the
	// context-cost estimate; these two fields just keep the ID it already
	// read in hand rather than discarding it, so a caller (issue #24's
	// omca_query "generation" query kind) can name which generation a
	// Sources list came from without a second manifest read.
	CurrentGenerationID string `json:"currentGenerationId,omitempty"`
	PendingGenerationID string `json:"pendingGenerationId,omitempty"`
}

// DriftCard is one ActionCard plus the stable, content-addressed ID
// `omca drift show <id>` and `omca matrix <id>` resolve against
// (drift.ActionCard itself carries no ID — see cardid.go). Anonymous
// embedding flattens ActionCard's own JSON fields (rootCause, remediation,
// category, ...) into DriftCard's own JSON object alongside "id", so a
// DriftCard's wire shape is exactly a drift.ActionCard with one extra field,
// not a nested sub-object.
type DriftCard struct {
	ID string `json:"id"`
	drift.ActionCard
}

// HostSummary is one host's contribution to an Artifact: its Knowledge
// lifecycle status, context-cost estimate, and Observed/Effective/Desired
// graph sizes, for the report's Overview and Debug sections
// (docs/architecture/reporting.md §9).
type HostSummary struct {
	Host        string `json:"host"`
	HostVersion string `json:"hostVersion,omitempty"`

	Knowledge HostKnowledge `json:"knowledge"`

	// ContextCost is nil when this host has no readable "current" generation
	// to estimate a delta for (docs/architecture/reporting.md §8: "Unknown
	// prompt assembly is reported as unknown rather than converted into a
	// false token count") — never a synthesized zero.
	ContextCost *ContextCostEntry `json:"contextCost,omitempty"`

	Planes HostPlaneCounts `json:"planes"`
}

// HostKnowledge is one host's Knowledge Pack resolution outcome — the
// round-2 audit's "per-host Knowledge status (FRESH/DUE/STALE/...)"
// requirement. Status/PackID are populated only when Qualified; an
// unqualified host still reports Reason honestly rather than a guessed
// status (internal/knowledge.Resolution.Status's own "degrade honestly"
// contract).
type HostKnowledge struct {
	Qualified bool                   `json:"qualified"`
	PackID    string                 `json:"packId,omitempty"`
	Status    domain.KnowledgeStatus `json:"status,omitempty"`
	Reason    string                 `json:"reason,omitempty"`
}

// ContextCostEntry is contextcost.ContextCostEstimate plus HostVersion —
// issue #23's literal AC text: "Context-cost entries carry method,
// hostVersion, and confidence." internal/contextcost's existing
// ContextCostEstimate (reused here, not reinvented, per this issue's own
// instruction) already carries Method and Confidence; HostVersion is added
// at this projection layer via embedding rather than by changing
// internal/contextcost's already-shipped, already-tested type, so every
// existing caller of contextcost.ContextCostEstimate (omca_status, omca
// doctor/env) is unaffected.
//
// This type used to embed internal/mcp.ContextCostEstimate directly (this
// package's own original PR-19 implementation); PR-20 (issue #24) moved the
// underlying type to internal/contextcost (see that package's doc comment)
// specifically so this package could stop importing internal/mcp — a
// prerequisite for internal/mcp's own omca_query implementation to import
// this package in turn, without an import cycle. internal/mcp still exports
// ContextCostEstimate as a type alias of contextcost.ContextCostEstimate,
// so this is a source-compatible rename, not a shape change.
type ContextCostEntry struct {
	contextcost.ContextCostEstimate
	HostVersion string `json:"hostVersion,omitempty"`
}

// HostPlaneCounts are one host's Observed/Effective/Desired/Current/Pending
// entry counts — the per-host half of domain.ReportPlanes (which this
// Artifact's Report.Spec.Planes carries pre-summed across every host).
type HostPlaneCounts struct {
	Observed  int `json:"observed"`
	Effective int `json:"effective"`
	Conflicts int `json:"conflicts,omitempty"`
	Desired   int `json:"desired,omitempty"`
	Current   int `json:"current,omitempty"`
	Pending   int `json:"pending,omitempty"`
}

// ContextCostAttribution is the "context-cost attribution" the round-2 audit
// requires alongside the duplicate-capability section: an honest, clearly
// labeled estimate of the extra context spent loading the same logical
// capability through more than one transport, using the exact same
// "method + confidence, never a bare number" discipline internal/mcp's own
// ContextCostEstimate established (docs/architecture/reporting.md §8).
type ContextCostAttribution struct {
	// RedundantSources is len(Sources)-1: every source beyond the first is
	// context spent on a capability the model already has access to.
	RedundantSources int    `json:"redundantSources"`
	EstimatedTokens  int    `json:"estimatedTokens"`
	Method           string `json:"method"`
	Confidence       string `json:"confidence"`
}

// DuplicateCapabilityEntry is one effective.DuplicateCapability plus its
// context-cost attribution.
type DuplicateCapabilityEntry struct {
	Fingerprint string                 `json:"fingerprint"`
	Sources     []effective.ToolSource `json:"sources"`
	ContextCost ContextCostAttribution `json:"contextCost"`
}

// PhysicalSource is one physical Candidate an EffectiveEntry's resolver
// trace names, expanded with the identity/evidence detail
// docs/architecture/reporting.md §14's debug invariant requires ("every
// resolver trace expands to physical sources and Knowledge evidence").
type PhysicalSource struct {
	Ref           string                   `json:"ref"`
	Path          string                   `json:"path,omitempty"`
	Kind          string                   `json:"kind,omitempty"`
	Disposition   domain.SourceDisposition `json:"disposition,omitempty"`
	EvidenceLevel domain.EvidenceLevel     `json:"evidenceLevel,omitempty"`
	ContentDigest string                   `json:"contentDigest,omitempty"`
}

// ExplainTrace is the `--trace` expansion docs/architecture/reporting.md
// requires: "effective value -> resolver trace -> physical sources ->
// Knowledge evidence." Only populated when `--trace` is requested — see
// ExplainResult.Trace's own doc comment for why this is honest rather than
// a hidden/truncated field.
type ExplainTrace struct {
	ResolverTrace     effective.Provenance          `json:"resolverTrace"`
	PhysicalSources   []PhysicalSource              `json:"physicalSources"`
	KnowledgeEvidence []domain.KnowledgeEvidenceRef `json:"knowledgeEvidence,omitempty"`
}

// ExplainResult is `omca explain <concept> <logical-id> [--trace]`'s answer:
// the resolved (or conflicted, or absent) effective value for one logical
// entity on one host, plus its evidence/guarantee, plus — only when
// requested — the full expansion chain.
type ExplainResult struct {
	Host      string `json:"host"`
	Concept   string `json:"concept"`
	LogicalID string `json:"logicalId"`

	// Found is false when no EffectiveEntry and no Conflict exists for
	// (Concept, LogicalID) on Host — an honest "nothing known about this
	// identity," not a zero-valued Found=true result.
	Found bool `json:"found"`

	// Conflict is true when this logical entity's sources were left
	// unresolved (internal/effective.Conflict) rather than one confirmed
	// EffectiveEntry.
	Conflict bool `json:"conflict"`

	EvidenceLevel domain.EvidenceLevel  `json:"evidenceLevel,omitempty"`
	Guarantee     domain.GuaranteeLevel `json:"guarantee,omitempty"`
	Confirmed     bool                  `json:"confirmed,omitempty"`
	Reason        string                `json:"reason,omitempty"`

	// Trace is nil unless `--trace` was requested (docs/architecture/
	// reporting.md §10's "[--trace]" flag) — the JSON contract stays stable
	// either way (a present-but-empty Trace would misrepresent "no
	// resolver trace was computed" as "the resolver trace is genuinely
	// empty"), and a caller that never asks for it never pays for building
	// it.
	Trace *ExplainTrace `json:"trace,omitempty"`
}

// Plane names one of docs/architecture/reporting.md §2's six reported state
// planes, restricted to the subset this package's compare/diff projection
// can answer from data an Artifact actually computes. NATIVE and OBSERVED
// currently project identically (both read internal/effective.ObservedGraph
// — see ComparePlanes' doc comment): this package has no separate raw/
// pre-parse representation of "native" reality yet, an honest, documented
// scope note rather than a silent conflation.
type Plane string

const (
	PlaneNative    Plane = "NATIVE"
	PlaneObserved  Plane = "OBSERVED"
	PlaneDesired   Plane = "DESIRED"
	PlaneEffective Plane = "HOST_EFFECTIVE"
	PlaneCurrent   Plane = "CURRENT"
	PlanePending   Plane = "PENDING"
)

// PlaneRow is one (concept, id) entity's state within a single Plane.
type PlaneRow struct {
	Concept string `json:"concept"`
	ID      string `json:"id"`
	Present bool   `json:"present"`
	Active  bool   `json:"active"`
	Detail  string `json:"detail,omitempty"`
}

// CompareResult is `omca compare`/`omca diff`'s answer: every (concept, id)
// entity known to either plane, with its row (or absence) in each, and
// whether the two planes agree.
type CompareResult struct {
	Host   string `json:"host"`
	PlaneA Plane  `json:"planeA"`
	PlaneB Plane  `json:"planeB"`

	Rows []CompareRow `json:"rows"`
}

// CompareRow is one entity's side-by-side comparison across CompareResult's
// two planes.
type CompareRow struct {
	Concept string    `json:"concept"`
	ID      string    `json:"id"`
	A       *PlaneRow `json:"a,omitempty"`
	B       *PlaneRow `json:"b,omitempty"`
	Differs bool      `json:"differs"`
}
