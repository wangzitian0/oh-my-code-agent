package domain

import "testing"

func TestRepairProposal_Valid_Golden(t *testing.T) {
	var rp RepairProposal
	loadFixture(t, "repairproposal-valid.json", &rp)

	if err := ValidateRepairProposal(rp); err != nil {
		t.Fatalf("ValidateRepairProposal: %v", err)
	}
	if rp.Spec.Confirmation != RepairAutoStage {
		t.Fatalf("confirmation = %q, want AUTO_STAGE", rp.Spec.Confirmation)
	}
	if err := ValidateRepairProposalAgainstReport(rp, rp.Spec.ReportFingerprint); err != nil {
		t.Fatalf("ValidateRepairProposalAgainstReport(matching): %v", err)
	}
	mismatched := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := ValidateRepairProposalAgainstReport(rp, mismatched); err == nil {
		t.Fatal("expected an error for a mismatched report fingerprint")
	}
}

func TestRepairProposal_Invalid_Prohibited(t *testing.T) {
	var rp RepairProposal
	loadFixture(t, "repairproposal-invalid-prohibited.json", &rp)

	err := ValidateRepairProposal(rp)
	if err == nil {
		t.Fatal("expected an error: a PROHIBITED proposal must be rejected outright")
	}
}

func TestRepairProposal_Invalid_LLMMissingModel(t *testing.T) {
	var rp RepairProposal
	loadFixture(t, "repairproposal-invalid-llm-missing-model.json", &rp)

	if err := ValidateRepairProposal(rp); err == nil {
		t.Fatal("expected an error: an llm author must name a model")
	}
}
