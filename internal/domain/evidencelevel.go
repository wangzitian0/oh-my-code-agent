package domain

import "fmt"

// EvidenceLevel is how strongly a reported conclusion was established
// (docs/architecture/reporting.md §4, "Evidence Levels"). The wire value is
// the short code ("E0".."E5"), matching how docs/ontology/README.md §7's
// Adapter Record Contract and docs/knowledge/README.md §4's Knowledge Pack
// Contract both write it (e.g. `level: E2`), not the long-form name.
type EvidenceLevel string

const (
	// EvidenceLevelDiscovered (E0): a physical source or host surface was found.
	EvidenceLevelDiscovered EvidenceLevel = "E0"
	// EvidenceLevelParsed (E1): the source was parsed losslessly or retained
	// safely as opaque.
	EvidenceLevelParsed EvidenceLevel = "E1"
	// EvidenceLevelResolved (E2): a qualified resolver computed the effective
	// result.
	EvidenceLevelResolved EvidenceLevel = "E2"
	// EvidenceLevelHostReported (E3): a native status, environment, debug, or
	// introspection interface confirmed it.
	EvidenceLevelHostReported EvidenceLevel = "E3"
	// EvidenceLevelBehaviorProbed (E4): an isolated session produced the
	// expected canary behavior.
	EvidenceLevelBehaviorProbed EvidenceLevel = "E4"
	// EvidenceLevelExternallyProven (E5): an OS, enterprise control, or
	// independent audit system established it.
	EvidenceLevelExternallyProven EvidenceLevel = "E5"
)

// evidenceLevelNames maps the closed set of wire codes to the long-form name
// docs/architecture/reporting.md §4 gives each level, for display only; the
// wire/comparison value always remains the short code.
var evidenceLevelNames = map[EvidenceLevel]string{
	EvidenceLevelDiscovered:       "DISCOVERED",
	EvidenceLevelParsed:           "PARSED",
	EvidenceLevelResolved:         "RESOLVED",
	EvidenceLevelHostReported:     "HOST_REPORTED",
	EvidenceLevelBehaviorProbed:   "BEHAVIOR_PROBED",
	EvidenceLevelExternallyProven: "EXTERNALLY_PROVEN",
}

// Valid reports whether e is one of the six defined evidence levels.
func (e EvidenceLevel) Valid() bool {
	_, ok := evidenceLevelNames[e]
	return ok
}

// Name returns the long-form name for e (e.g. "RESOLVED" for E2), or "" if e
// is not a valid level.
func (e EvidenceLevel) Name() string {
	return evidenceLevelNames[e]
}

// evidenceLevelRank orders the closed set of levels for same-claim
// comparison only (see StrongestEvidence); it must never be used to compare
// evidence for two different claims.
var evidenceLevelRank = map[EvidenceLevel]int{
	EvidenceLevelDiscovered:       0,
	EvidenceLevelParsed:           1,
	EvidenceLevelResolved:         2,
	EvidenceLevelHostReported:     3,
	EvidenceLevelBehaviorProbed:   4,
	EvidenceLevelExternallyProven: 5,
}

// Rank returns e's position in the closed E0-E5 ordering (0 for E0 through 5
// for E5), or -1 if e is not a valid level.
func (e EvidenceLevel) Rank() int {
	if r, ok := evidenceLevelRank[e]; ok {
		return r
	}
	return -1
}

// ValidateEvidenceLevel rejects any value outside the closed E0-E5 enum.
func ValidateEvidenceLevel(e EvidenceLevel) error {
	if !e.Valid() {
		return fmt.Errorf("invalid evidence level %q", e)
	}
	return nil
}
