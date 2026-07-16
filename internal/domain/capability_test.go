package domain

import "testing"

func TestCapabilityLevelValid(t *testing.T) {
	valid := []CapabilityLevel{
		CapabilityExact, CapabilityCompatible, CapabilityPartial,
		CapabilityOpaque, CapabilityUnknown, CapabilityUnsupported,
	}
	for _, c := range valid {
		if !c.Valid() {
			t.Errorf("CapabilityLevel(%q).Valid() = false, want true", c)
		}
		if err := ValidateCapabilityLevel(c); err != nil {
			t.Errorf("ValidateCapabilityLevel(%q) = %v, want nil", c, err)
		}
	}

	// VENDOR_ONLY/ABSENT/PARTIAL-without-COMPATIBLE belong to the ontology's
	// classification vocabulary (docs/ontology/README.md §1.2), a distinct
	// enum from this Knowledge Pack capability vocabulary; VENDOR_ONLY must
	// not leak in here as a synonym.
	invalid := CapabilityLevel("VENDOR_ONLY")
	if invalid.Valid() {
		t.Error("CapabilityLevel(VENDOR_ONLY).Valid() = true, want false")
	}
	if err := ValidateCapabilityLevel(invalid); err == nil {
		t.Error("ValidateCapabilityLevel(VENDOR_ONLY) = nil, want error")
	}
}
