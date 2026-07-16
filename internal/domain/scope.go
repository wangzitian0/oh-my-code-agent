package domain

import "fmt"

// KnownScopeKinds are the canonical scopes from docs/ontology/README.md §2
// (Scope Model). Scopes form a graph, not a universal priority ladder: this
// registry only closes the vocabulary, it does not encode precedence.
var KnownScopeKinds = map[string]bool{
	"builtin":   true,
	"managed":   true,
	"user":      true,
	"profile":   true,
	"workspace": true,
	"worktree":  true,
	"directory": true,
	"local":     true,
	"session":   true,
}

// ValidateScopeKind rejects a scope.kind that does not name a canonical
// scope from the Scope Model.
func ValidateScopeKind(kind string) error {
	if !KnownScopeKinds[kind] {
		return fmt.Errorf("unknown scope kind %q", kind)
	}
	return nil
}
