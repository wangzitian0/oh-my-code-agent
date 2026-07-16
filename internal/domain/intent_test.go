package domain

import "testing"

func TestIntentValid(t *testing.T) {
	valid := []Intent{IntentRequired, IntentDefault, IntentAvailable, IntentDenied}
	for _, i := range valid {
		if !i.Valid() {
			t.Errorf("Intent(%q).Valid() = false, want true", i)
		}
		if err := ValidateIntent(i); err != nil {
			t.Errorf("ValidateIntent(%q) = %v, want nil", i, err)
		}
	}

	invalid := Intent("MAYBE")
	if invalid.Valid() {
		t.Error("Intent(MAYBE).Valid() = true, want false")
	}
	if err := ValidateIntent(invalid); err == nil {
		t.Error("ValidateIntent(MAYBE) = nil, want error")
	}
}
