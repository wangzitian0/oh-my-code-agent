package domain

import "fmt"

// SupportedAPIVersion is the only apiVersion this build accepts for the
// desired-state kinds (Profile, Binding, Activation). A document declaring
// any other apiVersion is rejected rather than partially interpreted.
const SupportedAPIVersion = "omca.dev/v1alpha1"

// ValidateAPIVersion rejects any apiVersion this build does not know how to
// interpret. Unknown versions fail closed instead of being guessed at.
func ValidateAPIVersion(kind, apiVersion string) error {
	if apiVersion != SupportedAPIVersion {
		return fmt.Errorf("%s: unsupported apiVersion %q, this build only accepts %q", kind, apiVersion, SupportedAPIVersion)
	}
	return nil
}

// ValidateKind rejects a document whose kind field does not match the kind
// its schema-specific validator expects.
func ValidateKind(want, got string) error {
	if got != want {
		return fmt.Errorf("expected kind %q, got %q", want, got)
	}
	return nil
}
