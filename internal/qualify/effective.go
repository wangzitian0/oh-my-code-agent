package qualify

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/ontology"
)

// effectiveKind is the fixed kind literal every expected-effective.json must
// declare.
const effectiveKind = "FixtureExpectedEffective"

// Unknown is the sentinel ExpectedEffectiveEntry.SelectedSource (and
// IgnoredSources) value recorded whenever this qualification pass's safe-
// invocation rules did not let it behaviorally confirm which source wins
// (docs/ontology/README.md: "UNKNOWN is safer than a guessed adapter";
// issue #10 acceptance criterion: "no guessed winners").
const Unknown = "UNKNOWN"

// SourceRef is one physical source contributing to an
// ExpectedEffectiveEntry.
type SourceRef struct {
	Scope       string                   `json:"scope"`
	Path        string                   `json:"path"`
	Disposition domain.SourceDisposition `json:"disposition"`
}

// DocumentedClaim cites what docs/ontology/README.md (or
// docs/knowledge/README.md) already claims about this precedence question,
// kept structurally separate from SelectedSource/Confirmed so a documented
// claim can never be silently mistaken for something this fixture pass
// itself established.
type DocumentedClaim struct {
	Statement     string               `json:"statement"`
	Citation      string               `json:"citation"`
	EvidenceLevel domain.EvidenceLevel `json:"evidenceLevel"`
}

// ExpectedEffectiveEntry is this lab's recorded expectation for one logical
// entity (e.g. one instruction concatenation, one skill name, one MCP server
// ID) under a concept collision case.
type ExpectedEffectiveEntry struct {
	LogicalID       string                 `json:"logicalId"`
	Sources         []SourceRef            `json:"sources"`
	MergeOperator   ontology.MergeOperator `json:"mergeOperator"`
	SelectedSource  string                 `json:"selectedSource"`
	IgnoredSources  []string               `json:"ignoredSources,omitempty"`
	Guarantee       domain.GuaranteeLevel  `json:"guarantee"`
	EvidenceLevel   domain.EvidenceLevel   `json:"evidenceLevel"`
	Confirmed       bool                   `json:"confirmed"`
	DocumentedClaim *DocumentedClaim       `json:"documentedClaim,omitempty"`
	Reason          string                 `json:"reason"`
}

// ExpectedEffectiveDocument is the parsed shape of expected-effective.json.
type ExpectedEffectiveDocument struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Host       domain.ObservationHost   `json:"host"`
	Concept    string                   `json:"concept"`
	Entries    []ExpectedEffectiveEntry `json:"entries"`
}

// confirmingLevels is the set of EvidenceLevels strong enough to back a
// Confirmed=true entry: host-reported, behavior-probed, or externally
// proven. E0/E1/E2 describe discovery, parsing, or an unqualified resolver
// computation — none of those is this qualification lab actually observing
// the host behave a certain way, so none can back a "this fixture pass
// confirmed it" claim.
var confirmingLevels = map[domain.EvidenceLevel]bool{
	domain.EvidenceLevelHostReported:     true,
	domain.EvidenceLevelBehaviorProbed:   true,
	domain.EvidenceLevelExternallyProven: true,
}

// LoadExpectedEffective reads and validates one case's expected-effective.json.
func LoadExpectedEffective(path string) (ExpectedEffectiveDocument, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ExpectedEffectiveDocument{}, fmt.Errorf("qualify: LoadExpectedEffective: %w", err)
	}
	var doc ExpectedEffectiveDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ExpectedEffectiveDocument{}, fmt.Errorf("qualify: LoadExpectedEffective: %s: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return ExpectedEffectiveDocument{}, fmt.Errorf("qualify: LoadExpectedEffective: %s: %w", path, err)
	}
	return doc, nil
}

// Validate rejects a structurally malformed document, a closed-enum
// violation, or — the discipline this package exists to enforce — an entry
// that claims Confirmed=true without evidence strong enough to back it, or
// that leaves SelectedSource as Unknown while still claiming Confirmed=true.
func (d ExpectedEffectiveDocument) Validate() error {
	if err := domain.ValidateAPIVersion("FixtureExpectedEffective", d.APIVersion); err != nil {
		return err
	}
	if d.Kind != effectiveKind {
		return fmt.Errorf("expected kind %q, got %q", effectiveKind, d.Kind)
	}
	if err := domain.ValidateHostID(d.Host.ID); err != nil {
		return err
	}
	if _, ok := ontology.Concept(d.Concept); !ok {
		return fmt.Errorf("concept %q is not a known ontology concept", d.Concept)
	}
	if len(d.Entries) == 0 {
		return fmt.Errorf("entries must not be empty")
	}
	for i, e := range d.Entries {
		if e.LogicalID == "" {
			return fmt.Errorf("entries[%d]: logicalId is required", i)
		}
		if err := ontology.ValidateMergeOperator(e.MergeOperator); err != nil {
			return fmt.Errorf("entries[%d] (%s): %w", i, e.LogicalID, err)
		}
		if err := domain.ValidateGuaranteeLevel(e.Guarantee); err != nil {
			return fmt.Errorf("entries[%d] (%s): %w", i, e.LogicalID, err)
		}
		if err := domain.ValidateEvidenceLevel(e.EvidenceLevel); err != nil {
			return fmt.Errorf("entries[%d] (%s): %w", i, e.LogicalID, err)
		}
		if e.SelectedSource == "" {
			return fmt.Errorf("entries[%d] (%s): selectedSource is required (use %q if unproven)", i, e.LogicalID, Unknown)
		}
		if e.Reason == "" {
			return fmt.Errorf("entries[%d] (%s): reason is required", i, e.LogicalID)
		}
		if e.SelectedSource == Unknown && e.Confirmed {
			return fmt.Errorf("entries[%d] (%s): selectedSource is %q but confirmed=true — an unproven winner must not be marked confirmed", i, e.LogicalID, Unknown)
		}
		if e.Confirmed && !confirmingLevels[e.EvidenceLevel] {
			return fmt.Errorf("entries[%d] (%s): confirmed=true requires an evidence level of E3, E4, or E5 (host-reported, behavior-probed, or externally proven); got %s", i, e.LogicalID, e.EvidenceLevel)
		}
		for _, src := range e.Sources {
			if err := domain.ValidateScopeKind(src.Scope); err != nil {
				return fmt.Errorf("entries[%d] (%s): source %q: %w", i, e.LogicalID, src.Path, err)
			}
			if err := domain.ValidateSourceDisposition(src.Disposition); err != nil {
				return fmt.Errorf("entries[%d] (%s): source %q: %w", i, e.LogicalID, src.Path, err)
			}
		}
	}
	return nil
}
