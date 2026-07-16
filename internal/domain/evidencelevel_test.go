package domain

import "testing"

func TestEvidenceLevelValid(t *testing.T) {
	cases := []struct {
		level EvidenceLevel
		name  string
	}{
		{EvidenceLevelDiscovered, "DISCOVERED"},
		{EvidenceLevelParsed, "PARSED"},
		{EvidenceLevelResolved, "RESOLVED"},
		{EvidenceLevelHostReported, "HOST_REPORTED"},
		{EvidenceLevelBehaviorProbed, "BEHAVIOR_PROBED"},
		{EvidenceLevelExternallyProven, "EXTERNALLY_PROVEN"},
	}
	for _, c := range cases {
		if !c.level.Valid() {
			t.Errorf("EvidenceLevel(%q).Valid() = false, want true", c.level)
		}
		if err := ValidateEvidenceLevel(c.level); err != nil {
			t.Errorf("ValidateEvidenceLevel(%q) = %v, want nil", c.level, err)
		}
		if got := c.level.Name(); got != c.name {
			t.Errorf("EvidenceLevel(%q).Name() = %q, want %q", c.level, got, c.name)
		}
	}

	invalid := EvidenceLevel("E6")
	if invalid.Valid() {
		t.Error("EvidenceLevel(E6).Valid() = true, want false")
	}
	if err := ValidateEvidenceLevel(invalid); err == nil {
		t.Error("ValidateEvidenceLevel(E6) = nil, want error")
	}
	if got := invalid.Name(); got != "" {
		t.Errorf("EvidenceLevel(E6).Name() = %q, want empty", got)
	}

	// Reject the long-form name too: the wire value is the short code only.
	longForm := EvidenceLevel("RESOLVED")
	if longForm.Valid() {
		t.Error("EvidenceLevel(RESOLVED).Valid() = true, want false (long form is not the wire value)")
	}
}
