package profiles

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// categoryOrder is the broad-to-narrow identity scope order docs/product/
// requirements.md §5.1 composes: "personal default + repository-bound
// company Profiles + matching team Profiles + project Profile + local
// worktree activation". internal/resolve.Resolve treats later Profiles in
// its input slice as the more specific, "lower" scope (see its own doc
// comment), so FinalProfileIDs must hand Profile IDs back in exactly this
// order for that precedence convention to hold.
//
// "task" is a documented profiles/ subdirectory (docs/architecture/
// README.md §7) with no explicit position in §5.1's composition formula.
// It is treated as the narrowest scope, ordered after "project": a task
// profile's whole purpose is a short-lived, per-piece-of-work refinement,
// narrower than anything else in the formula. This placement is this
// package's own documented, defensible choice — flagged for review, not
// drawn from an explicit requirements.md statement.
var categoryOrder = []string{"personal", "company", "team", "project", "task"}

// profileCategory extracts the identity category from a Profile ID, using
// the "<category>:<name>" convention every documented example in
// docs/product/requirements.md §4 uses (e.g. "company:example",
// "personal:alice", "team:payments", "project:order-service"). An ID with
// no ":" is its own, single-entry category — this keeps profileCategory
// total (never panics) for a malformed or unconventional ID rather than
// rejecting it outright; domain.ValidateProfile is the actual gatekeeper
// for what counts as a well-formed Profile document.
func profileCategory(id string) string {
	if i := strings.IndexByte(id, ':'); i >= 0 {
		return id[:i]
	}
	return id
}

// AmbiguousCategory names one identity category with more than one
// plausible Profile ID after Binding matching. Candidates is sorted for
// determinism.
type AmbiguousCategory struct {
	Category   string
	Candidates []string
}

// IdentityResolution is the result of grouping Binding-matched Profile IDs
// by category: Resolved names the single plausible Profile for every
// unambiguous category, and Ambiguous lists every category that still has
// more than one plausible candidate and therefore needs an explicit
// selection (docs/product/requirements.md §5.1: "If multiple company or
// project identities remain plausible, OMCA asks the user to select one").
type IdentityResolution struct {
	Resolved  map[string]string
	Ambiguous []AmbiguousCategory
}

// IsAmbiguous reports whether any identity category still has more than one
// plausible candidate.
func (r IdentityResolution) IsAmbiguous() bool {
	return len(r.Ambiguous) > 0
}

// ResolveIdentities groups matchedProfileIDs (the union of Binding-selected
// Profile IDs — see MatchedProfileIDs) by category and reports, per
// category, either the one plausible Profile or every candidate that
// remains plausible. It never guesses among multiple candidates: that
// selection is either a caller-supplied explicit choice (FinalProfileIDs)
// or a previously persisted one (ReadSelection) — see this package's doc
// comment and issue #16's scope note ("this is NOT this package's job to
// guess").
func ResolveIdentities(matchedProfileIDs []string) IdentityResolution {
	byCategory := map[string][]string{}
	var order []string
	for _, id := range matchedProfileIDs {
		cat := profileCategory(id)
		if _, ok := byCategory[cat]; !ok {
			order = append(order, cat)
		}
		byCategory[cat] = append(byCategory[cat], id)
	}
	sort.Strings(order)

	res := IdentityResolution{Resolved: map[string]string{}}
	for _, cat := range order {
		ids := byCategory[cat]
		if len(ids) == 1 {
			res.Resolved[cat] = ids[0]
			continue
		}
		distinct := dedupeSorted(ids)
		if len(distinct) == 1 {
			res.Resolved[cat] = distinct[0]
			continue
		}
		res.Ambiguous = append(res.Ambiguous, AmbiguousCategory{Category: cat, Candidates: distinct})
	}
	return res
}

