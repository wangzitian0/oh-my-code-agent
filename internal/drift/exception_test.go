package drift

import (
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestClassify_ExceptionExpiryTransition is the round-2-audit-added AC:
// "an authorized, unexpired exception reports as EXCEPTION; an expired one
// reverts to its underlying drift class (test)." It classifies the exact
// same Signal at two reference instants that straddle one Exception's
// ExpiresAt and asserts the classification transitions explicitly, not as
// an implicit side effect.
func TestClassify_ExceptionExpiryTransition(t *testing.T) {
	sig := Signal{
		EntityID: "asset:sandbox-mode", Field: "value",
		Category: domain.DriftConfigDrift,
		Expected: "workspace-write", Observed: "danger-full-access",
		RootCause: "company:example/security-default", Remediation: "rebuild artifact",
		Project: "infra2", Host: "codex",
		AssetID:       "sandbox-mode",
		EvidenceLevel: domain.EvidenceLevelHostReported, Guarantee: domain.GuaranteeReconciled,
	}

	expiresAt := fixedNow.Add(1 * time.Hour)
	exceptions := []domain.Exception{
		{
			APIVersion: domain.SupportedAPIVersion, Kind: "Exception",
			Metadata:      domain.Metadata{ID: "exception:sandbox-mode-2026"},
			AssetID:       "sandbox-mode",
			Scope:         "company:example/security-default",
			Justification: "vetted CI runner needs full access",
			ExpiresAt:     expiresAt,
		},
	}

	// Before expiry: authorized and unexpired -> EXCEPTION.
	before, ok, err := Classify(sig, exceptions, expiresAt.Add(-1*time.Minute))
	if err != nil || !ok {
		t.Fatalf("Classify (before expiry): ok=%v err=%v", ok, err)
	}
	if before.Category != domain.DriftException {
		t.Errorf("before expiry: Category = %q, want %q", before.Category, domain.DriftException)
	}
	if before.UnderlyingCategory != domain.DriftConfigDrift {
		t.Errorf("before expiry: UnderlyingCategory = %q, want %q", before.UnderlyingCategory, domain.DriftConfigDrift)
	}
	if before.ExceptionRef != "exception:sandbox-mode-2026" {
		t.Errorf("before expiry: ExceptionRef = %q, want the exception's metadata.id", before.ExceptionRef)
	}

	// Exactly at ExpiresAt: no longer strictly before it -> already expired.
	atExpiry, ok, err := Classify(sig, exceptions, expiresAt)
	if err != nil || !ok {
		t.Fatalf("Classify (at expiry): ok=%v err=%v", ok, err)
	}
	if atExpiry.Category != domain.DriftConfigDrift {
		t.Errorf("at expiry: Category = %q, want reverted %q", atExpiry.Category, domain.DriftConfigDrift)
	}
	if atExpiry.ExceptionRef != "" {
		t.Errorf("at expiry: ExceptionRef = %q, want empty", atExpiry.ExceptionRef)
	}

	// After expiry: reverts to the underlying drift class.
	after, ok, err := Classify(sig, exceptions, expiresAt.Add(1*time.Hour))
	if err != nil || !ok {
		t.Fatalf("Classify (after expiry): ok=%v err=%v", ok, err)
	}
	if after.Category != domain.DriftConfigDrift {
		t.Errorf("after expiry: Category = %q, want reverted %q", after.Category, domain.DriftConfigDrift)
	}
	if after.UnderlyingCategory != domain.DriftConfigDrift {
		t.Errorf("after expiry: UnderlyingCategory = %q, want %q", after.UnderlyingCategory, domain.DriftConfigDrift)
	}
	if after.ExceptionRef != "" {
		t.Errorf("after expiry: ExceptionRef = %q, want empty (reverted, no exception applies)", after.ExceptionRef)
	}
}

// TestClassify_ExceptionScopeMustMatch proves an Exception for the right
// AssetID but the wrong Scope does not except the signal — only "an
// exception the defining policy allows" counts (internal/domain/
// exception.go's doc comment), mirroring internal/resolve's same rule.
func TestClassify_ExceptionScopeMustMatch(t *testing.T) {
	sig := Signal{
		EntityID: "asset:sandbox-mode", Field: "value",
		Category: domain.DriftConfigDrift,
		Expected: "workspace-write", Observed: "danger-full-access",
		RootCause: "company:example/security-default",
		Project:   "infra2", Host: "codex",
		AssetID: "sandbox-mode",
	}
	exceptions := []domain.Exception{
		{
			APIVersion: domain.SupportedAPIVersion, Kind: "Exception",
			Metadata:      domain.Metadata{ID: "exception:wrong-scope"},
			AssetID:       "sandbox-mode",
			Scope:         "team:some-other-policy",
			Justification: "unrelated",
			ExpiresAt:     fixedNow.Add(1 * time.Hour),
		},
	}
	a, ok, err := Classify(sig, exceptions, fixedNow)
	if err != nil || !ok {
		t.Fatalf("Classify: ok=%v err=%v", ok, err)
	}
	if a.Category != domain.DriftConfigDrift {
		t.Errorf("Category = %q, want %q (scope mismatch must not except)", a.Category, domain.DriftConfigDrift)
	}
}

// TestClassify_UnknownCannotBeExcepted proves an explicit UNKNOWN signal is
// never overridden to EXCEPTION even if an Exception would otherwise match:
// the engine cannot authorize excepting a difference it could not classify
// in the first place.
func TestClassify_UnknownCannotBeExcepted(t *testing.T) {
	sig := Signal{
		EntityID: "asset:mystery", Field: "value",
		Category:  domain.DriftUnknown,
		RootCause: "company:example/security-default",
		Project:   "infra2", Host: "codex",
		AssetID: "mystery",
	}
	exceptions := []domain.Exception{
		{
			APIVersion: domain.SupportedAPIVersion, Kind: "Exception",
			Metadata:      domain.Metadata{ID: "exception:mystery"},
			AssetID:       "mystery",
			Scope:         "company:example/security-default",
			Justification: "n/a",
			ExpiresAt:     fixedNow.Add(1 * time.Hour),
		},
	}
	a, ok, err := Classify(sig, exceptions, fixedNow)
	if err != nil || !ok {
		t.Fatalf("Classify: ok=%v err=%v", ok, err)
	}
	if a.Category != domain.DriftUnknown {
		t.Errorf("Category = %q, want %q", a.Category, domain.DriftUnknown)
	}
}
