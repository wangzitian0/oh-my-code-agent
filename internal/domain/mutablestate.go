package domain

import "fmt"

// MutableStateClass is one of the six sharing classes
// docs/architecture/runtime.md §9 ("Mutable State") defines for host-written
// state that is not itself a compiled config artifact: sessions and archived
// sessions, logs and crash reports, SQLite databases, model/provider caches,
// trust decisions, memory, and installation metadata. Unlike Ownership
// (ADR 0002, adjacent but distinct: ownership answers "who is allowed to
// write this artifact", MutableStateClass answers "which isolated homes may
// this piece of host-written runtime state be visible from"), this is a new,
// small enum rather than a reuse of Ownership: runtime.md §9 spells these
// out with no 1:1 correspondence to Ownership's own five ("host-global
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
	// MutableStateWorkspaceShared: state shared across every worktree under
	// one declared set of workspace roots, but not across different sets and
	// not across identities.
	//
	// This is the class the model was missing, and its absence has a
	// measurable cost. A host's own model/provider cache is not per-checkout
	// by nature -- the same download serves every worktree the same person
	// opens -- but with only `worktree-shared` available it can be classified
	// no wider than one checkout. On a machine with several worktrees of the
	// same repository that means several full copies of the same recreatable
	// bytes, which is how one codex native home reached 126 MB of cache and
	// scratch state that nothing else on the machine could reuse.
	//
	// It is deliberately narrower than MutableStateIdentityShared: identity
	// scope answers "the same account, everywhere", which is the right home
	// for login state and the wrong one for a cache that should not follow a
	// person into an unrelated employer's checkouts. The concrete grouping
	// this class shares across is a declared root set -- the shape
	// internal/hub's `profiles.<name>.workspace_roots` already uses to
	// separate a personal domain from a corporate one.
	//
	// In docs/ontology/README.md §2's vocabulary this sits between the
	// `worktree` and `user` scopes, spanning the `workspace` scope's roots as
	// grouped by a profile. The Scope Model is a graph, not a ladder, so that
	// is a position in it rather than a rung.
	MutableStateWorkspaceShared MutableStateClass = "workspace-shared"
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
	MutableStateWorkspaceShared:    true,
	MutableStateIdentityShared:     true,
	MutableStateHostGlobalExternal: true,
	MutableStateProhibitedImport:   true,
}

// Valid reports whether m is one of the defined mutable-state classes.
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
// visible from more than one generation's own isolated home (worktree-,
// workspace- or identity-shared). generation-local, host-global external, and
// prohibited import all keep state out of every OTHER generation's isolated
// home by definition -- host-global external because it never enters
// isolation at all, prohibited import because entering isolation is
// forbidden outright, generation-local because it is scoped to exactly one.
// Callers (e.g. internal/auth's symlink-allowlist planner) use this to
// decide whether a class is even eligible to appear in a sharing allowlist.
func (m MutableStateClass) SharesAcrossGenerations() bool {
	return m == MutableStateWorktreeShared ||
		m == MutableStateWorkspaceShared ||
		m == MutableStateIdentityShared
}
