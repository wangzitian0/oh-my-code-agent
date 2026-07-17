package drift

import "github.com/wangzitian0/oh-my-code-agent/internal/domain"

// Signal is one candidate machine-level difference the engine may classify
// into an [Assertion]: an entity field's expected value (DESIRED/CURRENT
// plane) versus its observed/effective value (OBSERVED/HOST_EFFECTIVE
// plane), plus the root cause, remediation, and context dimensions
// reporting.md §6/§7 require. It is the graph-shaped input this package
// consumes in place of PR-17's not-yet-landed real resolver output (see
// doc.go's "Scope and PR-17 dependency" section).
//
// Category names the base drift category this Signal represents before the
// engine's own EXCEPTION/UNKNOWN overlay (see Classify): one of
// domain.DriftConfigDrift, DriftEffectiveDrift, DriftSourceDrift,
// DriftCapabilityGap, DriftKnowledgeDrift, or DriftContextDrift. Leaving it
// empty (or explicitly domain.DriftUnknown) marks a signal the producer
// itself could not safely classify — the UNKNOWN category exists precisely
// for that case, so this is a valid, expected input, not a caller bug.
// domain.DriftException is rejected: the engine computes that category
// itself from Exception matching and must never be handed it directly.
type Signal struct {
	EntityID string
	Concept  string
	Field    string
	Category domain.DriftCategory

	Expected any
	Observed any

	RootCause   string
	Remediation string

	// Project, Host, HostVersion, and AdapterVersion are the structured
	// context-cell dimensions (docs/architecture/reporting.md §6's "host/
	// version/context cell" and §7's grouping-by-adapter-version and
	// impact-counting-by-project/host).
	Project        string
	Host           string
	HostVersion    string
	AdapterVersion string

	EvidenceLevel domain.EvidenceLevel
	Guarantee     domain.GuaranteeLevel

	// AssetID is the identity an Exception's AssetID is matched against.
	// It defaults to EntityID when empty, since for most drift signals the
	// entity being asserted about and the asset an Exception documents are
	// the same thing.
	AssetID string

	// ExceptionScopes lists the policy/Profile IDs whose Exception can
	// except this signal (domain.Exception.Scope — "the defining Profile's
	// ID", per internal/domain/exception.go). It defaults to []string
	// {RootCause} when empty: RootCause is typically the same defining
	// policy ID an Exception's Scope names (e.g. "company:example/
	// security-default" in reporting.md §7's worked example), so this
	// keeps hand-built fixtures from having to repeat it.
	ExceptionScopes []string
}

// Assertion is one classified Drift record: a domain.DriftAssertion (the
// frozen protocol shape, embedded so its fields are promoted and Assertion
// satisfies every "matches the assertion form" check the same way a bare
// domain.DriftAssertion would) plus the structured dimensions [Group] needs
// to aggregate by root cause and count impact by project/host, and the
// bookkeeping EXCEPTION classification leaves behind.
type Assertion struct {
	domain.DriftAssertion

	// UnderlyingCategory is the base category Classify computed before any
	// EXCEPTION overlay — always populated, even when Category itself is
	// domain.DriftException, so "an expired exception reverts to its
	// underlying drift class" has a concrete field to revert to and a
	// concrete field to test against (docs/architecture/reporting.md §6).
	UnderlyingCategory domain.DriftCategory `json:"underlyingCategory"`

	Project        string `json:"project,omitempty"`
	Host           string `json:"host,omitempty"`
	HostVersion    string `json:"hostVersion,omitempty"`
	AdapterVersion string `json:"adapterVersion,omitempty"`

	// ExceptionRef is the metadata.id of the domain.Exception that produced
	// an EXCEPTION classification, empty otherwise (including when the
	// exception has expired and classification reverted).
	ExceptionRef string `json:"exceptionRef,omitempty"`
}

// Impact is an ActionCard's cross-product summary
// (docs/architecture/reporting.md §7's "Impact 8 projects · 5 hosts · 40
// artifacts" line): the distinct project count, distinct host count, and
// total assertion count (one per affected entity field cell) the card's
// full Matrix spans.
type Impact struct {
	Projects  int `json:"projects"`
	Hosts     int `json:"hosts"`
	Artifacts int `json:"artifacts"`
}

// ActionCard is one human-scale root-cause group
// (docs/architecture/reporting.md §7): every Assertion sharing the same
// root cause, remediation, outcome class (Category), and adapter version
// collapses into a single card, rather than one alert per affected entity.
type ActionCard struct {
	RootCause      string               `json:"rootCause"`
	Remediation    string               `json:"remediation,omitempty"`
	Category       domain.DriftCategory `json:"category"`
	AdapterVersion string               `json:"adapterVersion,omitempty"`

	Impact Impact `json:"impact"`

	// EvidenceCounts tallies Matrix entries by EvidenceLevel (reporting.md
	// §7's "Evidence 38 × E3, 2 × E2" line).
	EvidenceCounts map[domain.EvidenceLevel]int `json:"evidenceCounts,omitempty"`

	// Guarantee is the card's representative GuaranteeLevel: populated only
	// when every Matrix entry shares the same non-empty level, left empty
	// when the card's assertions disagree rather than silently picking one
	// and hiding the disagreement.
	Guarantee domain.GuaranteeLevel `json:"guarantee,omitempty"`

	// Samples is a deterministic, bounded subset of Matrix: one
	// representative per distinct (outcome, exceptional) bucket first, then
	// redundant entries in canonical Matrix order up to the sample limit
	// (see selectSamples). It is illustrative only — Matrix is the source
	// of truth and is always fully queryable (reporting.md §7: "The report
	// always exposes the complete matrix count and query").
	Samples []Assertion `json:"samples"`

	// Matrix is every Assertion this card groups, sorted deterministically
	// by (Project, Host, EntityID, Field). The full matrix stays queryable
	// from every card via Query and Matrix itself.
	Matrix []Assertion `json:"matrix"`
}

// Query returns every Matrix entry for entityID, preserving Matrix's
// deterministic order. It is the "full matrix stays queryable from every
// card" hook (docs/architecture/reporting.md §7, §14's debug invariant
// "every action card expands to all affected cells").
func (c ActionCard) Query(entityID string) []Assertion {
	var out []Assertion
	for _, a := range c.Matrix {
		if a.EntityID == entityID {
			out = append(out, a)
		}
	}
	return out
}
