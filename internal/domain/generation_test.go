package domain

import "testing"

func TestGeneration_Valid_Golden(t *testing.T) {
	var g Generation
	loadFixture(t, "generation-valid.json", &g)

	if err := ValidateGeneration(g); err != nil {
		t.Fatalf("ValidateGeneration: %v", err)
	}
	codex, ok := g.Spec.Hosts["codex"]
	if !ok {
		t.Fatal("expected a codex host entry")
	}
	if codex.Ownership != OwnershipManaged {
		t.Fatalf("codex.ownership = %q, want managed", codex.Ownership)
	}
	if g.Metadata.Parent != nil {
		t.Fatalf("parent = %v, want nil for a bootstrap generation", g.Metadata.Parent)
	}
}

func TestGeneration_Invalid_BadDigest(t *testing.T) {
	var g Generation
	loadFixture(t, "generation-invalid-bad-digest.json", &g)

	if err := ValidateGeneration(g); err == nil {
		t.Fatal("expected an error for a malformed desiredGraphDigest, got nil")
	}
}

func TestGeneration_Invalid_BadOwnership(t *testing.T) {
	var g Generation
	loadFixture(t, "generation-invalid-bad-ownership.json", &g)

	if err := ValidateGeneration(g); err == nil {
		t.Fatal("expected an error for an invalid ownership value, got nil")
	}
}
