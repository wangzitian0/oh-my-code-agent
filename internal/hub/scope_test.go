package hub

import (
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestScope_MatchesHowThePathsAreDerived keeps the declaration honest.
//
// A hub that declared user scope while deriving its socket from a worktree
// would be documentation, not a fact. The paths are the evidence: both come
// from the home directory, so one daemon serves every worktree.
func TestScope_MatchesHowThePathsAreDerived(t *testing.T) {
	if got := Scope(); got != domain.RuntimeScopeUser {
		t.Fatalf("Scope() = %q, want %q", got, domain.RuntimeScopeUser)
	}
	if err := domain.ValidateRuntimeScope(Scope()); err != nil {
		t.Fatalf("Scope() is not a valid runtime scope: %v", err)
	}

	sock, cfg := DefaultSocketPath(), DefaultConfigPath()
	for name, p := range map[string]string{"socket": sock, "config": cfg} {
		if p == "" {
			t.Fatalf("default %s path is empty", name)
		}
		if strings.Contains(p, "worktree") || strings.Contains(p, "generations") {
			t.Errorf("%s path %q is derived from a worktree, contradicting the declared user scope", name, p)
		}
	}

	// A user-scope runtime is the one that can own identity-shared state;
	// that pairing is what makes the declaration load-bearing rather than a
	// label.
	class, ok := Scope().SharingClass()
	if !ok || class != domain.MutableStateIdentityShared {
		t.Errorf("user scope should own %q, got (%q, %v)", domain.MutableStateIdentityShared, class, ok)
	}
}
