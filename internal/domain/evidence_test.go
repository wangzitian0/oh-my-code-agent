package domain

import "testing"

func TestEvidence_Valid_Golden(t *testing.T) {
	var e Evidence
	loadFixture(t, "evidence-valid.json", &e)

	if err := ValidateEvidence(e); err != nil {
		t.Fatalf("ValidateEvidence: %v", err)
	}
	if e.Spec.Level != EvidenceLevelResolved {
		t.Fatalf("level = %q, want E2", e.Spec.Level)
	}
	if e.Spec.Guarantee != GuaranteeReconciled {
		t.Fatalf("guarantee = %q, want RECONCILED", e.Spec.Guarantee)
	}
}

func TestEvidence_Invalid_BadLevel(t *testing.T) {
	var e Evidence
	loadFixture(t, "evidence-invalid-bad-level.json", &e)

	if err := ValidateEvidence(e); err == nil {
		t.Fatal("expected an error for an invalid evidence level, got nil")
	}
}

func TestEvidence_Invalid_MissingSubject(t *testing.T) {
	var e Evidence
	loadFixture(t, "evidence-invalid-missing-subject.json", &e)

	if err := ValidateEvidence(e); err == nil {
		t.Fatal("expected an error for a missing subject.logicalId, got nil")
	}
}

func TestStrongestEvidence(t *testing.T) {
	subject := EvidenceSubject{Concept: "instruction", LogicalID: "codex:instruction:AGENTS.md", Field: "precedence"}
	otherSubject := EvidenceSubject{Concept: "skill", LogicalID: "codex:skill:code-review", Field: "discovery"}

	records := []Evidence{
		{Spec: EvidenceSpec{Subject: subject, Level: EvidenceLevelDiscovered}},
		{Spec: EvidenceSpec{Subject: subject, Level: EvidenceLevelHostReported}},
		{Spec: EvidenceSpec{Subject: subject, Level: EvidenceLevelParsed}},
		// A stronger level for a *different* claim must never win: evidence
		// is monotonic only for the same subject (reporting.md §4).
		{Spec: EvidenceSpec{Subject: otherSubject, Level: EvidenceLevelExternallyProven}},
	}

	strongest, ok := StrongestEvidence(subject, records)
	if !ok {
		t.Fatal("expected a match")
	}
	if strongest.Spec.Level != EvidenceLevelHostReported {
		t.Fatalf("strongest level = %q, want E3 (the strongest record for THIS subject, ignoring the E5 for a different one)", strongest.Spec.Level)
	}

	if _, ok := StrongestEvidence(EvidenceSubject{Concept: "unknown", LogicalID: "nothing"}, records); ok {
		t.Fatal("expected no match for a subject with no records")
	}
}
