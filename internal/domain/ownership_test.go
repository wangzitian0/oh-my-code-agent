package domain

import "testing"

func TestOwnershipValid(t *testing.T) {
	valid := []Ownership{
		OwnershipManaged, OwnershipPatched, OwnershipObserved,
		OwnershipPassthrough, OwnershipExternal,
	}
	for _, o := range valid {
		if !o.Valid() {
			t.Errorf("Ownership(%q).Valid() = false, want true", o)
		}
		if err := ValidateOwnership(o); err != nil {
			t.Errorf("ValidateOwnership(%q) = %v, want nil", o, err)
		}
	}

	// Case matters: the doc spells these lowercase.
	invalid := Ownership("MANAGED")
	if invalid.Valid() {
		t.Error("Ownership(MANAGED).Valid() = true, want false (must be lowercase)")
	}
	if err := ValidateOwnership(invalid); err == nil {
		t.Error("ValidateOwnership(MANAGED) = nil, want error")
	}
}
