package domain

import "fmt"

// DriftCategory is the canonical classification of one Drift assertion
// (docs/architecture/reporting.md §6, "Drift Model" canonical categories).
// Not one of the six enums PR-04's issue names explicitly, but Report's
// drift entries need a closed category exactly as much as the named six do,
// and reporting.md calls this set "canonical categories" verbatim.
type DriftCategory string

const (
	// DriftConfigDrift: a managed artifact differs from desired state.
	DriftConfigDrift DriftCategory = "CONFIG_DRIFT"
	// DriftEffectiveDrift: host-effective state differs from
	// desired/current state.
	DriftEffectiveDrift DriftCategory = "EFFECTIVE_DRIFT"
	// DriftSourceDrift: representations of one logical entity diverge.
	DriftSourceDrift DriftCategory = "SOURCE_DRIFT"
	// DriftCapabilityGap: adapter cannot safely normalize, compile, or
	// verify.
	DriftCapabilityGap DriftCategory = "CAPABILITY_GAP"
	// DriftKnowledgeDrift: host version or evidence exceeds qualified
	// Knowledge.
	DriftKnowledgeDrift DriftCategory = "KNOWLEDGE_DRIFT"
	// DriftContextDrift: invocation bypassed or selected a different
	// context.
	DriftContextDrift DriftCategory = "CONTEXT_DRIFT"
	// DriftException: authorized, documented, and unexpired difference.
	DriftException DriftCategory = "EXCEPTION"
	// DriftUnknown: the system cannot safely classify the result.
	DriftUnknown DriftCategory = "UNKNOWN"
)

var validDriftCategories = map[DriftCategory]bool{
	DriftConfigDrift:    true,
	DriftEffectiveDrift: true,
	DriftSourceDrift:    true,
	DriftCapabilityGap:  true,
	DriftKnowledgeDrift: true,
	DriftContextDrift:   true,
	DriftException:      true,
	DriftUnknown:        true,
}

// Valid reports whether c is one of the eight defined drift categories.
func (c DriftCategory) Valid() bool {
	return validDriftCategories[c]
}

// ValidateDriftCategory rejects any value outside the closed drift category
// enum.
func ValidateDriftCategory(c DriftCategory) error {
	if !c.Valid() {
		return fmt.Errorf("invalid drift category %q", c)
	}
	return nil
}
