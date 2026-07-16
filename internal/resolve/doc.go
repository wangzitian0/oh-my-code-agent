// Package resolve computes the fully-resolved per-host desired state from an
// already-selected set of domain.Profile values, a domain.Activation, and
// domain.Exception values, for exactly one target host.
//
// Resolve is the intent resolution engine referenced by
// internal/domain/activation.go's doc comment: Profile/Activation validation
// in the domain package checks each document in isolation (and, for
// Activation, the single direct-scope DENIED cross-check in
// ValidateActivationAgainstProfiles); this package performs the full
// cross-host, cross-scope REQUIRED/DEFAULT/AVAILABLE/DENIED resolution,
// including hosts-selector refinement and Exception handling.
//
// See the Resolve doc comment for the precedence algorithm.
package resolve
