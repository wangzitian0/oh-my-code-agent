package domain

import "testing"

func TestGuaranteeLevelValid(t *testing.T) {
	valid := []GuaranteeLevel{GuaranteeHard, GuaranteeReconciled, GuaranteeAdvisory, GuaranteeObserved}
	for _, g := range valid {
		if !g.Valid() {
			t.Errorf("GuaranteeLevel(%q).Valid() = false, want true", g)
		}
		if err := ValidateGuaranteeLevel(g); err != nil {
			t.Errorf("ValidateGuaranteeLevel(%q) = %v, want nil", g, err)
		}
	}

	invalid := GuaranteeLevel("MAYBE")
	if invalid.Valid() {
		t.Error("GuaranteeLevel(MAYBE).Valid() = true, want false")
	}
	if err := ValidateGuaranteeLevel(invalid); err == nil {
		t.Error("ValidateGuaranteeLevel(MAYBE) = nil, want error")
	}
}
