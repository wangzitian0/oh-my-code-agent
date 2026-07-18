package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/drift"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// ArtifactFunc computes the current report.Artifact on demand. queryToolHandler
// calls it fresh for every omca_query "tools/call" request — the identical
// "never answer from a value computed once at startup" discipline
// StatusFunc's own doc comment establishes for omca_status, because the
// report an omca_query call answers from can change during this server's
// own lifetime exactly like the generation omca_status reports on can: a
// restart-activated new generation, a freshly staged pending generation, a
// new observation picked up from disk. cmd/omca/mcp.go's runMCP wires this
// to the same detect-observe-compose-Build pipeline cmd/omca/reportbuild.go's
// buildArtifactForCLI already runs fresh for every `omca report`/`omca
// drift`/... CLI invocation — omca_query is a thin MCP-shaped wrapper over
// that same, already-built pipeline, not a second implementation of it.
type ArtifactFunc func() (report.Artifact, error)

// QueryKind selects the shape of thing omca_query retrieves — the literal
// five nouns issue #24's own acceptance criteria name: "Query reaches
// logical entities, drift IDs, evidence records, generations, artifacts."
type QueryKind string

const (
	// QueryKindEntity resolves one logical entity's effective value (a thin
	// wrapper over report.Explain — `omca explain`'s engine).
	QueryKindEntity QueryKind = "entity"
	// QueryKindDrift lists every current drift card's summary, or — when
	// DriftID is given — one card's full detail including its paged Matrix
	// (a thin wrapper over report.Artifact.ActionCards — `omca drift`/
	// `omca drift show`/`omca matrix`'s engine).
	QueryKindDrift QueryKind = "drift"
	// QueryKindEvidence lists one host's Knowledge Pack evidence citations
	// (report.Artifact.Debug[host].KnowledgeEvidence).
	QueryKindEvidence QueryKind = "evidence"
	// QueryKindGeneration lists one host's current or pending generation's
	// source entries plus that generation's own ID (report.Artifact.
	// Debug[host].CurrentSources/PendingSources).
	QueryKindGeneration QueryKind = "generation"
	// QueryKindArtifact returns a small, fixed-size overview of the whole
	// bound report.Artifact (report identity, per-host summaries, plane
	// counts, drift/duplicate-capability counts) — never the raw Debug
	// graphs, which are exactly the kind of large, unbounded content this
	// tool's paging discipline exists to avoid dumping by default.
	QueryKindArtifact QueryKind = "artifact"
)

// defaultQueryPageLimit / maxQueryPageLimit bound every list-shaped
// omca_query result (issue #24's "Large results use paging... a size-budget
// test bounds tool schemas and default responses" acceptance criterion): a
// caller that never sets Limit gets defaultQueryPageLimit rows, and no
// caller — however large a Limit it asks for — ever gets more than
// maxQueryPageLimit in one response. Both are small enough that even a
// worst-case fixture (many drift cards, each with a large Matrix) produces
// a bounded response, and large enough to be genuinely useful in one call
// rather than forcing a client to page one row at a time.
const (
	defaultQueryPageLimit = 50
	maxQueryPageLimit     = 200
)

