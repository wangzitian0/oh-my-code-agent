package domain

import "testing"

func TestSourceDispositionValid(t *testing.T) {
	valid := []SourceDisposition{
		DispositionDiscovered, DispositionImported, DispositionActive,
		DispositionAvailable, DispositionExcluded, DispositionDenied,
		DispositionShadowed, DispositionOrphaned, DispositionOpaque,
		DispositionUnknown,
	}
	for _, d := range valid {
		if !d.Valid() {
			t.Errorf("SourceDisposition(%q).Valid() = false, want true", d)
		}
		if err := ValidateSourceDisposition(d); err != nil {
			t.Errorf("ValidateSourceDisposition(%q) = %v, want nil", d, err)
		}
	}

	invalid := SourceDisposition("HIDDEN")
	if invalid.Valid() {
		t.Error("SourceDisposition(HIDDEN).Valid() = true, want false")
	}
	if err := ValidateSourceDisposition(invalid); err == nil {
		t.Error("ValidateSourceDisposition(HIDDEN) = nil, want error")
	}
}
