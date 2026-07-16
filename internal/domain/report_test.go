package domain

import "testing"

func TestReport_Valid_Golden(t *testing.T) {
	var r Report
	loadFixture(t, "report-valid.json", &r)

	if err := ValidateReport(r); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
	if len(r.Spec.Drift) != 1 {
		t.Fatalf("drift = %d, want 1", len(r.Spec.Drift))
	}
	if r.Spec.Drift[0].Category != DriftEffectiveDrift {
		t.Fatalf("drift[0].category = %q, want EFFECTIVE_DRIFT", r.Spec.Drift[0].Category)
	}
	if r.Spec.KnowledgeStatus["codex"] != KnowledgeFresh {
		t.Fatalf("knowledgeStatus[codex] = %q, want FRESH", r.Spec.KnowledgeStatus["codex"])
	}
}

func TestReport_Invalid_BadFingerprint(t *testing.T) {
	var r Report
	loadFixture(t, "report-invalid-bad-fingerprint.json", &r)

	if err := ValidateReport(r); err == nil {
		t.Fatal("expected an error for a malformed fingerprint, got nil")
	}
}

func TestReport_Invalid_BadDriftCategory(t *testing.T) {
	var r Report
	loadFixture(t, "report-invalid-bad-drift-category.json", &r)

	if err := ValidateReport(r); err == nil {
		t.Fatal("expected an error for an unknown drift category, got nil")
	}
}