// QueryArguments is omca_query's "tools/call" arguments shape.
//
// Deliberately absent: any worktree/run/generation ID field. Every query
// resolves against the one report.Artifact ArtifactFunc computes for THIS
// process's own bound worktree/generation (exactly like StatusFunc/
// ComputeStatusRequest never accept one either — see cmd/omca/mcp.go's
// runMCP doc comment for how that binding is established once, from the
// process's own environment, at server startup). This is issue #24's
// round-4 audit made structural rather than merely documented: Go's own
// type system makes a caller-supplied worktree/run/generation ID argument
// impossible to honor even by accident, because ComputeQuery's artifact
// parameter is a plain report.Artifact value the caller of ComputeQuery
// (queryToolHandler) resolves entirely independently of args — there is no
// field on this struct a retargeting attempt could even bind to, and
// encoding/json's default Unmarshal behavior silently ignores any JSON
// property with no matching struct field (a "worktreeId"/"runId"/
// "generationId" key in a crafted tools/call payload decodes into nothing,
// not an error and not a redirect). See query_test.go's
// TestQueryToolHandler_IgnoresWorktreeRetargetingArguments for the
// end-to-end proof.
type QueryArguments struct {
	// Kind selects which of the five query shapes to run. Required.
	Kind QueryKind `json:"kind"`

	// Host scopes an entity/evidence/generation query to one host. Empty
	// defaults to the first host report.Build produced Debug data for
	// (report.Artifact.Hosts[0].Host) — the same "first built host" default
	// cmd/omca/explain.go/compare.go/diff.go already use when --host is
	// omitted. Unused for kind == "drift"/"artifact" (drift cards and the
	// artifact overview are already cross-host).
	Host string `json:"host,omitempty"`

	// Concept + LogicalID select one logical entity (kind == "entity"
	// only); both are required for that kind.
	Concept   string `json:"concept,omitempty"`
	LogicalID string `json:"logicalId,omitempty"`
	// Trace requests the full resolver-trace/physical-sources/Knowledge-
	// evidence expansion (kind == "entity" only), mirroring `omca explain
	// --trace`. A caller that only wants the summary line is never charged
	// for assembling it, matching report.Explain's own doc comment.
	Trace bool `json:"trace,omitempty"`

	// DriftID selects one drift card's full detail (kind == "drift" only).
	// Leaving it empty lists every current card's summary instead.
	DriftID string `json:"driftId,omitempty"`

	// Plane selects "current" or "pending" (kind == "generation" only);
	// empty defaults to "current".
	Plane string `json:"plane,omitempty"`

	// Offset/Limit page a list-shaped result: the drift-card list, one
	// drift card's Matrix, the evidence list, or a generation's source
	// list. Offset < 0 is treated as 0; Limit <= 0 defaults to
	// defaultQueryPageLimit; Limit above maxQueryPageLimit is clamped down
	// to it. Ignored (harmlessly) for kind == "entity"/"artifact", which
	// are never list-shaped.
	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit,omitempty"`
}

// PageInfo describes one page of a list-shaped QueryResult field — present
// on every QueryResult (zero-valued and meaningless for the non-list kinds,
// "entity" and "artifact") so the wire shape does not vary field-by-field
// per kind.
type PageInfo struct {
	Offset   int  `json:"offset"`
	Limit    int  `json:"limit"`
	Total    int  `json:"total"`
	Returned int  `json:"returned"`
	HasMore  bool `json:"hasMore"`
}

// DriftSummary is one drift card's list-view row (kind == "drift" with no
// DriftID): everything ActionCard carries except its full Matrix, which is
// exactly the size-bounding cut a card-list response needs — Matrix can
// legitimately hold hundreds of rows (docs/architecture/reporting.md §7's
// "8 projects x 5 hosts x 40 artifacts" worked example), while a card's
// RootCause/Impact/EvidenceCounts summary is always small.
type DriftSummary struct {
	ID             string                       `json:"id"`
	RootCause      string                       `json:"rootCause"`
	Remediation    string                       `json:"remediation,omitempty"`
	Category       domain.DriftCategory         `json:"category"`
	AdapterVersion string                       `json:"adapterVersion,omitempty"`
	Impact         drift.Impact                 `json:"impact"`
	Guarantee      domain.GuaranteeLevel        `json:"guarantee,omitempty"`
	EvidenceCounts map[domain.EvidenceLevel]int `json:"evidenceCounts,omitempty"`
}

// DriftDetail is one drift card's full detail (kind == "drift" with a
// DriftID): its DriftSummary plus a page of its Matrix — "the report always
// exposes the complete matrix count and query" (docs/architecture/
// reporting.md §7), paged rather than dumped whole so a card with a large
// Matrix still produces a bounded response.
type DriftDetail struct {
	DriftSummary
	Matrix []drift.Assertion `json:"matrix"`
}

// GenerationDetail is one host's current/pending generation (kind ==
// "generation"): the generation's own ID (empty when no such generation
// exists yet for this host) plus a page of its Sources.
type GenerationDetail struct {
	Host         string                         `json:"host"`
	Plane        string                         `json:"plane"`
	GenerationID string                         `json:"generationId,omitempty"`
	Sources      []domain.GenerationSourceEntry `json:"sources"`
}

// HostArtifactSummary is one host's contribution to ArtifactSummary — a
// small subset of report.HostSummary (Knowledge status and plane counts
// only, never the underlying Debug graphs those counts summarize).
type HostArtifactSummary struct {
	Host      string                 `json:"host"`
	Knowledge report.HostKnowledge   `json:"knowledge"`
	Planes    report.HostPlaneCounts `json:"planes"`
}

