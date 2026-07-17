package profiles

import (
	"fmt"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// CompositionInput bundles everything Compose needs to load and compose one
// worktree's desired-state inputs: the repository/path identity to match
// Bindings against (docs/product/requirements.md §4.2), the document
// directories to load Profiles/Bindings/Exceptions from (docs/architecture/
// README.md §7's `~/.config/omca/...` and `<repository>/.omca/...`
// layout), the worktree state paths to load Activation from and persist/
// read an identity selection at (§8's `<worktree state dir>/desired/...`
// layout), and the reference instant for Exception expiry.
//
// Every path/directory field is caller-supplied and never resolved
// internally, matching this project's existing discipline (cmd/omca/
// statedir.go, internal/runtime.EnsureGeneration's generationsRoot) of
// keeping real XDG/state path resolution in the command layer only.
type CompositionInput struct {
	// Repository and RelPath identify the context Bindings are matched
	// against (MatchBindings). RelPath uses "/" separators and "" for the
	// repository root.
	Repository string
	RelPath    string

	// ProfileDirs, BindingDirs, and ExceptionDirs are every directory to
	// load the respective document kind from, both the user config layout
	// (~/.config/omca/profiles/{personal,company,team,task}/,
	// ~/.config/omca/bindings/, ~/.config/omca/exceptions/) and the
	// repository layout (<repository>/.omca/profiles/, .omca/policies/ or
	// wherever a caller's Binding documents live, <repository>/.omca/
	// exceptions/). A missing directory is silently skipped (LoadProfiles/
	// LoadBindings/LoadExceptions).
	ProfileDirs   []string
	BindingDirs   []string
	ExceptionDirs []string

	// ActivationPath is the single Activation document's path
	// (<worktree state dir>/desired/activation.yaml). A missing file means
	// "no worktree-specific choices recorded yet", not an error.
	ActivationPath string

	// WorktreeStateDir is the worktree's state root
	// (<worktree state dir>, i.e. one level above desired/), used to read a
	// previously persisted identity selection (ReadSelection). It is never
	// a path under the repository checkout.
	WorktreeStateDir string

	// Now is the reference instant for Exception expiry (LoadExceptions)
	// and is threaded through unchanged, the same explicit-clock discipline
	// resolve.Resolve itself uses — Compose is not itself a pure function
	// (it reads the filesystem), but nothing it does needs to call
	// time.Now() internally as long as this is supplied.
	Now time.Time
}

// CompositionResult is the []domain.Profile + domain.Activation +
// []domain.Exception set ready to hand to resolve.Resolve, once per host
// (see internal/resolve.Resolve's signature). ExpiredExceptions is
// additionally reported — even though those Exceptions never reach
// Resolve — so a report/doctor-style consumer can surface "this exception
// expired and its asset is enforced again" (issue #16 round-2 AC).
type CompositionResult struct {
	Profiles          []domain.Profile
	Activation        domain.Activation
	Exceptions        []domain.Exception
	ExpiredExceptions []domain.Exception
}

// AmbiguousIdentityError reports that Compose could not proceed because one
// or more identity categories have multiple plausible Profiles and no
// (or no complete/valid) selection has been persisted yet. A caller — a
// future CLI prompt, or this package's own test — is expected to present
// Ambiguous, obtain an explicit choice per category, call PersistSelection,
// and call Compose again.
type AmbiguousIdentityError struct {
	Ambiguous []AmbiguousCategory
}

func (e *AmbiguousIdentityError) Error() string {
	return fmt.Sprintf("profiles: %d identity categories are ambiguous and need an explicit selection", len(e.Ambiguous))
}

// Compose loads every desired-state document input names, resolves which
// Profiles apply to the worktree (Binding matching, then identity
// resolution against any previously persisted selection), and returns the
// []domain.Profile + domain.Activation + []domain.Exception set ready to
// pass to resolve.Resolve once per host.
//
// Compose does not call resolve.Resolve itself, and does not implement
// REQUIRED/DEFAULT/AVAILABLE/DENIED precedence: that is internal/resolve's
// job (already built, PR-13). Compose's job stops at handing resolve.Resolve
// its inputs.
//
// If Binding matching leaves more than one plausible Profile for some
// identity category and no valid selection is available (nothing persisted
// yet, or a persisted selection that no longer names a real candidate),
// Compose returns *AmbiguousIdentityError instead of guessing (docs/
// product/requirements.md §5.1). The caller is expected to resolve the
// ambiguity (e.g. via a future CLI prompt), call PersistSelection, and call
// Compose again.
func Compose(input CompositionInput) (CompositionResult, error) {
	allProfiles, err := LoadProfiles(input.ProfileDirs)
	if err != nil {
		return CompositionResult{}, err
	}
	bindings, err := LoadBindings(input.BindingDirs)
	if err != nil {
		return CompositionResult{}, err
	}
	matched := MatchBindings(bindings, input.Repository, input.RelPath)
	matchedIDs := MatchedProfileIDs(matched)

	resolution := ResolveIdentities(matchedIDs)

	selection, _, err := ReadSelection(input.WorktreeStateDir)
	if err != nil {
		return CompositionResult{}, err
	}

	finalIDs, err := FinalProfileIDs(resolution, selection)
	if err != nil {
		if resolution.IsAmbiguous() {
			return CompositionResult{}, &AmbiguousIdentityError{Ambiguous: resolution.Ambiguous}
		}
		return CompositionResult{}, err
	}

	activation, _, err := LoadActivation(input.ActivationPath)
	if err != nil {
		return CompositionResult{}, err
	}

	exResult, err := LoadExceptions(input.ExceptionDirs, input.Now)
	if err != nil {
		return CompositionResult{}, err
	}

	return CompositionResult{
		Profiles:          selectProfilesByID(allProfiles, finalIDs),
		Activation:        activation,
		Exceptions:        exResult.Live,
		ExpiredExceptions: exResult.Expired,
	}, nil
}

// selectProfilesByID returns, in ids' order, the Profile from all whose
// Metadata.ID matches. An id with no matching loaded Profile is silently
// skipped rather than erroring: a Binding can name a Profile ID that has no
// corresponding file on this machine yet (e.g. a teammate's personal
// Profile referenced by a shared team Binding) without that being a hard
// failure for every other, resolvable Profile in the same composition.
func selectProfilesByID(all []domain.Profile, ids []string) []domain.Profile {
	byID := make(map[string]domain.Profile, len(all))
	for _, p := range all {
		byID[p.Metadata.ID] = p
	}
	out := make([]domain.Profile, 0, len(ids))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			out = append(out, p)
		}
	}
	return out
}
