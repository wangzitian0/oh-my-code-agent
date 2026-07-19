package domain

import "testing"

func validKnowledgeCandidate() KnowledgeCandidate {
	return KnowledgeCandidate{
		APIVersion: SupportedAPIVersion,
		Kind:       "KnowledgeCandidate",
		Metadata: KnowledgeCandidateMetadata{
			ID:          "candidate:codex:cli:2026-07-19T00:00:00Z",
			Host:        "codex",
			Surface:     "cli",
			CollectedAt: "2026-07-19T00:00:00Z",
			Automation:  "omca knowledge poll",
		},
		Spec: KnowledgeCandidateSpec{
			ChangedSources: []ChangedSource{
				{SourceID: "codex-cli-doc", Kind: "official-doc", URL: "https://learn.chatgpt.com/docs/codex/cli", OldDigest: "sha256:old", NewDigest: "sha256:new"},
			},
			VersionRange: VersionRangeChange{Old: ">=0.144.0 <0.145.0"},
			FixtureResults: []FixtureResult{
				{ID: "codex-skill-collision", Status: FixtureResultNotRun},
			},
			WriteCapabilityImpacts: []WriteCapabilityImpact{
				{Concept: "skill", Change: WriteCapabilityBlocked, Reason: "STALE Pack: no expansion of write behavior until re-qualified"},
			},
		},
	}
}

func TestKnowledgeCandidate_Valid(t *testing.T) {
	if err := ValidateKnowledgeCandidate(validKnowledgeCandidate()); err != nil {
		t.Fatalf("ValidateKnowledgeCandidate: %v", err)
	}
}

func TestKnowledgeCandidate_Invalid_NoChangedSources(t *testing.T) {
	kc := validKnowledgeCandidate()
	kc.Spec.ChangedSources = nil
	if err := ValidateKnowledgeCandidate(kc); err == nil {
		t.Fatal("want an error for a candidate with no changed sources")
	}
}

func TestKnowledgeCandidate_Invalid_ChangedSourceMissingNewDigest(t *testing.T) {
	kc := validKnowledgeCandidate()
	kc.Spec.ChangedSources[0].NewDigest = ""
	if err := ValidateKnowledgeCandidate(kc); err == nil {
		t.Fatal("want an error for a changed source with no newDigest")
	}
}

func TestKnowledgeCandidate_Invalid_MissingAutomation(t *testing.T) {
	kc := validKnowledgeCandidate()
	kc.Metadata.Automation = ""
	if err := ValidateKnowledgeCandidate(kc); err == nil {
		t.Fatal("want an error when metadata.automation is empty -- a candidate must identify the automation that produced it")
	}
}

func TestKnowledgeCandidate_Invalid_BadFixtureStatus(t *testing.T) {
	kc := validKnowledgeCandidate()
	kc.Spec.FixtureResults[0].Status = "MAYBE"
	if err := ValidateKnowledgeCandidate(kc); err == nil {
		t.Fatal("want an error for an invalid fixture result status")
	}
}

func TestKnowledgeCandidate_Invalid_BadWriteCapabilityChange(t *testing.T) {
	kc := validKnowledgeCandidate()
	kc.Spec.WriteCapabilityImpacts[0].Change = "MAYBE"
	if err := ValidateKnowledgeCandidate(kc); err == nil {
		t.Fatal("want an error for an invalid write capability change")
	}
}

func TestKnowledgeCandidate_Invalid_BadAPIVersion(t *testing.T) {
	kc := validKnowledgeCandidate()
	kc.APIVersion = "v0"
	if err := ValidateKnowledgeCandidate(kc); err == nil {
		t.Fatal("want an error for an unsupported apiVersion")
	}
}

func TestKnowledgeCandidate_Invalid_BadHost(t *testing.T) {
	kc := validKnowledgeCandidate()
	kc.Metadata.Host = "not-a-real-host"
	if err := ValidateKnowledgeCandidate(kc); err == nil {
		t.Fatal("want an error for an unrecognized host id")
	}
}