// ArtifactSummary is kind == "artifact"'s answer: a small, fixed-size
// overview of the whole bound report.Artifact, deliberately never including
// the raw per-host Debug graphs (Candidates/Observations/full Effective
// Graph) — those are exactly the "large content" issue #24's paging
// requirement targets, and this package has no query shape that returns
// them wholesale at all; a caller that needs graph-level detail uses kind
// == "entity" (one logical entity's resolver trace, via Trace) instead.
type ArtifactSummary struct {
	ReportID    string `json:"reportId"`
	Worktree    string `json:"worktree"`
	GeneratedAt string `json:"generatedAt"`
	Fingerprint string `json:"fingerprint,omitempty"`

	Hosts []HostArtifactSummary `json:"hosts"`

	DriftCardCount           int `json:"driftCardCount"`
	DuplicateCapabilityCount int `json:"duplicateCapabilityCount"`

	Planes domain.ReportPlanes `json:"planes"`
}

// QueryResult is omca_query's complete response: Kind echoes the request,
// Page is populated for every list-shaped kind (zero-valued otherwise), and
// exactly one of the kind-specific fields below is non-nil, matching
// req.Kind — the same "one result struct, several optional kind-specific
// fields" shape report.ExplainResult's own Trace field already established
// in this codebase.
type QueryResult struct {
	Kind QueryKind `json:"kind"`
	Page PageInfo  `json:"page"`

	Entity     *report.ExplainResult         `json:"entity,omitempty"`
	Drift      *DriftDetail                  `json:"drift,omitempty"`
	DriftCards []DriftSummary                `json:"driftCards,omitempty"`
	Evidence   []domain.KnowledgeEvidenceRef `json:"evidence,omitempty"`
	Generation *GenerationDetail             `json:"generation,omitempty"`
	Artifact   *ArtifactSummary              `json:"artifact,omitempty"`
}

// ComputeQuery answers one omca_query request against a — an already-built
// report.Artifact the caller (queryToolHandler) is responsible for having
// computed fresh (see ArtifactFunc's doc comment). This function is pure
// and side-effect-free specifically so it can be tested directly against
// hand-built or synthetic Artifact fixtures (matching internal/mcp/
// status_test.go's ComputeStatus precedent) without ever needing a real
// host binary, filesystem state, or MCP transport.
func ComputeQuery(a report.Artifact, args QueryArguments) (QueryResult, error) {
	switch args.Kind {
	case QueryKindEntity:
		return queryEntity(a, args)
	case QueryKindDrift:
		return queryDrift(a, args)
	case QueryKindEvidence:
		return queryEvidence(a, args)
	case QueryKindGeneration:
		return queryGeneration(a, args)
	case QueryKindArtifact:
		return queryArtifactSummary(a), nil
	case "":
		return QueryResult{}, fmt.Errorf("mcp: ComputeQuery: \"kind\" is required (want one of entity, drift, evidence, generation, artifact)")
	default:
		return QueryResult{}, fmt.Errorf("mcp: ComputeQuery: unknown kind %q (want one of entity, drift, evidence, generation, artifact)", args.Kind)
	}
}

// resolveHost returns host if non-empty and known to a (a real error
// otherwise -- matching cmd/omca/explain.go's own explainAcrossHosts
// behavior for an explicitly-named but unbuilt host), or the first host a
// was built for when host is empty (matching cmd/omca/reportjson.go's
// firstHost default, duplicated here since that helper does not cross the
// cmd/omca <-> internal/mcp package boundary — the same small, package-
// local helper duplication internal/mcp/status_test.go's mustWriteFile
// doc comment already documents as this project's convention).
func resolveHost(a report.Artifact, host string) (string, error) {
	if host != "" {
		if _, ok := a.Debug[host]; !ok {
			return "", fmt.Errorf("mcp: ComputeQuery: host %q was not built into this report (no observation data)", host)
		}
		return host, nil
	}
	if len(a.Hosts) == 0 {
		return "", fmt.Errorf("mcp: ComputeQuery: no host was built into this report (no installed/observed host)")
	}
	return a.Hosts[0].Host, nil
}

func queryEntity(a report.Artifact, args QueryArguments) (QueryResult, error) {
	if args.Concept == "" || args.LogicalID == "" {
		return QueryResult{}, fmt.Errorf("mcp: ComputeQuery: kind %q requires both \"concept\" and \"logicalId\"", QueryKindEntity)
	}
	host, err := resolveHost(a, args.Host)
	if err != nil {
		return QueryResult{}, err
	}
	result := report.Explain(a, host, args.Concept, args.LogicalID, args.Trace)
	return QueryResult{Kind: QueryKindEntity, Entity: &result}, nil
}

// findDriftCardByID returns the ActionCard in a whose content-addressed ID
// equals id — the same lookup cmd/omca/reportjson.go's findDriftCard
// performs for `omca drift show`/`omca matrix`, duplicated here for the
// same package-boundary reason resolveHost's doc comment gives.
func findDriftCardByID(a report.Artifact, id string) (report.DriftCard, bool) {
	for _, c := range a.ActionCards {
		if c.ID == id {
			return c, true
		}
	}
	return report.DriftCard{}, false
}

