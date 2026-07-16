package domain

import "fmt"

// AssetRef is one asset entry inside a Profile's desired-state assets list.
// Hosts is a selector (docs/product/requirements.md §4.1): empty means the
// intent applies to every host launched in the matching context; a
// non-empty list refines the intent for exactly those canonical host IDs.
type AssetRef struct {
	ID     string   `json:"id"`
	Intent Intent   `json:"intent"`
	Hosts  []string `json:"hosts,omitempty"`
}

// ProfileAssets groups the asset kinds a Profile can carry intent for.
type ProfileAssets struct {
	Skills       []AssetRef `json:"skills,omitempty"`
	MCPServers   []AssetRef `json:"mcpServers,omitempty"`
	Instructions []AssetRef `json:"instructions,omitempty"`
}

// PermissionRef is one permission entry inside a Profile's policy.
type PermissionRef struct {
	Intent Intent `json:"intent"`
	Value  string `json:"value,omitempty"`
}

// ProfilePolicy carries non-asset desired-state constraints such as
// sandbox permissions.
type ProfilePolicy struct {
	Permissions map[string]PermissionRef `json:"permissions,omitempty"`
}

// ProfileSpec is the body of a Profile document.
type ProfileSpec struct {
	Assets ProfileAssets `json:"assets,omitempty"`
	Policy ProfilePolicy `json:"policy,omitempty"`
}

// Metadata carries a document's stable logical ID.
type Metadata struct {
	ID string `json:"id"`
}

// Profile is a composable set of desired-state intent
// (docs/product/requirements.md §4.1, init.md decision 3).
type Profile struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   Metadata    `json:"metadata"`
	Spec       ProfileSpec `json:"spec"`
}

// ValidateProfile validates a Profile document against the v1alpha1 schema
// semantics: apiVersion/kind, required metadata, valid intents, and
// hosts-selector entries that name canonical host IDs.
func ValidateProfile(p Profile) error {
	if err := ValidateAPIVersion("Profile", p.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("Profile", p.Kind); err != nil {
		return err
	}
	if p.Metadata.ID == "" {
		return fmt.Errorf("Profile: metadata.id is required")
	}

	assetGroups := []struct {
		name string
		refs []AssetRef
	}{
		{"skills", p.Spec.Assets.Skills},
		{"mcpServers", p.Spec.Assets.MCPServers},
		{"instructions", p.Spec.Assets.Instructions},
	}
	for _, group := range assetGroups {
		for _, ref := range group.refs {
			if err := validateAssetRef(group.name, ref); err != nil {
				return err
			}
		}
	}

	for name, perm := range p.Spec.Policy.Permissions {
		if err := ValidateIntent(perm.Intent); err != nil {
			return fmt.Errorf("Profile: permission %q: %w", name, err)
		}
	}

	return nil
}

func validateAssetRef(group string, ref AssetRef) error {
	if ref.ID == "" {
		return fmt.Errorf("Profile: %s: asset id is required", group)
	}
	if err := ValidateIntent(ref.Intent); err != nil {
		return fmt.Errorf("Profile: %s %q: %w", group, ref.ID, err)
	}
	if err := ValidateHostIDs(ref.Hosts); err != nil {
		return fmt.Errorf("Profile: %s %q: %w", group, ref.ID, err)
	}
	return nil
}
