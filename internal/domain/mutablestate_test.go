package domain

import "testing"

func TestMutableStateClassValid(t *testing.T) {
	valid := []MutableStateClass{
		MutableStateGenerationLocal, MutableStateWorktreeShared, MutableStateWorkspaceShared,
		MutableStateIdentityShared, MutableStateHostGlobalExternal, MutableStateProhibitedImport,
	}
	// Enumerating by hand is what let a newly added class go untested, so
	// assert the hand-written list against the enum itself (Copilot review
	// finding on the PR that added workspace-shared).
	if len(valid) != len(mutableStateClasses) {
		t.Fatalf("this test enumerates %d classes but the enum defines %d; a class added without a case here would be validated by nothing", len(valid), len(mutableStateClasses))
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
	shared := []MutableStateClass{MutableStateWorktreeShared, MutableStateWorkspaceShared, MutableStateIdentityShared}
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
	// Together the two lists must account for the whole enum, so a future
	// class cannot be added without a deliberate decision about whether it
	// shares.
	if len(shared)+len(notShared) != len(mutableStateClasses) {
		t.Errorf("shared(%d) + notShared(%d) != %d defined classes; every class must be on exactly one side of this question", len(shared), len(notShared), len(mutableStateClasses))
	}
}
