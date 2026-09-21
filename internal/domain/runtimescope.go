package domain

import "fmt"

// RuntimeScope answers a question the model could not previously express:
// at which scope does one running instance of this runtime live?
//
// The two existing axes classify different things. docs/ontology/README.md
// §2's Scope Model classifies where a configuration SOURCE comes from.
// MutableStateClass classifies which isolated homes a piece of host-written
// STATE may be visible from. Neither says anything about processes, because
// for a long time there was only one process shape to say it about: the
// per-worktree generation.
//
// That stopped being true when a resident hub arrived (internal/hub): one
// daemon per OS user, brokering shared tool processes for every window the
// user has open. It is not a worktree runtime, and with no way to say so it
// read as a violation of init.md's Non-goals rather than as the first
// instance of a scope the model had simply never named.
//
// Naming it matters beyond bookkeeping. Per-worktree isolation is the
// product's whole point, and its direct consequence is that anything shared
// gets duplicated N times unless some runtime owns the shared scope. A
// 126 MB provider cache copied per checkout is that consequence, not an
// accident. So is a login that has to be repeated per checkout, since
// ADR 0003 forbids copying or symlinking credential material into a
// generation at all: the only place identity-scoped state can legitimately
// live is an identity-scoped runtime.
//
// Values are drawn from the Scope Model's own closed vocabulary rather than
// invented here, so the two axes stay in one language. The Scope Model is a
// graph and not a priority ladder; these are positions in it, not rungs.
type RuntimeScope string

const (
	// RuntimeScopeWorktree: one instance per Git worktree. The compiled
	// generation and its PATH shims -- everything docs/architecture/
	// runtime.md §5 describes -- and the default for anything whose whole
	// purpose is per-checkout isolation.
	RuntimeScopeWorktree RuntimeScope = "worktree"
	// RuntimeScopeWorkspace: one instance per declared set of workspace
	// roots, the grouping MutableStateWorkspaceShared shares across. Nothing
	// occupies this scope yet; it exists so that a runtime serving one
	// domain of repositories without serving every other domain the same OS
	// user works in has a name before it is built, rather than after.
	RuntimeScopeWorkspace RuntimeScope = "workspace"
	// RuntimeScopeUser: one instance per OS user, serving every worktree and
	// every window that user has open. The resident hub is this: its socket
	// and config are derived from the real home (internal/hub's
	// DefaultSocketPath/DefaultConfigPath), never from a worktree, and it
	// multiplexes profiles internally rather than by running more copies.
	//
	// A user-scope runtime is the only correct home for state classified
	// MutableStateIdentityShared, and the only way a per-worktree isolation
	// model can avoid paying for shared things once per checkout.
	RuntimeScopeUser RuntimeScope = "user"
)

// runtimeScopes is the closed set RuntimeScope.Valid checks against,
// matching mutableStateClasses' map-literal style in this package.
var runtimeScopes = map[RuntimeScope]bool{
	RuntimeScopeWorktree:  true,
	RuntimeScopeWorkspace: true,
	RuntimeScopeUser:      true,
}

// Valid reports whether r is one of the defined runtime scopes.
func (r RuntimeScope) Valid() bool {
	return runtimeScopes[r]
}

// ValidateRuntimeScope rejects any value outside the closed runtime-scope
// enum.
func ValidateRuntimeScope(r RuntimeScope) error {
	if !r.Valid() {
		return fmt.Errorf("invalid runtime scope %q", r)
	}
	return nil
}

// IsScopeKind reports whether r names a scope the Scope Model also defines
// (docs/ontology/README.md §2, KnownScopeKinds).
//
// This is the seam between the two axes, asserted rather than assumed: a
// runtime scope that the Scope Model does not recognize would mean the
// project had started describing process placement in a second, private
// vocabulary, which is exactly the drift naming this axis exists to avoid.
func (r RuntimeScope) IsScopeKind() bool {
	return KnownScopeKinds[string(r)]
}

// SharingClass returns the MutableStateClass whose visibility a runtime at
// this scope can legitimately own, and true when such a class exists.
//
// The pairing is the practical point of both enums: state may be shared no
// more widely than some runtime is there to serve it. Classifying a cache
// as workspace-shared while no workspace-scope runtime exists does not make
// it shared -- it makes the classification a claim nothing backs, the same
// defect as a report that overstates isolation.
func (r RuntimeScope) SharingClass() (MutableStateClass, bool) {
	switch r {
	case RuntimeScopeWorktree:
		return MutableStateWorktreeShared, true
	case RuntimeScopeWorkspace:
		return MutableStateWorkspaceShared, true
	case RuntimeScopeUser:
		return MutableStateIdentityShared, true
	default:
		return "", false
	}
}
