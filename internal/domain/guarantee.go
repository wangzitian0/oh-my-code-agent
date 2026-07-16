package domain

import "fmt"

// GuaranteeLevel answers "what prevents this from changing or being
// violated?" as distinct from EvidenceLevel, which answers "why do we
// believe this result?" (docs/architecture/reporting.md §5, "Guarantee
// Levels"). The two dimensions are independent.
type GuaranteeLevel string

const (
	// GuaranteeHard: an OS, enterprise, or host enforcement mechanism
	// prevents violation.
	GuaranteeHard GuaranteeLevel = "HARD"
	// GuaranteeReconciled: OMCA detects and restores drift, but temporary
	// divergence is possible.
	GuaranteeReconciled GuaranteeLevel = "RECONCILED"
	// GuaranteeAdvisory: correctness depends on model or human compliance.
	GuaranteeAdvisory GuaranteeLevel = "ADVISORY"
	// GuaranteeObserved: OMCA can report but cannot control the outcome.
	GuaranteeObserved GuaranteeLevel = "OBSERVED"
)

// Valid reports whether g is one of the four defined guarantee levels.
func (g GuaranteeLevel) Valid() bool {
	switch g {
	case GuaranteeHard, GuaranteeReconciled, GuaranteeAdvisory, GuaranteeObserved:
		return true
	default:
		return false
	}
}

// ValidateGuaranteeLevel rejects any value outside the closed guarantee enum.
func ValidateGuaranteeLevel(g GuaranteeLevel) error {
	if !g.Valid() {
		return fmt.Errorf("invalid guarantee level %q", g)
	}
	return nil
}
