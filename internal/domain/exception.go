package domain

import "time"

// Exception is a minimal, resolver-input shape for excepting one asset from
// the normal REQUIRED/DENIED enforcement rules (init.md "Activation Intent":
// REQUIRED "cannot be disabled without an explicit exception allowed by the
// defining policy"; DENIED "cannot be re-enabled by lower scopes" — an
// Exception is the one documented escape valve for both).
//
// This type intentionally defines only what the intent resolution engine
// (internal/resolve, roadmap PR-13) needs to consume and test against: which
// asset it excepts, which defining policy scope allows the exception, a
// human-authored justification, and when it stops applying. It is not a
// file-loading format — PR-12 (#16, "Exception loading from files") is a
// separate, not-yet-built roadmap item and should extend or reuse this type
// rather than defining a second, possibly-conflicting Exception shape.
//
// Scope identifies the defining policy the exception is scoped to: the
// Profile Metadata.ID that established the REQUIRED or DENIED intent being
// excepted. An Exception whose Scope does not match the defining Profile's ID
// does not apply — only "an exception the defining policy allows" counts.
type Exception struct {
	AssetID       string    `json:"assetId"`
	Scope         string    `json:"scope"`
	Justification string    `json:"justification"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// Valid reports whether e is well-formed enough to be consumed by the
// resolver: a non-empty AssetID and Scope. It does not evaluate expiry —
// that depends on a caller-supplied reference time, so it belongs to the
// resolver, not this structural check.
func (e Exception) Valid() bool {
	return e.AssetID != "" && e.Scope != ""
}
