package domain

import "testing"

func TestObservation_Valid_Golden(t *testing.T) {
	var o Observation
	loadFixture(t, "observation-valid.json", &o)

	if err := ValidateObservation(o); err != nil {
		t.Fatalf("ValidateObservation: %v", err)
	}
	if o.Spec.Concept != "instruction" {
		t.Fatalf("concept = %q, want instruction", o.Spec.Concept)
	}
	if o.Spec.Disposition != DispositionActive {
		t.Fatalf("disposition = %q, want ACTIVE", o.Spec.Disposition)
	}
	if o.Spec.EvidenceLevel != EvidenceLevelResolved {
		t.Fatalf("evidenceLevel = %q, want E2", o.Spec.EvidenceLevel)
	}
}

func TestObservation_Invalid_UnknownHost(t *testing.T) {
	var o Observation
	loadFixture(t, "observation-invalid-unknown-host.json", &o)

	if err := ValidateObservation(o); err == nil {
		t.Fatal("expected an error for an unknown host id, got nil")
	}
}

func TestObservation_Invalid_BadDisposition(t *testing.T) {
	var o Observation
	loadFixture(t, "observation-invalid-bad-disposition.json", &o)

	if err := ValidateObservation(o); err == nil {
		t.Fatal("expected an error for an invalid disposition, got nil")
	}
}
