package domain

import "testing"

// TestRuntimeScope_ValuesAreScopeModelKinds asserts the seam between the two
// axes instead of assuming it.
//
// RuntimeScope draws its values from docs/ontology/README.md §2's closed
// vocabulary so that process placement and configuration sourcing are
// described in one language. A value the Scope Model does not recognize
// would mean a second, private vocabulary had started growing -- the drift
// naming this axis exists to avoid.
func TestRuntimeScope_ValuesAreScopeModelKinds(t *testing.T) {
	for r := range runtimeScopes {
		if !r.IsScopeKind() {
			t.Errorf("RuntimeScope %q is not a Scope Model kind; process placement must stay in the Scope Model's vocabulary, not a parallel one", r)
		}
	}
}

// TestRuntimeScope_PairsWithASharingClass is the practical invariant: state
// may be shared no more widely than some runtime is there to serve it.
//
// Classifying a cache as workspace-shared while no workspace-scope runtime
// exists does not make it shared; it makes the classification a claim
// nothing backs. Every sharing class must therefore have exactly one
// runtime scope that can own it, and vice versa.
func TestRuntimeScope_PairsWithASharingClass(t *testing.T) {
	seen := map[MutableStateClass]RuntimeScope{}
	for r := range runtimeScopes {
		class, ok := r.SharingClass()
		if !ok {
			t.Errorf("runtime scope %q owns no sharing class; state at this scope would have nowhere to be classified", r)
			continue
		}
		if !class.Valid() {
			t.Errorf("runtime scope %q maps to %q, which is not a defined MutableStateClass", r, class)
		}
		if prev, dup := seen[class]; dup {
			t.Errorf("sharing class %q is owned by both %q and %q; the mapping must be one-to-one or 'which runtime serves this state' is ambiguous", class, prev, r)
		}
		seen[class] = r
	}

	// The reverse direction: every class that shares across generations must
	// have a runtime scope to serve it, or it is unimplementable by
	// construction.
	for _, class := range []MutableStateClass{
		MutableStateWorktreeShared, MutableStateWorkspaceShared, MutableStateIdentityShared,
	} {
		if !class.SharesAcrossGenerations() {
			t.Errorf("%q should share across generations", class)
		}
		if _, ok := seen[class]; !ok {
			t.Errorf("sharing class %q has no runtime scope that can own it; nothing would be able to serve state classified this way", class)
		}
	}
}

// TestRuntimeScope_RejectsUnknown keeps the enum closed, matching
// ValidateMutableStateClass's own discipline.
func TestRuntimeScope_RejectsUnknown(t *testing.T) {
	if err := ValidateRuntimeScope("machine"); err == nil {
		t.Error("ValidateRuntimeScope accepted a value outside the closed enum")
	}
	for r := range runtimeScopes {
		if err := ValidateRuntimeScope(r); err != nil {
			t.Errorf("ValidateRuntimeScope(%q) = %v, want nil", r, err)
		}
	}
}
