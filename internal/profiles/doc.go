// Package profiles composes Profiles, Bindings, Activation, and exceptions into desired state.
//
// This package owns everything upstream of internal/resolve.Resolve
// (already built, PR-13, the frozen intent resolution engine): loading
// Profile/Binding/Activation/Exception documents from disk (docs/
// architecture/README.md §7's `~/.config/omca/...` and
// `<repository>/.omca/...` layout, plus §8's worktree-local state layout for
// Activation and the persisted identity selection), matching Bindings by
// repository and path, resolving which identity's Profiles actually apply
// to a worktree (asking for an explicit selection, never guessing, when
// more than one remains plausible), and handling Exception expiry. It does
// not implement REQUIRED/DEFAULT/AVAILABLE/DENIED precedence, host-scoped
// refinement, or conflict detection — that is internal/resolve's job.
//
// document.go: the shared YAML-document decoding helper (decodeYAMLDocument)
// every loader in this package uses.
//
// profile.go / binding.go / activation.go / exception.go: one loader per
// desired-state document kind, plus binding.go's Binding-matching
// (MatchBindings, MatchedProfileIDs) and exception.go's live/expired split
// (LoadExceptions).
//
// identity.go: grouping Binding-matched Profile IDs into per-category
// identities (ResolveIdentities), resolving remaining ambiguity against an
// explicit selection (FinalProfileIDs), and persisting/reading that
// selection as worktree-local state (PersistSelection, ReadSelection) —
// never written into the repository.
//
// compose.go: Compose, the single entry point that ties every loader and
// resolution step together into the []domain.Profile + domain.Activation +
// []domain.Exception set a caller hands to resolve.Resolve, once per host.
package profiles
