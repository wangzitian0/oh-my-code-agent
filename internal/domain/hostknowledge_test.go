package domain

import "testing"

func TestHostKnowledge_Valid_Golden(t *testing.T) {
	var hk HostKnowledge
	loadFixture(t, "hostknowledge-valid.json", &hk)

	if err := ValidateHostKnowledge(hk); err != nil {
		t.Fatalf("ValidateHostKnowledge: %v", err)
	}
	if hk.Metadata.Status != KnowledgeFresh {
		t.Fatalf("status = %q, want FRESH", hk.Metadata.Status)
	}
	if len(hk.Evidence) != 2 {
		t.Fatalf("evidence = %d, want 2", len(hk.Evidence))
	}
	skill, ok := hk.Capabilities["skill"]
	if !ok {
		t.Fatal("expected a skill capability entry")
	}
	if skill.Discover != CapabilityExact {
		t.Fatalf("skill.discover = %q, want EXACT", skill.Discover)
	}
}

func TestHostKnowledge_Invalid_BadStatus(t *testing.T) {
	var hk HostKnowledge
	loadFixture(t, "hostknowledge-invalid-bad-status.json", &hk)

	if err := ValidateHostKnowledge(hk); err == nil {
		t.Fatal("expected an error for an invalid lifecycle status, got nil")
	}
}

func TestHostKnowledge_Invalid_NoEvidence(t *testing.T) {
	var hk HostKnowledge
	loadFixture(t, "hostknowledge-invalid-no-evidence.json", &hk)

	if err := ValidateHostKnowledge(hk); err == nil {
		t.Fatal("expected an error for an empty evidence list, got nil")
	}
}
