package hub

import (
	"os"
	"path/filepath"
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

	// Assert what the paths ARE derived from, not what they merely lack.
	// An earlier version of this test grepped the strings for "worktree",
	// which a home directory containing that word would have failed for no
	// real reason, and which a worktree-derived path spelled differently
	// would have passed (Copilot review finding).
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}
	sock, cfg := DefaultSocketPath(), DefaultConfigPath()
	for name, p := range map[string]string{"socket": sock, "config": cfg} {
		if p == "" {
			t.Fatalf("default %s path is empty", name)
		}
		rel, relErr := filepath.Rel(home, p)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("%s path %q is not under the home directory, so it cannot be one-per-OS-user as the declared scope claims", name, p)
		}
	}

	// The decisive property: these are pure functions of the home directory,
	// so two different working directories produce the same paths. A
	// worktree-derived path could not do that.
	inRepo := t.TempDir()
	prevWD, wdErr := os.Getwd()
	if wdErr != nil {
		t.Fatalf("Getwd: %v", wdErr)
	}
	if chErr := os.Chdir(inRepo); chErr != nil {
		t.Fatalf("Chdir: %v", chErr)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if got := DefaultSocketPath(); got != sock {
		t.Errorf("socket path changed with the working directory (%q -> %q); a user-scope runtime must resolve to one socket regardless of where it is invoked from", sock, got)
	}
	if got := DefaultConfigPath(); got != cfg {
		t.Errorf("config path changed with the working directory (%q -> %q)", cfg, got)
	}

	// A user-scope runtime is the one that can own identity-shared state;
	// that pairing is what makes the declaration load-bearing rather than a
	// label.
	class, ok := Scope().SharingClass()
	if !ok || class != domain.MutableStateIdentityShared {
		t.Errorf("user scope should own %q, got (%q, %v)", domain.MutableStateIdentityShared, class, ok)
	}
}
