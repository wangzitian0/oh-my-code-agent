package qualify

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func sampleCaseOutput() CaseOutput {
	return CaseOutput{
		Manifest: InvocationManifest{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       invocationKind,
			Host:       "codex",
			Surface:    "cli",
			Version:    "0.144.5",
			Cwd:        "project",
			Invoke:     InvokeSpec{Attempted: false, Reason: "no safe path"},
			ObservationRules: []ObservationRule{
				{Root: "home", Concept: "instruction", Scope: "user", Surface: "cli"},
			},
		},
		Observations: []domain.Observation{
			{
				APIVersion: domain.SupportedAPIVersion,
				Kind:       "Observation",
				Metadata:   domain.Metadata{ID: "codex:instruction:home/AGENTS.md"},
				Spec: domain.ObservationSpec{
					Host:          domain.ObservationHost{ID: "codex", Version: "0.144.5"},
					Concept:       "instruction",
					Source:        domain.ObservationSource{Kind: "file", Path: "home/AGENTS.md", Digest: "sha256:abc"},
					Scope:         domain.ObservationScope{Kind: "user", Root: "home"},
					Disposition:   domain.DispositionDiscovered,
					EvidenceLevel: domain.EvidenceLevelParsed,
				},
			},
		},
		Effective: ExpectedEffectiveDocument{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       effectiveKind,
			Host:       domain.ObservationHost{ID: "codex", Version: "0.144.5"},
			Concept:    "instruction",
			Entries: []ExpectedEffectiveEntry{
				{
					LogicalID:      "x",
					MergeOperator:  "CONCAT_ORDERED",
					SelectedSource: Unknown,
					Guarantee:      domain.GuaranteeAdvisory,
					EvidenceLevel:  domain.EvidenceLevelParsed,
					Reason:         "unproven",
				},
			},
		},
	}
}

func TestCaseOutputDigestIsDeterministic(t *testing.T) {
	a := sampleCaseOutput()
	b := sampleCaseOutput()

	da, err := a.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	db, err := b.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if da != db {
		t.Errorf("Digest() = %q and %q for identical inputs, want equal", da, db)
	}
	if !domain.IsCanonicalDigest(da) {
		t.Errorf("Digest() = %q, not a canonical sha256 digest", da)
	}
}

func TestCaseOutputDigestChangesWithContent(t *testing.T) {
	a := sampleCaseOutput()
	b := sampleCaseOutput()
	b.Observations[0].Spec.Source.Digest = "sha256:different"

	da, _ := a.Digest()
	db, _ := b.Digest()
	if da == db {
		t.Error("Digest() unchanged after content changed, want different digests")
	}
}
