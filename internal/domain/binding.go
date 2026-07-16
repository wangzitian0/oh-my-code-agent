package domain

import "fmt"

// BindingMatch selects the contexts a Binding applies to.
type BindingMatch struct {
	Repository string   `json:"repository"`
	Paths      []string `json:"paths,omitempty"`
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
	if b.Spec.Match.Repository == "" {
		return fmt.Errorf("Binding: spec.match.repository is required")
	}
	if len(b.Spec.Profiles) == 0 {
		return fmt.Errorf("Binding: spec.profiles must not be empty")
	}
	return nil
}
