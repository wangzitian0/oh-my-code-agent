package domain

import (
	"strings"
	"testing"
)

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

// TestGeneration_Valid_WithSources_Golden proves the additive spec.sources
// field (PR-09, issue #13 AC "The manifest lists every included and
// excluded source with a reason") round-trips and validates: an included
// entry, a plain excluded entry, and a capabilityGap entry with a
// trackingIssue all coexist on one Generation without disturbing any
// existing required field.
func TestGeneration_Valid_WithSources_Golden(t *testing.T) {
	var g Generation
	loadFixture(t, "generation-valid-with-sources.json", &g)

	if err := ValidateGeneration(g); err != nil {
		t.Fatalf("ValidateGeneration: %v", err)
	}
	if len(g.Spec.Sources) != 3 {
		t.Fatalf("sources = %d, want 3", len(g.Spec.Sources))
	}
	included := g.Spec.Sources[0]
	if !included.Included || included.Reason == "" {
		t.Errorf("sources[0] = %+v, want an included entry with a non-empty reason", included)
	}
	excluded := g.Spec.Sources[1]
	if excluded.Included || excluded.Reason == "" {
		t.Errorf("sources[1] = %+v, want an excluded entry with a non-empty reason", excluded)
	}
	gap := g.Spec.Sources[2]
	if !gap.CapabilityGap || gap.TrackingIssue == "" {
		t.Errorf("sources[2] = %+v, want a capability gap entry with a non-empty trackingIssue", gap)
	}
}

// TestGeneration_Invalid_CapabilityGapMissingTrackingIssue proves
// ValidateGeneration rejects capabilityGap:true without a trackingIssue --
// the Go-level enforcement of issue #13's round-2 policy, "capability-gap
// shipping is allowed, hiding is not." Reverting the corresponding check in
// ValidateGeneration (internal/domain/generation.go) makes this test fail
// (see this PR's commit message for the actual before/after proof).
func TestGeneration_Invalid_CapabilityGapMissingTrackingIssue(t *testing.T) {
	var g Generation
	loadFixture(t, "generation-invalid-capability-gap-missing-issue.json", &g)

	err := ValidateGeneration(g)
	if err == nil {
		t.Fatal("expected an error for capabilityGap:true with no trackingIssue, got nil")
	}
	if !strings.Contains(err.Error(), "trackingIssue") {
		t.Errorf("error = %q, want it to mention the missing trackingIssue", err.Error())
	}
}
