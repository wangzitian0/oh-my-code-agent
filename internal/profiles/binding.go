package profiles

import (
	"fmt"
	"path"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// LoadBindings reads and validates every Binding YAML document under each of
// dirs (docs/architecture/README.md §7:
// ~/.config/omca/bindings/ and <repository>/.omca's own binding-shaped
// documents, if any). Loading semantics match LoadProfiles: a missing
// directory is skipped, and the first invalid document fails closed with an
// error naming the file and field.
func LoadBindings(dirs []string) ([]domain.Binding, error) {
	var out []domain.Binding
	for _, dir := range dirs {
		files, err := discoverYAMLFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			var b domain.Binding
			if err := decodeYAMLDocument(f, &b); err != nil {
				return nil, err
			}
			if err := domain.ValidateBinding(b); err != nil {
				return nil, fmt.Errorf("profiles: %s: %w", f, err)
			}
			out = append(out, b)
		}
	}
	return out, nil
}

// MatchBindings returns every Binding in bindings whose spec.match selects
// repository and relPath (docs/product/requirements.md §4.2). repository is
// caller-supplied: this package does not itself derive a repository
// identifier from a worktree (e.g. by parsing `git remote get-url` output)
// — internal/context.DetectWorktree deliberately never shells out to git,
// and no code in this repository resolves a canonical repository identifier
// yet, so MatchBindings takes it as an explicit input, matching this
// project's existing discipline of never resolving an ambient value deep
// inside a core package. In this build, every real call site
// (cmd/omca/activate.go, cmd/omca/mcp.go) actually passes wt.Root, a
// worktree's absolute filesystem path, not the canonical remote identifier
// docs/product/requirements.md §4.2's YAML example suggests ("a documented,
// known discrepancy — see domain.BindingMatch's doc comment; deliberately
// out of scope here). relPath is the path being evaluated, relative to the
// repository root, using "/" separators regardless of OS (matching the
// glob-pattern convention docs/product/requirements.md §4.2's YAML example
// authors patterns with, e.g. "apps/api/**").
//
// Matching rules:
//   - spec.match.repository, if set, must equal repository exactly: one
//     Binding = one literal repository value. This is the original
//     matching mode and its semantics are unchanged by the addition of
//     spec.match.repositoryGlob below — every existing Binding using
//     repository keeps matching identically.
//   - spec.match.repositoryGlob, if set instead, matches repository
//     against a doublestar-style glob pattern via globMatch — the same
//     matcher this function already uses for spec.match.paths, reused
//     directly rather than duplicated (both match a "/"-separated string
//     against a "**"/"*" segment pattern; a worktree path is exactly such
//     a string). This closes a real gap: expressing "apply this Binding to
//     every project under ~/workspace" previously required one Binding per
//     project, with no way to automatically pick up new checkouts or `git
//     worktree add` worktrees created later. domain.ValidateBinding
//     enforces that spec.match sets exactly one of repository or
//     repositoryGlob, so exactly one of these two rules ever applies to a
//     given Binding.
//   - An empty spec.match.paths matches every relPath: a Binding scoped to
//     a whole repository does not need to repeat "**". A non-empty
//     spec.match.paths matches if relPath matches at least one pattern
//     (matchesPaths / globMatch).
//
// On priority when a repository matches more than one Binding (e.g. one
// glob Binding and one exact-repository Binding, or two overlapping
// globs): MatchBindings deliberately applies no precedence between them —
// every matching Binding is returned, and MatchedProfileIDs unions all of
// their spec.profiles. This is not an oversight; omca's model is that
// nothing loads unless some selected Profile explicitly includes it (there
// is no "exclude" concept at the Binding-match layer), and any real
// disagreement between two selected Profiles about the same asset is
// already resolved by internal/effective's precedence operators (e.g.
// DENY_WINS), which run downstream of this selection step and operate on
// individual assets, not whole Profiles. A use case like "skynet-base for
// every ~/workspace/** project, nothing extra under ~/zitian/**" needs
// exactly one glob Binding (scoped to ~/workspace/**) and zero Bindings for
// ~/zitian — no exclusion Binding required, because a repository with no
// matching Binding at all simply selects no additional Profiles. Add
// Binding-match-layer precedence only if a real case surfaces where two
// matched Bindings' selected Profiles conflict in a way the downstream
// merge operators cannot already resolve.
func MatchBindings(bindings []domain.Binding, repository, relPath string) []domain.Binding {
	var out []domain.Binding
	for _, b := range bindings {
		if !matchesRepository(b.Spec.Match, repository) {
			continue
		}
		if matchesPaths(b.Spec.Match.Paths, relPath) {
			out = append(out, b)
		}
	}
	return out
}