func summarizeDriftCard(c report.DriftCard) DriftSummary {
	return DriftSummary{
		ID:             c.ID,
		RootCause:      c.RootCause,
		Remediation:    c.Remediation,
		Category:       c.Category,
		AdapterVersion: c.AdapterVersion,
		Impact:         c.Impact,
		Guarantee:      c.Guarantee,
		EvidenceCounts: c.EvidenceCounts,
	}
}

func queryDrift(a report.Artifact, args QueryArguments) (QueryResult, error) {
	offset, limit := normalizePage(args.Offset, args.Limit)

	if args.DriftID != "" {
		card, ok := findDriftCardByID(a, args.DriftID)
		if !ok {
			return QueryResult{}, fmt.Errorf("mcp: ComputeQuery: no drift card with ID %q", args.DriftID)
		}
		matrixPage, page := pageSlice(card.Matrix, offset, limit)
		detail := DriftDetail{DriftSummary: summarizeDriftCard(card), Matrix: matrixPage}
		return QueryResult{Kind: QueryKindDrift, Page: page, Drift: &detail}, nil
	}

	summaries := make([]DriftSummary, 0, len(a.ActionCards))
	for _, c := range a.ActionCards {
		summaries = append(summaries, summarizeDriftCard(c))
	}
	summaryPage, page := pageSlice(summaries, offset, limit)
	return QueryResult{Kind: QueryKindDrift, Page: page, DriftCards: summaryPage}, nil
}

func queryEvidence(a report.Artifact, args QueryArguments) (QueryResult, error) {
	host, err := resolveHost(a, args.Host)
	if err != nil {
		return QueryResult{}, err
	}
	offset, limit := normalizePage(args.Offset, args.Limit)
	evidencePage, page := pageSlice(a.Debug[host].KnowledgeEvidence, offset, limit)
	return QueryResult{Kind: QueryKindEvidence, Page: page, Evidence: evidencePage}, nil
}

func queryGeneration(a report.Artifact, args QueryArguments) (QueryResult, error) {
	host, err := resolveHost(a, args.Host)
	if err != nil {
		return QueryResult{}, err
	}
	plane := strings.ToLower(strings.TrimSpace(args.Plane))
	if plane == "" {
		plane = "current"
	}

	hd := a.Debug[host]
	var sources []domain.GenerationSourceEntry
	var genID string
	switch plane {
	case "current":
		sources, genID = hd.CurrentSources, hd.CurrentGenerationID
	case "pending":
		sources, genID = hd.PendingSources, hd.PendingGenerationID
	default:
		return QueryResult{}, fmt.Errorf("mcp: ComputeQuery: unknown plane %q for kind %q (want \"current\" or \"pending\")", args.Plane, QueryKindGeneration)
	}

	offset, limit := normalizePage(args.Offset, args.Limit)
	sourcesPage, page := pageSlice(sources, offset, limit)
	detail := GenerationDetail{Host: host, Plane: plane, GenerationID: genID, Sources: sourcesPage}
	return QueryResult{Kind: QueryKindGeneration, Page: page, Generation: &detail}, nil
}

func queryArtifactSummary(a report.Artifact) QueryResult {
	hosts := make([]HostArtifactSummary, 0, len(a.Hosts))
	for _, h := range a.Hosts {
		hosts = append(hosts, HostArtifactSummary{Host: h.Host, Knowledge: h.Knowledge, Planes: h.Planes})
	}
	summary := ArtifactSummary{
		ReportID:                 a.Report.Metadata.ID,
		Worktree:                 a.Report.Metadata.Worktree,
		GeneratedAt:              a.Report.Metadata.GeneratedAt,
		Fingerprint:              a.Report.Spec.Fingerprint,
		Hosts:                    hosts,
		DriftCardCount:           len(a.ActionCards),
		DuplicateCapabilityCount: len(a.DuplicateCapabilities),
		Planes:                   a.Report.Spec.Planes,
	}
	return QueryResult{Kind: QueryKindArtifact, Artifact: &summary}
}

// normalizePage clamps a caller-supplied (offset, limit) pair into this
// tool's disciplined range: offset never negative, limit always in
// [1, maxQueryPageLimit], defaulting to defaultQueryPageLimit when the
// caller left it unset (<= 0) — the one place every list-shaped query kind
// enforces issue #24's size-budget requirement, so a new query kind added
// later gets it for free by calling pageSlice rather than needing to
// re-derive the same clamping.
func normalizePage(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = defaultQueryPageLimit
	}
	if limit > maxQueryPageLimit {
		limit = maxQueryPageLimit
	}
	return offset, limit
}

