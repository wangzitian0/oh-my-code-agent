package domain

import "testing"

func TestValidateBinding_RequiredFields(t *testing.T) {
	base := Binding{
		APIVersion: SupportedAPIVersion,
		Kind:       "Binding",
		Metadata:   Metadata{ID: "binding:order-service"},
		Spec: BindingSpec{
			Match:    BindingMatch{Repository: "github.com/example/order-service"},
			Profiles: []string{"company:example"},
		},
	}
	if err := ValidateBinding(base); err != nil {
		t.Fatalf("baseline binding should validate: %v", err)
	}

	missingID := base
	missingID.Metadata = Metadata{}
	if err := ValidateBinding(missingID); err == nil {
		t.Error("expected an error for missing metadata.id")
	}

	missingRepo := base
	missingRepo.Spec.Match = BindingMatch{}
	if err := ValidateBinding(missingRepo); err == nil {
		t.Error("expected an error for missing spec.match.repository and spec.match.repositoryGlob (neither set)")
	}

	bothRepoFields := base
	bothRepoFields.Spec.Match = BindingMatch{
		Repository:     "github.com/example/order-service",
		RepositoryGlob: "github.com/example/**",
	}
	if err := ValidateBinding(bothRepoFields); err == nil {
		t.Error("expected an error when both spec.match.repository and spec.match.repositoryGlob are set")
	}

	globOnly := base
	globOnly.Spec.Match = BindingMatch{RepositoryGlob: "github.com/example/**"}
	if err := ValidateBinding(globOnly); err != nil {
		t.Errorf("a repositoryGlob-only spec.match should validate: %v", err)
	}

	missingProfiles := base
	missingProfiles.Spec.Profiles = nil
	if err := ValidateBinding(missingProfiles); err == nil {
		t.Error("expected an error for empty spec.profiles")
	}
}

func TestValidateProfile_RequiredFields(t *testing.T) {
	base := Profile{
		APIVersion: SupportedAPIVersion,
		Kind:       "Profile",
		Metadata:   Metadata{ID: "company:example"},
	}
	if err := ValidateProfile(base); err != nil {
		t.Fatalf("baseline profile should validate: %v", err)
	}

	missingID := base
	missingID.Metadata = Metadata{}
	if err := ValidateProfile(missingID); err == nil {
		t.Error("expected an error for missing metadata.id")
	}

	missingAssetID := base
	missingAssetID.Spec.Assets.Skills = []AssetRef{{Intent: IntentAvailable}}
	if err := ValidateProfile(missingAssetID); err == nil {
		t.Error("expected an error for an asset with no id")
	}

	badIntent := base
	badIntent.Spec.Assets.Skills = []AssetRef{{ID: "x", Intent: "MAYBE"}}
	if err := ValidateProfile(badIntent); err == nil {
		t.Error("expected an error for an invalid asset intent")
	}

	badPermission := base
	badPermission.Spec.Policy.Permissions = map[string]PermissionRef{
		"sandbox": {Intent: "MAYBE"},
	}
	if err := ValidateProfile(badPermission); err == nil {
		t.Error("expected an error for an invalid permission intent")
	}
}

func TestValidateActivation_RequiredFields(t *testing.T) {
	base := Activation{
		APIVersion: SupportedAPIVersion,
		Kind:       "Activation",
		Metadata:   ActivationMetadata{Worktree: "worktree:sha256:abc"},
	}
	if err := ValidateActivation(base); err != nil {
		t.Fatalf("baseline activation should validate: %v", err)
	}

	missingWorktree := base
	missingWorktree.Metadata = ActivationMetadata{}
	if err := ValidateActivation(missingWorktree); err == nil {
		t.Error("expected an error for missing metadata.worktree")
	}

	badHost := base
	badHost.Spec.Hosts = map[string]HostActivation{"not-a-host": {}}
	if err := ValidateActivation(badHost); err == nil {
		t.Error("expected an error for an unknown host key")
	}
}

func TestValidateActivationAgainstProfiles_HostNeutralEnableBlockedByHostNeutralDeny(t *testing.T) {
	profile := Profile{
		Spec: ProfileSpec{Assets: ProfileAssets{
			MCPServers: []AssetRef{{ID: "danger-server", Intent: IntentDenied}},
		}},
	}
	activation := Activation{
		Spec: ActivationSpec{Enable: ActivationSelection{MCPServers: []string{"danger-server"}}},
	}
	if err := ValidateActivationAgainstProfiles(activation, []Profile{profile}); err == nil {
		t.Error("expected an error: host-neutral enable re-enabled a host-neutral DENIED asset")
	}
}

func TestValidateActivationAgainstProfiles_AllowsNonDeniedAsset(t *testing.T) {
	profile := Profile{
		Spec: ProfileSpec{Assets: ProfileAssets{
			Skills: []AssetRef{{ID: "code-review", Intent: IntentAvailable}},
		}},
	}
	activation := Activation{
		Spec: ActivationSpec{Enable: ActivationSelection{Skills: []string{"code-review"}}},
	}
	if err := ValidateActivationAgainstProfiles(activation, []Profile{profile}); err != nil {
		t.Errorf("unexpected error enabling a non-denied asset: %v", err)
	}
}

func TestCanonicalDigest_RejectsUnmarshalable(t *testing.T) {
	_, err := CanonicalDigest(make(chan int))
	if err == nil {
		t.Error("expected an error digesting an unmarshalable value")
	}
}
