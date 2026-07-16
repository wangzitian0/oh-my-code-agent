package plugin

import "testing"

func TestCapabilityValid(t *testing.T) {
	valid := []Capability{
		CapabilityExact, CapabilityCompatible, CapabilityPartial,
		CapabilityOpaque, CapabilityUnknown, CapabilityUnsupported,
	}
	for _, c := range valid {
		if !c.Valid() {
			t.Errorf("Capability(%q).Valid() = false, want true", c)
		}
		if err := ValidateCapability(c); err != nil {
			t.Errorf("ValidateCapability(%q) = %v, want nil", c, err)
		}
	}

	invalid := Capability("SORT_OF")
	if invalid.Valid() {
		t.Error("Capability(SORT_OF).Valid() = true, want false")
	}
	if err := ValidateCapability(invalid); err == nil {
		t.Error("ValidateCapability(SORT_OF) = nil, want error")
	}
}