// pageSlice returns items[offset:offset+limit] (clamped to items' actual
// bounds) plus the PageInfo describing that slice — offset/limit are
// assumed already normalized (normalizePage). An offset at or beyond
// len(items) returns an empty, non-nil slice (never items itself, and never
// nil where a JSON consumer might otherwise see `null` instead of `[]`) so
// the response shape stays uniform regardless of how far past the end a
// caller pages.
func pageSlice[T any](items []T, offset, limit int) ([]T, PageInfo) {
	total := len(items)
	info := PageInfo{Offset: offset, Limit: limit, Total: total}
	if offset >= total {
		return []T{}, info
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := items[offset:end]
	info.Returned = len(out)
	info.HasMore = end < total
	return out, info
}

// queryToolDescription is what tools/list reports for omca_query --
// deliberately small, matching statusToolDescription's own "tool schemas
// and default responses remain deliberately small" standard
// (docs/architecture/runtime.md §6's M4 exit-gate design goal).
const queryToolDescription = "Query the bound worktree's current report: one logical entity's resolved effective value and evidence (kind=entity, needs concept+logicalId), a drift card or the current drift-card list (kind=drift, optional driftId), one host's Knowledge Pack evidence citations (kind=evidence), one host's current or pending generation's source list (kind=generation, optional plane), or a small overview of the whole report artifact (kind=artifact). Always scoped to this process's own bound worktree/generation -- there is no worktree/run/generation argument. List-shaped results are paged (offset/limit, default 50 rows, max 200 per call)."

// queryInputSchema is omca_query's tools/list inputSchema. It deliberately
// has no worktree/run/generation-id property at all (see QueryArguments'
// own doc comment for why that is the load-bearing security property, not
// this schema) and additionalProperties:false, so a schema-validating
// client gets an explicit signal that no such argument is accepted, on top
// of QueryArguments' own structural inability to bind one.
func queryInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind": map[string]any{
				"type":        "string",
				"enum":        []string{string(QueryKindEntity), string(QueryKindDrift), string(QueryKindEvidence), string(QueryKindGeneration), string(QueryKindArtifact)},
				"description": "Which shape of thing to query.",
			},
			"host": map[string]any{
				"type":        "string",
				"description": "Host to scope an entity/evidence/generation query to (default: the first host this report was built for). Ignored for kind=drift/artifact.",
			},
			"concept": map[string]any{
				"type":        "string",
				"description": "Logical entity's concept (kind=entity, required).",
			},
			"logicalId": map[string]any{
				"type":        "string",
				"description": "Logical entity's ID (kind=entity, required).",
			},
			"trace": map[string]any{
				"type":        "boolean",
				"description": "Expand the full resolver-trace/physical-sources/evidence chain (kind=entity only).",
			},
			"driftId": map[string]any{
				"type":        "string",
				"description": "One drift card's content-addressed ID (kind=drift). Omit to list every current card's summary instead.",
			},
			"plane": map[string]any{
				"type":        "string",
				"enum":        []string{"current", "pending"},
				"description": "Which generation to query (kind=generation, default \"current\").",
			},
			"offset": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"description": "Row offset for a paged list result (default 0).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     maxQueryPageLimit,
				"description": "Max rows for a paged list result (default 50, max 200).",
			},
		},
		"required":             []string{"kind"},
		"additionalProperties": false,
	}
}

// queryToolHandler adapts an ArtifactFunc into a toolHandler: decode
// arguments, compute a FRESH report.Artifact (never a cached one -- see
// ArtifactFunc's doc comment), then answer via the pure ComputeQuery.
// Both an argument-decoding failure and an ArtifactFunc failure become
// tool-level errors (IsError:true, via handleToolsCall's shared error
// path), matching StatusFunc's own failure handling -- a client can recover
// from either without treating the whole JSON-RPC exchange as broken.
func queryToolHandler(artifactFn ArtifactFunc) toolHandler {
	return func(arguments json.RawMessage) (any, error) {
		var args QueryArguments
		if len(arguments) > 0 {
			if err := json.Unmarshal(arguments, &args); err != nil {
				return nil, fmt.Errorf("mcp: omca_query: invalid arguments: %w", err)
			}
		}
		artifact, err := artifactFn()
		if err != nil {
			return nil, fmt.Errorf("mcp: omca_query: computing report: %w", err)
		}
		return ComputeQuery(artifact, args)
	}
}