// matchesRepository reports whether match selects repository, using
// whichever of match.Repository (exact string equality) or
// match.RepositoryGlob (globMatch) is set. domain.ValidateBinding already
// guarantees exactly one of the two is non-empty for any Binding that
// reached this function via LoadBindings, but this helper does not assume
// that invariant was already checked: it dispatches explicitly on which
// field is actually set (RepositoryGlob takes priority if, unexpectedly,
// both were somehow set) rather than falling back to a bare
// `match.Repository == repository` comparison, whose == would itself
// evaluate true for an unset match.Repository ("") against an
// empty-string repository argument -- silently matching an unvalidated,
// field-less Binding against an empty caller-supplied repository, instead
// of correctly matching nothing (Copilot review finding on this PR; see
// TestMatchesRepository_UnsetMatchNeverMatches for the regression this
// guards). A Binding with neither field set must match no repository
// value at all, empty string included.
func matchesRepository(match domain.BindingMatch, repository string) bool {
	switch {
	case match.RepositoryGlob != "":
		return globMatch(match.RepositoryGlob, repository)
	case match.Repository != "":
		return match.Repository == repository
	default:
		return false
	}
}

// MatchedProfileIDs returns the union of every matched Binding's
// spec.profiles entries, deduplicated, in first-seen order across bindings
// (the order MatchBindings returned them in).
func MatchedProfileIDs(bindings []domain.Binding) []string {
	seen := make(map[string]bool)
	var out []string
	for _, b := range bindings {
		for _, id := range b.Spec.Profiles {
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// matchesPaths reports whether relPath matches at least one of patterns, or
// whether patterns is empty (an unscoped Binding matches every path).
func matchesPaths(patterns []string, relPath string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if globMatch(pattern, relPath) {
			return true
		}
	}
	return false
}

// globMatch reports whether p matches pattern, a doublestar-style glob:
// "**" matches zero or more entire path segments (so it can match the
// repository root itself, not only non-empty subpaths), "*" matches within
// a single segment (never crossing "/", via path.Match), and any other
// segment must match literally. This is a small, purpose-built matcher
// rather than a dependency (go.mod has none for this) — Binding path
// patterns are a closed, simple grammar (docs/product/requirements.md
// §4.2's only documented example is "**"), so a general glob library would
// be more machinery than the grammar needs.
//
// p == "" (the repository root) is special-cased to zero segments, not one
// empty-string segment: strings.Split("", "/") returns [""] (a single
// empty-string element), and without this case a single-segment pattern
// like "*" would incorrectly match the root too — path.Match("*", "")
// itself returns true, since "*" matches a zero-length sequence of
// non-separator characters — even though "*" is meant to mean "exactly one
// real path segment," which the root is not (Copilot review finding on
// this PR). Only "**" (zero-or-more segments) or an empty patterns list
// (matchesPaths' own separate case) should ever match the root.
func globMatch(pattern, p string) bool {
	segments := strings.Split(p, "/")
	if p == "" {
		segments = nil
	}
	return globMatchSegments(strings.Split(pattern, "/"), segments)
}

func globMatchSegments(pattern, seg []string) bool {
	if len(pattern) == 0 {
		return len(seg) == 0
	}
	if pattern[0] == "**" {
		if globMatchSegments(pattern[1:], seg) {
			return true
		}
		if len(seg) == 0 {
			return false
		}
		return globMatchSegments(pattern, seg[1:])
	}
	if len(seg) == 0 {
		return false
	}
	matched, err := path.Match(pattern[0], seg[0])
	if err != nil || !matched {
		return false
	}
	return globMatchSegments(pattern[1:], seg[1:])
}
