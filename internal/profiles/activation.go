package profiles

import (
	"fmt"
	"os"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// LoadActivation reads and validates the single Activation YAML document at
// path (docs/architecture/README.md §8:
// <worktree state dir>/desired/activation.yaml — worktree-local state, not
// repository configuration, per §7's "Personal identity selection and
// worktree activation are local state, not shared repository
// configuration").
//
// A path that does not exist is not an error: ok is false and the zero
// domain.Activation is returned, meaning "no worktree-specific choices have
// been recorded yet" — a perfectly normal state for a worktree that has
// never had `omca` activate anything locally. A present-but-invalid
// document is an error naming path and the offending field (issue #16 AC).
func LoadActivation(path string) (domain.Activation, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return domain.Activation{}, false, nil
		}
		return domain.Activation{}, false, fmt.Errorf("profiles: %s: %w", path, err)
	}

	var a domain.Activation
	if err := decodeYAMLDocument(path, &a); err != nil {
		return domain.Activation{}, false, err
	}
	if err := domain.ValidateActivation(a); err != nil {
		return domain.Activation{}, false, fmt.Errorf("profiles: %s: %w", path, err)
	}
	return a, true, nil
}
