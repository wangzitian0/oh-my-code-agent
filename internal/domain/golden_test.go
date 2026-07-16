package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadFixture decodes a testdata JSON document into v. Fixtures mirror the
// example documents in docs/product/requirements.md §4; they are authored as
// JSON rather than the doc's literal YAML because the domain package has no
// YAML dependency yet (deferred to the config-loading PR that reads real
// user-authored files).
func loadFixture(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
}

func TestProfileCompanyExample_Golden(t *testing.T) {
	var p Profile
	loadFixture(t, "profile-company-example.json", &p)

	if err := ValidateProfile(p); err != nil {
		t.Fatalf("ValidateProfile: %v", err)
	}

	if len(p.Spec.Assets.Skills) != 2 {
		t.Fatalf("skills = %d, want 2", len(p.Spec.Assets.Skills))
	}
	deepRefactor := p.Spec.Assets.Skills[1]
	if deepRefactor.ID != "deep-refactor" || deepRefactor.Intent != IntentDefault {
		t.Fatalf("deep-refactor = %+v, want id=deep-refactor intent=DEFAULT", deepRefactor)
	}
	if len(deepRefactor.Hosts) != 1 || deepRefactor.Hosts[0] != "claude-code" {
		t.Fatalf("deep-refactor.hosts = %v, want [claude-code]", deepRefactor.Hosts)
	}

	codegraph := p.Spec.Assets.MCPServers[1]
	if codegraph.ID != "codegraph" || len(codegraph.Hosts) != 1 || codegraph.Hosts[0] != "codex" {
		t.Fatalf("codegraph = %+v, want hosts=[codex]", codegraph)
	}
}

func TestBindingOrderService_Golden(t *testing.T) {
	var b Binding
	loadFixture(t, "binding-order-service.json", &b)

	if err := ValidateBinding(b); err != nil {
		t.Fatalf("ValidateBinding: %v", err)
	}
	if b.Spec.Match.Repository != "github.com/example/order-service" {
		t.Fatalf("repository = %q", b.Spec.Match.Repository)
	}
	if len(b.Spec.Profiles) != 4 {
		t.Fatalf("profiles = %d, want 4", len(b.Spec.Profiles))
	}
}

func TestActivationWorktree_Golden(t *testing.T) {
	var a Activation
	loadFixture(t, "activation-worktree.json", &a)

	if err := ValidateActivation(a); err != nil {
		t.Fatalf("ValidateActivation: %v", err)
	}

	claude, ok := a.Spec.Hosts["claude-code"]
	if !ok {
		t.Fatal("expected a claude-code host entry")
	}
	if len(claude.Enable.Skills) != 1 || claude.Enable.Skills[0] != "ui-review" {
		t.Fatalf("claude-code enable.skills = %v, want [ui-review]", claude.Enable.Skills)
	}

	codex, ok := a.Spec.Hosts["codex"]
	if !ok {
		t.Fatal("expected a codex host entry")
	}
	if len(codex.Disable.MCPServers) != 1 || codex.Disable.MCPServers[0] != "internal-docs" {
		t.Fatalf("codex disable.mcpServers = %v, want [internal-docs]", codex.Disable.MCPServers)
	}
}

func TestProfile_UnknownAPIVersion_Rejected(t *testing.T) {
	var p Profile
	loadFixture(t, "profile-unknown-apiversion.json", &p)

	err := ValidateProfile(p)
	if err == nil {
		t.Fatal("expected an error for an unknown apiVersion, got nil")
	}
}

func TestProfile_UnknownHostID_Rejected(t *testing.T) {
	var p Profile
	loadFixture(t, "profile-unknown-host.json", &p)

	err := ValidateProfile(p)
	if err == nil {
		t.Fatal("expected an error for an unknown host id, got nil")
	}
}

func TestActivation_CannotReenableDeniedAsset(t *testing.T) {
	var profile Profile
	loadFixture(t, "profile-with-denied-asset.json", &profile)
	if err := ValidateProfile(profile); err != nil {
		t.Fatalf("ValidateProfile(deny fixture): %v", err)
	}

	var activation Activation
	loadFixture(t, "activation-reenable-denied.json", &activation)
	if err := ValidateActivation(activation); err != nil {
		t.Fatalf("ValidateActivation(reenable fixture): %v", err)
	}

	err := ValidateActivationAgainstProfiles(activation, []Profile{profile})
	if err == nil {
		t.Fatal("expected an error: a host-scoped Activation entry re-enabled a DENIED asset")
	}
}