func dedupeSorted(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// FinalProfileIDs returns the concrete Profile ID for every category res
// names, in categoryOrder's broad-to-narrow order (with any category
// outside that fixed list appended afterward, sorted, for determinism): the
// unambiguous Resolved value, or, for each Ambiguous category, selection's
// answer.
//
// It returns an error — never a guess — if selection has no answer for an
// Ambiguous category, or if selection names a Profile ID that is not one of
// that category's actual candidates (a stale or corrupted selection file
// must not silently pick something Binding matching never actually
// offered).
func FinalProfileIDs(res IdentityResolution, selection map[string]string) ([]string, error) {
	categories := make([]string, 0, len(res.Resolved)+len(res.Ambiguous))
	for cat := range res.Resolved {
		categories = append(categories, cat)
	}
	ambByCategory := make(map[string]AmbiguousCategory, len(res.Ambiguous))
	for _, amb := range res.Ambiguous {
		categories = append(categories, amb.Category)
		ambByCategory[amb.Category] = amb
	}
	categories = orderedCategories(categories)

	out := make([]string, 0, len(categories))
	for _, cat := range categories {
		if id, ok := res.Resolved[cat]; ok {
			out = append(out, id)
			continue
		}
		amb := ambByCategory[cat]
		chosen, ok := selection[cat]
		if !ok || chosen == "" {
			return nil, fmt.Errorf("profiles: identity category %q is ambiguous (candidates: %v) and no selection was provided", cat, amb.Candidates)
		}
		found := false
		for _, c := range amb.Candidates {
			if c == chosen {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("profiles: selected identity %q for category %q is not one of the plausible candidates %v", chosen, cat, amb.Candidates)
		}
		out = append(out, chosen)
	}
	return out, nil
}

// orderedCategories sorts categories per categoryOrder's fixed broad-to-
// narrow ranking, with any category outside that list appended afterward
// in alphabetical order for determinism.
func orderedCategories(categories []string) []string {
	rank := make(map[string]int, len(categoryOrder))
	for i, c := range categoryOrder {
		rank[c] = i
	}
	sorted := append([]string(nil), categories...)
	sort.Slice(sorted, func(i, j int) bool {
		ri, iok := rank[sorted[i]]
		rj, jok := rank[sorted[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok:
			return true
		case jok:
			return false
		default:
			return sorted[i] < sorted[j]
		}
	})
	return sorted
}

// identitySelectionDocument is the on-disk shape PersistSelection writes and
// ReadSelection reads at <worktree state dir>/desired/identities.yaml
// (docs/architecture/README.md §8). It is this package's own local state
// format, never authored by a human and never read by any other package, so
// it uses plain `yaml:` tags directly rather than this file's JSON-tagged-
// struct round-trip idiom (decodeYAMLDocument) — that idiom exists to
// correctly decode externally-authored, JSON-schema-governed documents
// (Profile/Binding/Activation/Exception); this type has no JSON-Schema
// counterpart and no external author to stay compatible with.
type identitySelectionDocument struct {
	Worktree   string            `yaml:"worktree"`
	SelectedAt time.Time         `yaml:"selectedAt"`
	Selection  map[string]string `yaml:"selection"`
}

// identitiesFileName is the fixed leaf name docs/architecture/README.md §8
// gives worktree identity-selection state.
const identitiesFileName = "identities.yaml"

// identitiesPath computes the identities.yaml path under worktreeStateDir's
// desired/ subdirectory, the one place this package ever writes or reads a
// persisted identity selection.
func identitiesPath(worktreeStateDir string) string {
	return filepath.Join(worktreeStateDir, "desired", identitiesFileName)
}

// PersistSelection writes selection (one Profile ID per identity category —
// the caller-resolved answer to an IdentityResolution.Ambiguous set) to
// worktreeStateDir/desired/identities.yaml.
//
// worktreeStateDir is caller-supplied and never resolved internally,
// matching this project's existing discipline of injecting real XDG/state
// roots from the command layer only (cmd/omca/statedir.go's
// worktreeStateDirPath is the one place that turns a worktree ID into a
// real path; internal/runtime.EnsureGeneration's generationsRoot parameter
// is the same pattern). It must never be a path under
// <repository>/.omca — passing one would defeat the entire point of this
// function: docs/architecture/README.md §7's "Personal identity selection
// and worktree activation are local state, not shared repository
// configuration" (issue #16 AC: "the choice persists locally and is never
// written into the repository").
//
// The write is atomic (temp file + rename within the same directory) so a
// concurrent reader never observes a half-written selection, matching
// internal/runtime.SetCurrentGeneration's own atomic-write discipline for
// worktree state.
func PersistSelection(worktreeStateDir, worktreeID string, selection map[string]string, now time.Time) error {
	if worktreeStateDir == "" {
		return fmt.Errorf("profiles: PersistSelection: worktreeStateDir is required")
	}
	desiredDir := filepath.Join(worktreeStateDir, "desired")
	if err := os.MkdirAll(desiredDir, 0o755); err != nil {
		return fmt.Errorf("profiles: PersistSelection: %w", err)
	}

	doc := identitySelectionDocument{
		Worktree:   worktreeID,
		SelectedAt: now.UTC(),
		Selection:  selection,
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("profiles: PersistSelection: %w", err)
	}

	finalPath := identitiesPath(worktreeStateDir)
	tmpPath := finalPath + fmt.Sprintf(".tmp-%d-%d", os.Getpid(), now.UnixNano())
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("profiles: PersistSelection: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("profiles: PersistSelection: %w", err)
	}
	return nil
}

// ReadSelection reads back a selection PersistSelection previously wrote
// under worktreeStateDir. ok is false, with no error, when no selection has
// ever been persisted for this worktree — an expected state the very first
// time an ambiguous identity is encountered.
func ReadSelection(worktreeStateDir string) (map[string]string, bool, error) {
	path := identitiesPath(worktreeStateDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("profiles: ReadSelection: %s: %w", path, err)
	}
	var doc identitySelectionDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, false, fmt.Errorf("profiles: ReadSelection: %s: %w", path, err)
	}
	return doc.Selection, true, nil
}
