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

// TestGeneration_Valid_WithDesiredState_Golden proves the additive PR-14
// (issue #18) fields -- ontologyVersion, sourceDigest, desiredState,
// expectedEvidence, riskConfirmations, metadata.invocation -- round-trip and
// validate together, alongside every field PR-09 already established,
// without disturbing any of them (the same "additive schema evolution"
// discipline TestGeneration_Valid_WithSources_Golden already proved for
// PR-09's own Sources field).
func TestGeneration_Valid_WithDesiredState_Golden(t *testing.T) {
	var g Generation
	loadFixture(t, "generation-valid-with-desired-state.json", &g)

	if err := ValidateGeneration(g); err != nil {
		t.Fatalf("ValidateGeneration: %v", err)
	}
	if g.Metadata.Invocation != "omca run codex" {
		t.Errorf("metadata.invocation = %q, want %q", g.Metadata.Invocation, "omca run codex")
	}
	if g.Spec.OntologyVersion != CurrentOntologyVersion {
		t.Errorf("spec.ontologyVersion = %q, want %q", g.Spec.OntologyVersion, CurrentOntologyVersion)
	}
	if !IsCanonicalDigest(g.Spec.SourceDigest) {
		t.Errorf("spec.sourceDigest %q is not a canonical digest", g.Spec.SourceDigest)
	}
	if g.Spec.DesiredState == nil {
		t.Fatal("spec.desiredState is nil, want populated")
	}
	if len(g.Spec.DesiredState.Profiles) != 1 || g.Spec.DesiredState.Profiles[0].ID != "company:example" {
		t.Errorf("spec.desiredState.profiles = %+v, want one entry naming company:example", g.Spec.DesiredState.Profiles)
	}
	if len(g.Spec.ExpectedEvidence) != 1 || g.Spec.ExpectedEvidence[0].Host != "codex" {
		t.Errorf("spec.expectedEvidence = %+v, want one entry naming codex", g.Spec.ExpectedEvidence)
	}
}

// TestGeneration_Invalid_BadDesiredStateProfileDigest proves
// ValidateGeneration rejects a spec.desiredState.profiles entry whose digest
// is not a canonical sha256 digest -- the same "fail closed on a malformed
// digest reference" discipline every other digest field in this document
// already has.
func TestGeneration_Invalid_BadDesiredStateProfileDigest(t *testing.T) {
	var g Generation
	loadFixture(t, "generation-invalid-bad-desiredstate-digest.json", &g)

	err := ValidateGeneration(g)
	if err == nil {
		t.Fatal("expected an error for a malformed spec.desiredState.profiles digest, got nil")
	}
	if !strings.Contains(err.Error(), "desiredState") {
		t.Errorf("error = %q, want it to mention desiredState", err.Error())
	}
}
