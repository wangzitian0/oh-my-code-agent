package domain

import "fmt"

// CapabilityLevel is the relation a Knowledge Pack declares for one
// host × surface × version × concept × operation cell
// (docs/knowledge/README.md §5, "Capability Vocabulary"). It is distinct
// from the ontology's per-host mapping classification
// (docs/ontology/README.md §1.2: EXACT/PARTIAL/VENDOR_ONLY/ABSENT/UNKNOWN) —
// that vocabulary describes cross-host concept mappings, this one describes
// one Knowledge Pack's proven operations.
type CapabilityLevel string

const (
	// CapabilityExact: semantics and representation are proven for the
	// declared operation.
	CapabilityExact CapabilityLevel = "EXACT"
	// CapabilityCompatible: canonical behavior is compatible but native
	// representation differs.
	CapabilityCompatible CapabilityLevel = "COMPATIBLE"
	// CapabilityPartial: only declared fields or scenarios are supported.
	CapabilityPartial CapabilityLevel = "PARTIAL"
	// CapabilityOpaque: location and content digest are preserved without
	// semantic interpretation.
	CapabilityOpaque CapabilityLevel = "OPAQUE"
	// CapabilityUnknown: primary evidence does not establish behavior.
	CapabilityUnknown CapabilityLevel = "UNKNOWN"
	// CapabilityUnsupported: the host has no corresponding operation or
	// concept.
	CapabilityUnsupported CapabilityLevel = "UNSUPPORTED"
)

// Valid reports whether c is one of the six defined capability levels.
func (c CapabilityLevel) Valid() bool {
	switch c {
	case CapabilityExact, CapabilityCompatible, CapabilityPartial, CapabilityOpaque, CapabilityUnknown, CapabilityUnsupported:
		return true
	default:
		return false
	}
}

// ValidateCapabilityLevel rejects any value outside the closed capability
// vocabulary enum.
func ValidateCapabilityLevel(c CapabilityLevel) error {
	if !c.Valid() {
		return fmt.Errorf("invalid capability level %q", c)
	}
	return nil
}
