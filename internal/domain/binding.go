package domain

import "fmt"

// BindingMatch selects the contexts a Binding applies to. Exactly one of
// Repository or RepositoryGlob must be set (ValidateBinding enforces this);
// they are mutually exclusive alternatives for naming which repository
// context(s) the Binding selects, not independent filters that could both
// apply at once. See internal/profiles.MatchBindings for how each is
// matched, and that package's doc comment for why the "repository" value
// each is compared against is, in this build, a worktree's absolute
// filesystem path rather than the canonical remote identifier
// docs/product/requirements.md §4.2's example suggests.
type BindingMatch struct {
	// Repository requires an exact match against the repository value
	// MatchBindings is called with: one Binding = one literal repository.
	// This is the original, still-fully-supported matching mode; every
	// existing Binding using it keeps matching identically after
	// RepositoryGlob was added (backward compatible by construction, since
	// the two fields are alternatives, not a change to this one's
	// semantics).
	Repository string `json:"repository,omitempty"`
	// RepositoryGlob matches any repository value that satisfies the
	// pattern, using the same doublestar-style "**"/"*" segment grammar
	// internal/profiles.globMatch already implements for Paths (issue: one
	// Binding = one exact repository had no way to express "apply to every
	// checkout under ~/workspace, but not ~/zitian" -- a real gap for a
	// user with dozens of project checkouts/worktrees, each of which would
	// otherwise need its own near-duplicate Binding). A pattern like
	// "/Users/example/workspace/**" matches every worktree path under that
	// prefix, including newly created `git worktree add` checkouts that
	// didn't exist when the Binding was authored -- the whole point of a
	// glob over an enumerated list.
	RepositoryGlob string   `json:"repositoryGlob,omitempty"`
	Paths          []string `json:"paths,omitempty"`
}

// BindingSpec is the body of a Binding document.
type BindingSpec struct {
	Match    BindingMatch `json:"match"`
	Profiles []string     `json:"profiles"`
}

// Binding selects Profiles for a context (docs/product/requirements.md §4.2).
// It does not establish host precedence.
type Binding struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   Metadata    `json:"metadata"`
	Spec       BindingSpec `json:"spec"`
}

// ValidateBinding validates a Binding document against the v1alpha1 schema
// semantics.
func ValidateBinding(b Binding) error {
	if err := ValidateAPIVersion("Binding", b.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("Binding", b.Kind); err != nil {
		return err
	}
	if b.Metadata.ID == "" {
		return fmt.Errorf("Binding: metadata.id is required")
	}
	switch {
	case b.Spec.Match.Repository == "" && b.Spec.Match.RepositoryGlob == "":
		return fmt.Errorf("Binding: spec.match must set exactly one of repository or repositoryGlob, neither is set")
	case b.Spec.Match.Repository != "" && b.Spec.Match.RepositoryGlob != "":
		return fmt.Errorf("Binding: spec.match must set exactly one of repository or repositoryGlob, both are set")
	}
	if len(b.Spec.Profiles) == 0 {
		return fmt.Errorf("Binding: spec.profiles must not be empty")
	}
	return nil
}
