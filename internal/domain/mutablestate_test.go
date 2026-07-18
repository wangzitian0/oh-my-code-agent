package domain

import "testing"

func TestMutableStateClassValid(t *testing.T) {
	valid := []MutableStateClass{
		MutableStateGenerationLocal, MutableStateWorktreeShared, MutableStateIdentityShared,
		MutableStateHostGlobalExternal, MutableStateProhibitedImport,
	}
	for _, m := range valid {
		if !m.Valid() {
			t.Errorf("MutableStateClass(%q).Valid() = false, want true", m)
		}
		if err := ValidateMutableStateClass(m); err != nil {
			t.Errorf("ValidateMutableStateClass(%q) = %v, want nil", m, err)
		}
	}

	// Case matters, like Ownership: the doc spells these lowercase.
	invalid := MutableStateClass("GENERATION-LOCAL")
	if invalid.Valid() {
		t.Error("MutableStateClass(GENERATION-LOCAL).Valid() = true, want false (must be lowercase)")
	}
	if err := ValidateMutableStateClass(invalid); err == nil {
		t.Error("ValidateMutableStateClass(GENERATION-LOCAL) = nil, want error")
	}
	if err := ValidateMutableStateClass(MutableStateClass("bogus")); err == nil {
		t.Error("ValidateMutableStateClass(bogus) = nil, want error")
	}
}

func TestMutableStateClassSharesAcrossGenerations(t *testing.T) {
	shared := []MutableStateClass{MutableStateWorktreeShared, MutableStateIdentityShared}
	for _, m := range shared {
		if !m.SharesAcrossGenerations() {
			t.Errorf("%q.SharesAcrossGenerations() = false, want true", m)
		}
	}
	notShared := []MutableStateClass{MutableStateGenerationLocal, MutableStateHostGlobalExternal, MutableStateProhibitedImport}
	for _, m := range notShared {
		if m.SharesAcrossGenerations() {
			t.Errorf("%q.SharesAcrossGenerations() = true, want false", m)
		}
	}
}
