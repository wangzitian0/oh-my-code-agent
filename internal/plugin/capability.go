package plugin

import "fmt"

// Capability is the closed capability vocabulary a Knowledge Pack or an
// adapter's CapabilityManifest uses to rate one host × surface × version ×
// concept × operation cell (docs/knowledge/README.md §5 Capability
// Vocabulary). There is no host-wide "supported" flag: every rating is
// per-operation.
type Capability string

const (
	// CapabilityExact means semantics and representation are proven for the
	// declared operation.
	CapabilityExact Capability = "EXACT"
	// CapabilityCompatible means canonical behavior is compatible but native
	// representation differs.
	CapabilityCompatible Capability = "COMPATIBLE"
	// CapabilityPartial means only declared fields or scenarios are
	// supported.
	CapabilityPartial Capability = "PARTIAL"
	// CapabilityOpaque means location and content digest are preserved
	// without semantic interpretation.
	CapabilityOpaque Capability = "OPAQUE"
	// CapabilityUnknown means primary evidence does not establish behavior.
	CapabilityUnknown Capability = "UNKNOWN"
	// CapabilityUnsupported means the host has no corresponding operation or
	// concept.
	CapabilityUnsupported Capability = "UNSUPPORTED"
)

// Valid reports whether c is one of the six defined capability levels.
func (c Capability) Valid() bool {
	switch c {
	case CapabilityExact, CapabilityCompatible, CapabilityPartial, CapabilityOpaque, CapabilityUnknown, CapabilityUnsupported:
		return true
	default:
		return false
	}
}

// ValidateCapability rejects any value outside the closed capability enum.
func ValidateCapability(c Capability) error {
	if !c.Valid() {
		return fmt.Errorf("invalid capability %q", c)
	}
	return nil
}
