package domain

import (
	"fmt"
	"time"
)

// Exception is a minimal, resolver-input shape for excepting one asset from
// the normal REQUIRED/DENIED enforcement rules (init.md "Activation Intent":
// REQUIRED "cannot be disabled without an explicit exception allowed by the
// defining policy"; DENIED "cannot be re-enabled by lower scopes" — an
// Exception is the one documented escape valve for both).
//
// This type originally (PR-13) defined only what the intent resolution
// engine (internal/resolve) needs to consume and test against: which asset
// it excepts, which defining policy scope allows the exception, a
// human-authored justification, and when it stops applying. PR-12 (#16,
// "Exception loading from files") extends it in place, per this comment's
// own prior instruction, rather than defining a second, possibly-conflicting
// shape: APIVersion, Kind, and Metadata give a file-loaded Exception the
// same on-disk document identity every other loadable desired-state kind
// has (Profile, Binding, Activation), so internal/profiles' loader can
// validate it and produce actionable file+field errors the same way.
//
// These identity fields are deliberately added as top-level siblings of the
// original AssetID/Scope/Justification/ExpiresAt fields, NOT nested inside a
// `Spec` sub-struct the way ProfileSpec/BindingSpec/ActivationSpec are:
// internal/resolve (already merged, frozen per PR-12's own instructions) and
// its tests construct and read Exception with those four fields at the top
// level throughout; nesting them now would force edits to that package's
// already-reviewed test literals for a purely cosmetic reshuffle, with zero
// change in behavior. A resolver-constructed Exception (as every
// internal/resolve test builds) simply leaves APIVersion/Kind/Metadata at
// their zero values, which ValidateException (the file-loading validator)
// rejects but Valid() (the resolver's lightweight structural check) does
// not — the two checks intentionally serve different callers.
//
// Scope identifies the defining policy the exception is scoped to: the
// Profile Metadata.ID that established the REQUIRED or DENIED intent being
// excepted. An Exception whose Scope does not match the defining Profile's ID
// does not apply — only "an exception the defining policy allows" counts.
type Exception struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`

	AssetID       string    `json:"assetId"`
	Scope         string    `json:"scope"`
	Justification string    `json:"justification"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// Valid reports whether e is well-formed enough to be consumed by the
// resolver: a non-empty AssetID and Scope. It does not evaluate expiry —
// that depends on a caller-supplied reference time, so it belongs to the
// resolver, not this structural check. It also does not require the
// file-identity fields ValidateException checks: a resolver-constructed
// Exception (built directly in Go, not loaded from a file) never sets them.
func (e Exception) Valid() bool {
	return e.AssetID != "" && e.Scope != ""
}

// ValidateException validates a file-loaded Exception document: apiVersion/
// kind, required metadata.id, and the four resolver-input fields
// (docs/architecture/README.md §7's exceptions/ directory; round-2 addendum
// to issue #16, "Exception documents load with scope, justification, and
// expiry; schema errors are actionable"). Unlike Valid(), this also requires
// Justification and a non-zero ExpiresAt: a file-authored Exception without
// a stated justification or an explicit expiry is a malformed document, not
// merely one the resolver happens to ignore.
func ValidateException(e Exception) error {
	if err := ValidateAPIVersion("Exception", e.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("Exception", e.Kind); err != nil {
		return err
	}
	if e.Metadata.ID == "" {
		return fmt.Errorf("Exception: metadata.id is required")
	}
	if e.AssetID == "" {
		return fmt.Errorf("Exception: assetId is required")
	}
	if e.Scope == "" {
		return fmt.Errorf("Exception: scope is required")
	}
	if e.Justification == "" {
		return fmt.Errorf("Exception: justification is required")
	}
	if e.ExpiresAt.IsZero() {
		return fmt.Errorf("Exception: expiresAt is required")
	}
	return nil
}
