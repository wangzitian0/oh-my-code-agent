package resolve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// loadDomainFixture decodes a PR-03 testdata JSON document (committed at
// internal/domain/testdata) into v, so this package's golden scenario test
// exercises the exact documents docs/product/requirements.md §4 describes
// rather than a hand-retyped copy of them.
func loadDomainFixture(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "domain", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
}

// TestResolve_RequirementsSection4_GoldenScenario is the literal, named
// acceptance criterion from GitHub issue #17: "The requirements §4 golden
// scenario passes: claude-code gets deep-refactor + ui-review, codex gets
// codegraph, internal-docs disabled for codex." It loads the exact Profile
// and Activation fixtures committed by PR-03
// (internal/domain/testdata/profile-company-example.json and
// activation-worktree.json — docs/product/requirements.md §4.1 and §4.3)
// rather than retyping the documents.
func TestResolve_RequirementsSection4_GoldenScenario(t *testing.T) {
	var profile domain.Profile
	loadDomainFixture(t, "profile-company-example.json", &profile)
	if err := domain.ValidateProfile(profile); err != nil {
		t.Fatalf("ValidateProfile(profile-company-example.json): %v", err)
	}

	var activation domain.Activation
	loadDomainFixture(t, "activation-worktree.json", &activation)
	if err := domain.ValidateActivation(activation); err != nil {
		t.Fatalf("ValidateActivation(activation-worktree.json): %v", err)
	}

	// Cross-check the fixtures' exact asset/host assignments before relying
	// on them, so a silent drift in testdata fails loudly here rather than
	// producing a misleading pass/fail in the resolution assertions below.
	deepRefactor := profile.Spec.Assets.Skills[1]
	if deepRefactor.ID != "deep-refactor" || deepRefactor.Intent != domain.IntentDefault || len(deepRefactor.Hosts) != 1 || deepRefactor.Hosts[0] != "claude-code" {
		t.Fatalf("fixture drift: deep-refactor = %+v, want id=deep-refactor intent=DEFAULT hosts=[claude-code]", deepRefactor)
	}
	codegraph := profile.Spec.Assets.MCPServers[1]
	if codegraph.ID != "codegraph" || codegraph.Intent != domain.IntentDefault || len(codegraph.Hosts) != 1 || codegraph.Hosts[0] != "codex" {
		t.Fatalf("fixture drift: codegraph = %+v, want id=codegraph intent=DEFAULT hosts=[codex]", codegraph)
	}
	claudeHost := activation.Spec.Hosts["claude-code"]
	if len(claudeHost.Enable.Skills) != 1 || claudeHost.Enable.Skills[0] != "ui-review" {
		t.Fatalf("fixture drift: activation hosts.claude-code.enable.skills = %v, want [ui-review]", claudeHost.Enable.Skills)
	}
	codexHost := activation.Spec.Hosts["codex"]
	if len(codexHost.Disable.MCPServers) != 1 || codexHost.Disable.MCPServers[0] != "internal-docs" {
		t.Fatalf("fixture drift: activation hosts.codex.disable.mcpServers = %v, want [internal-docs]", codexHost.Disable.MCPServers)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{profile}

	claudeState, err := Resolve(profiles, activation, nil, "claude-code", now)
	if err != nil {
		t.Fatalf("Resolve(claude-code): %v", err)
	}
	if len(claudeState.Conflicts) != 0 {
		t.Fatalf("Resolve(claude-code) conflicts = %+v, want none", claudeState.Conflicts)
	}
	if !claudeState.IsActive(KindSkill, "deep-refactor") {
		t.Error("claude-code: deep-refactor should be active")
	}
	if !claudeState.IsActive(KindSkill, "ui-review") {
		t.Error("claude-code: ui-review should be active")
	}
	if claudeState.IsActive(KindMCPServer, "codegraph") {
		t.Error("claude-code: codegraph is codex-scoped and must NOT be active")
	}

	codexState, err := Resolve(profiles, activation, nil, "codex", now)
	if err != nil {
		t.Fatalf("Resolve(codex): %v", err)
	}
	if len(codexState.Conflicts) != 0 {
		t.Fatalf("Resolve(codex) conflicts = %+v, want none", codexState.Conflicts)
	}
	if !codexState.IsActive(KindMCPServer, "codegraph") {
		t.Error("codex: codegraph should be active")
	}
	if codexState.IsActive(KindMCPServer, "internal-docs") {
		t.Error("codex: internal-docs is disabled by the host-scoped Activation entry and must NOT be active")
	}
}

// TestResolve_ReenableDeniedAsset_FullResolution reuses the PR-03 fixtures
// behind domain.ValidateActivationAgainstProfiles (a host-neutral DENIED
// Profile entry plus a host-scoped Activation entry that tries to
// re-enable it) and proves the same invariant through the full Resolve
// function rather than only the direct-scope validator.
func TestResolve_ReenableDeniedAsset_FullResolution(t *testing.T) {
	var profile domain.Profile
	loadDomainFixture(t, "profile-with-denied-asset.json", &profile)
	var activation domain.Activation
	loadDomainFixture(t, "activation-reenable-denied.json", &activation)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	state, err := Resolve([]domain.Profile{profile}, activation, nil, "claude-code", now)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if state.IsActive(KindSkill, "release-production") {
		t.Error("release-production is host-neutrally DENIED; a host-scoped Activation enable must not re-enable it")
	}
}
