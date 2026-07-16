package plugin

import "fmt"

// ReconcileMode is the closed reconcile-mode vocabulary describing what an
// adapter does with the native artifact for one concept, independent of the
// Capability rating (docs/knowledge/README.md §5). Ownership follows the same
// vocabulary as docs/architecture/README.md §10.
type ReconcileMode string

const (
	// ReconcileManaged means OMCA owns the complete generated artifact.
	ReconcileManaged ReconcileMode = "MANAGED"
	// ReconcilePatched means OMCA owns specific fields in an external
	// artifact.
	ReconcilePatched ReconcileMode = "PATCHED"
	// ReconcileObserved means OMCA reports but does not write.
	ReconcileObserved ReconcileMode = "OBSERVED"
	// ReconcileOpaque means OMCA preserves the native block without
	// semantic interpretation.
	ReconcileOpaque ReconcileMode = "OPAQUE"
	// ReconcileBlocked means the adapter must not write or activate this
	// concept for the requested host (e.g. a capability gap or conflicted
	// Knowledge); write operations must fail closed rather than proceed.
	ReconcileBlocked ReconcileMode = "BLOCKED"
)

// Valid reports whether m is one of the five defined reconcile modes.
func (m ReconcileMode) Valid() bool {
	switch m {
	case ReconcileManaged, ReconcilePatched, ReconcileObserved, ReconcileOpaque, ReconcileBlocked:
		return true
	default:
		return false
	}
}

// ValidateReconcileMode rejects any value outside the closed reconcile-mode
// enum.
func ValidateReconcileMode(m ReconcileMode) error {
	if !m.Valid() {
		return fmt.Errorf("invalid reconcile mode %q", m)
	}
	return nil
}
