package domain

import "fmt"

// MutableStateClass is one of the five sharing classes
// docs/architecture/runtime.md §9 ("Mutable State") defines for host-written
// state that is not itself a compiled config artifact: sessions and archived
// sessions, logs and crash reports, SQLite databases, model/provider caches,
// trust decisions, memory, and installation metadata. Unlike Ownership
// (ADR 0002, adjacent but distinct: ownership answers "who is allowed to
// write this artifact", MutableStateClass answers "which isolated homes may
// this piece of host-written runtime state be visible from"), this is a new,
// small enum rather than a reuse of Ownership: runtime.md §9 spells out five
// values with no 1:1 correspondence to Ownership's five ("host-global
// external" is not the same concept as OwnershipExternal -- the former
// describes state that stays in the real native home and is never migrated
// into any isolated home at all, the latter describes an artifact/field
// OMCA never writes regardless of which home it lives in). Values are
// preserved lowercase verbatim from the doc, matching Ownership's own
// documented precedent for this package.
type MutableStateClass string

const (
	// MutableStateGenerationLocal: state scoped to exactly one generation's
	// own isolated home; never read or written by any other generation.
	// This is the conservative default for any state class this project has
	// not yet fixture-proven safe to share more broadly (docs/architecture/
	// runtime.md §12 invariant "unknown behavior cannot be promoted to
	// managed by an LLM" applies equally to promoting a state class to
	// shared without a fixture).
	MutableStateGenerationLocal MutableStateClass = "generation-local"
	// MutableStateWorktreeShared: state shared across every generation
	// compiled for the same Git worktree, but not across different
	// worktrees -- e.g. a recreatable, non-sensitive cache narrow enough to
	// be worth avoiding a re-fetch for.
	MutableStateWorktreeShared MutableStateClass = "worktree-shared"
	// MutableStateIdentityShared: state shared across every generation for
	// the same identity/account regardless of worktree. ADR 0003 decision
	// item 4 fixes this for Claude Code's account/OAuth state as a
	// non-negotiable constraint ("isolation must not force a fresh login for
	// every generation"); this class exists in the general vocabulary so
	// other identity-bound state (were a fixture ever to prove sharing it is
	// safe) has somewhere to be classified without inventing a new value.
	MutableStateIdentityShared MutableStateClass = "identity-shared"
	// MutableStateHostGlobalExternal: state that stays in the real,
	// unisolated native home and is never migrated into any isolated home at
	// all -- OMCA observes that it exists there, but a freshly compiled
	// isolated home simply does not have it (and, for state a host
	// regenerates fresh in an empty home, e.g. an installation id, is not
	// expected to). Distinct from prohibited import: this class covers state
	// with no plausible reason to ever cross into isolation in the first
	// place (docs/adr/0002-ownership.md's `external` -- "another authority
	// owns the state outright"), not state that is dangerous specifically
	// because it is a credential.
	MutableStateHostGlobalExternal MutableStateClass = "host-global external"
	// MutableStateProhibitedImport: state that must never be copied or
	// symlinked into any isolated home under any circumstance, regardless of
	// worktree or identity -- ADR 0003 decision item 3's auth.json/token
	// caches/keyrings/.ssh/cloud-credential prohibition, and any native
	// user-global file that mixes credential material with other state a
	// safe narrow extraction has not been fixture-proven for (see
	// internal/auth's classification table for concrete examples, e.g.
	// Claude Code's ~/.claude.json).
	MutableStateProhibitedImport MutableStateClass = "prohibited import"
)

// mutableStateClasses is the closed set MutableStateClass.Valid checks
// against, mirroring Ownership.Valid's map-literal style in this package.
var mutableStateClasses = map[MutableStateClass]bool{
	MutableStateGenerationLocal:    true,
	MutableStateWorktreeShared:     true,
	MutableStateIdentityShared:     true,
	MutableStateHostGlobalExternal: true,
	MutableStateProhibitedImport:   true,
}

// Valid reports whether m is one of the five defined mutable-state classes.
func (m MutableStateClass) Valid() bool {
	return mutableStateClasses[m]
}

// ValidateMutableStateClass rejects any value outside the closed
// mutable-state-class enum.
func ValidateMutableStateClass(m MutableStateClass) error {
	if !m.Valid() {
		return fmt.Errorf("invalid mutable state class %q", m)
	}
	return nil
}

// SharesAcrossGenerations reports whether m permits a piece of state to be
// visible from more than one generation's own isolated home (worktree-shared
// or identity-shared). generation-local, host-global external, and
// prohibited import all keep state out of every OTHER generation's isolated
// home by definition -- host-global external because it never enters
// isolation at all, prohibited import because entering isolation is
// forbidden outright, generation-local because it is scoped to exactly one.
// Callers (e.g. internal/auth's symlink-allowlist planner) use this to
// decide whether a class is even eligible to appear in a sharing allowlist.
func (m MutableStateClass) SharesAcrossGenerations() bool {
	return m == MutableStateWorktreeShared || m == MutableStateIdentityShared
}
