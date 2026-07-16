package domain

import "testing"

func TestValidateAPIVersion(t *testing.T) {
	if err := ValidateAPIVersion("Profile", SupportedAPIVersion); err != nil {
		t.Fatalf("unexpected error for supported version: %v", err)
	}
	if err := ValidateAPIVersion("Profile", "omca.dev/v2alpha1"); err == nil {
		t.Fatal("expected an error for an unknown apiVersion")
	}
	if err := ValidateAPIVersion("Profile", ""); err == nil {
		t.Fatal("expected an error for an empty apiVersion")
	}
}

func TestValidateKind(t *testing.T) {
	if err := ValidateKind("Profile", "Profile"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateKind("Profile", "Binding"); err == nil {
		t.Fatal("expected an error for a mismatched kind")
	}
}
