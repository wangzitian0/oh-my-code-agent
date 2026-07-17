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
// inside a core package. relPath is the path being evaluated, relative to
// the repository root, using "/" separators regardless of OS (matching the
// glob-pattern convention docs/product/requirements.md §4.2's YAML example
// authors patterns with, e.g. "apps/api/**").
//
// Matching rules:
//   - spec.match.repository must equal repository exactly. OMCA desired-
//     state documents name a repository by its canonical remote identifier
//     (§4.2's "github.com/example/order-service" example), not a
//     filesystem path, so this is a plain string comparison.
//   - An empty spec.match.paths matches every relPath: a Binding scoped to
//     a whole repository does not need to repeat "**". A non-empty
//     spec.match.paths matches if relPath matches at least one pattern
//     (matchesPaths / globMatch).
func MatchBindings(bindings []domain.Binding, repository, relPath string) []domain.Binding {
	var out []domain.Binding
	for _, b := range bindings {
		if b.Spec.Match.Repository != repository {
			continue
		}
		if matchesPaths(b.Spec.Match.Paths, relPath) {
			out = append(out, b)
		}
	}
	return out
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
func globMatch(pattern, p string) bool {
	return globMatchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
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
