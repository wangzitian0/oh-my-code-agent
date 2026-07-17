package domain

import (
	"testing"
	"time"
)

func TestExceptionValid(t *testing.T) {
	valid := Exception{AssetID: "release-production", Scope: "company:example", ExpiresAt: time.Now()}
	if !valid.Valid() {
		t.Error("Exception with AssetID and Scope set should be Valid()")
	}

	missingAsset := Exception{Scope: "company:example"}
	if missingAsset.Valid() {
		t.Error("Exception without AssetID should not be Valid()")
	}

	missingScope := Exception{AssetID: "release-production"}
	if missingScope.Valid() {
		t.Error("Exception without Scope should not be Valid()")
	}
}

func baseException() Exception {
	return Exception{
		APIVersion:    SupportedAPIVersion,
		Kind:          "Exception",
		Metadata:      Metadata{ID: "exception:release-production"},
		AssetID:       "release-production",
		Scope:         "company:example",
		Justification: "temporary rollout allowance approved by policy owner",
		ExpiresAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestValidateException_RequiredFields(t *testing.T) {
	base := baseException()
	if err := ValidateException(base); err != nil {
		t.Fatalf("baseline exception should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Exception)
	}{
		{"bad apiVersion", func(e *Exception) { e.APIVersion = "omca.dev/v2" }},
		{"bad kind", func(e *Exception) { e.Kind = "Something" }},
		{"missing metadata.id", func(e *Exception) { e.Metadata = Metadata{} }},
		{"missing assetId", func(e *Exception) { e.AssetID = "" }},
		{"missing scope", func(e *Exception) { e.Scope = "" }},
		{"missing justification", func(e *Exception) { e.Justification = "" }},
		{"missing expiresAt", func(e *Exception) { e.ExpiresAt = time.Time{} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := base
			c.mutate(&e)
			if err := ValidateException(e); err == nil {
				t.Errorf("expected an error for %s", c.name)
			}
		})
	}
}

// TestValidateException_ResolverConstructedExceptionFailsFileValidation
// proves the two checks intentionally diverge: every internal/resolve test
// builds an Exception with only the four resolver-input fields set (see
// this file's own TestExceptionValid and internal/resolve/resolve_test.go),
// which Valid() accepts but ValidateException must reject for lacking file
// identity — the loader's job, not the resolver's.
func TestValidateException_ResolverConstructedExceptionFailsFileValidation(t *testing.T) {
	resolverBuilt := Exception{
		AssetID:       "banned-tool",
		Scope:         "company:policy",
		Justification: "temporary",
		ExpiresAt:     time.Now(),
	}
	if !resolverBuilt.Valid() {
		t.Fatal("resolver-constructed exception should satisfy Valid()")
	}
	if err := ValidateException(resolverBuilt); err == nil {
		t.Error("resolver-constructed exception (no apiVersion/kind/metadata) should fail ValidateException")
	}
}
