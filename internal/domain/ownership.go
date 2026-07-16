package domain

import "fmt"

// Ownership is who is responsible for writing and maintaining an artifact
// (docs/architecture/README.md §10, "Ownership"). Unlike the other closed
// enums in this package, the doc spells these values lowercase, and this
// type preserves that casing verbatim rather than upper-casing for
// consistency with Intent/SourceDisposition/etc.
type Ownership string

const (
	// OwnershipManaged: OMCA owns the complete generated artifact.
	OwnershipManaged Ownership = "managed"
	// OwnershipPatched: OMCA owns specific fields in an external artifact.
	OwnershipPatched Ownership = "patched"
	// OwnershipObserved: OMCA reports but does not write.
	OwnershipObserved Ownership = "observed"
	// OwnershipPassthrough: OMCA parses around and preserves the native
	// block.
	OwnershipPassthrough Ownership = "passthrough"
	// OwnershipExternal: another authority owns the state.
	OwnershipExternal Ownership = "external"
)

// Valid reports whether o is one of the five defined ownership tiers.
func (o Ownership) Valid() bool {
	switch o {
	case OwnershipManaged, OwnershipPatched, OwnershipObserved, OwnershipPassthrough, OwnershipExternal:
		return true
	default:
		return false
	}
}

// ValidateOwnership rejects any value outside the closed ownership enum.
func ValidateOwnership(o Ownership) error {
	if !o.Valid() {
		return fmt.Errorf("invalid ownership %q", o)
	}
	return nil
}
