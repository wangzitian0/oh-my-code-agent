package domain

import "fmt"

// SourceDisposition is why a discovered source does or does not take effect
// (docs/architecture/reporting.md §3, "Source Disposition").
type SourceDisposition string

const (
	// DispositionDiscovered: found in a known physical source.
	DispositionDiscovered SourceDisposition = "DISCOVERED"
	// DispositionImported: explicitly brought into desired/runtime state.
	DispositionImported SourceDisposition = "IMPORTED"
	// DispositionActive: present in the current or host-effective state.
	DispositionActive SourceDisposition = "ACTIVE"
	// DispositionAvailable: cataloged but not selected.
	DispositionAvailable SourceDisposition = "AVAILABLE"
	// DispositionExcluded: intentionally absent from the runtime.
	DispositionExcluded SourceDisposition = "EXCLUDED"
	// DispositionDenied: blocked by policy.
	DispositionDenied SourceDisposition = "DENIED"
	// DispositionShadowed: discovered but ineffective because another
	// source wins.
	DispositionShadowed SourceDisposition = "SHADOWED"
	// DispositionOrphaned: source or installer ownership no longer
	// resolves.
	DispositionOrphaned SourceDisposition = "ORPHANED"
	// DispositionOpaque: retained without semantic interpretation.
	DispositionOpaque SourceDisposition = "OPAQUE"
	// DispositionUnknown: identity, precedence, or behavior is not proven.
	DispositionUnknown SourceDisposition = "UNKNOWN"
)

var validDispositions = map[SourceDisposition]bool{
	DispositionDiscovered: true,
	DispositionImported:   true,
	DispositionActive:     true,
	DispositionAvailable:  true,
	DispositionExcluded:   true,
	DispositionDenied:     true,
	DispositionShadowed:   true,
	DispositionOrphaned:   true,
	DispositionOpaque:     true,
	DispositionUnknown:    true,
}

// Valid reports whether d is one of the ten defined source dispositions.
func (d SourceDisposition) Valid() bool {
	return validDispositions[d]
}

// ValidateSourceDisposition rejects any value outside the closed
// disposition enum.
func ValidateSourceDisposition(d SourceDisposition) error {
	if !d.Valid() {
		return fmt.Errorf("invalid source disposition %q", d)
	}
	return nil
}
