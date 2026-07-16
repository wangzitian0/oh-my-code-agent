package resolve

import (
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func mustSkillProfile(id string, refs ...domain.AssetRef) domain.Profile {
	return domain.Profile{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Profile",
		Metadata:   domain.Metadata{ID: id},
		Spec:       domain.ProfileSpec{Assets: domain.ProfileAssets{Skills: refs}},
	}
}

func mustMCPProfile(id string, refs ...domain.AssetRef) domain.Profile {
	return domain.Profile{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Profile",
		Metadata:   domain.Metadata{ID: id},
		Spec:       domain.ProfileSpec{Assets: domain.ProfileAssets{MCPServers: refs}},
	}
}

var fixedNow = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

// TestResolve_InvalidHost_IsError proves an unknown host ID is a genuine
// input error (domain.ValidateHostID), not a Conflict.
func TestResolve_InvalidHost_IsError(t *testing.T) {
	_, err := Resolve(nil, domain.Activation{}, nil, "not-a-real-host", fixedNow)
	if err == nil {
		t.Fatal("Resolve with an unknown host ID should return an error")
	}
}

// TestResolve_DeniedCannotBeWeakenedByLowerScopeProfile covers the golden
// case: two Profiles disagree, one DENIED and one DEFAULT/AVAILABLE, for
// the same asset+host — DENIED wins regardless of which Profile (broad or
// narrow) asserts it.
func TestResolve_DeniedCannotBeWeakenedByLowerScopeProfile(t *testing.T) {
	t.Run("narrow profile tries to weaken a broad DENIED", func(t *testing.T) {
		broad := mustSkillProfile("company:broad", domain.AssetRef{ID: "risky-tool", Intent: domain.IntentDenied})
		narrow := mustSkillProfile("project:narrow", domain.AssetRef{ID: "risky-tool", Intent: domain.IntentAvailable})

		state, err := Resolve([]domain.Profile{broad, narrow}, domain.Activation{}, nil, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if state.IsActive(KindSkill, "risky-tool") {
			t.Error("a later, narrower Profile must not weaken an earlier DENIED")
		}
		asset, ok := state.Find(KindSkill, "risky-tool")
		if !ok || asset.Intent != domain.IntentDenied {
			t.Errorf("resolved intent = %+v, want DENIED", asset)
		}
	})

	t.Run("broad profile DEFAULT, narrow profile DENIED", func(t *testing.T) {
		broad := mustSkillProfile("company:broad2", domain.AssetRef{ID: "other-tool", Intent: domain.IntentDefault})
		narrow := mustSkillProfile("project:narrow2", domain.AssetRef{ID: "other-tool", Intent: domain.IntentDenied})

		state, err := Resolve([]domain.Profile{broad, narrow}, domain.Activation{}, nil, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if state.IsActive(KindSkill, "other-tool") {
			t.Error("DENIED from the narrower Profile must win over the broader DEFAULT")
		}
	})
}

// TestResolve_DeniedCannotBeWeakenedByHostScopedActivation covers both
// directions ValidateActivationAgainstProfiles's own doc comment says it
// does NOT cover: a host-neutral DENIED against a host-scoped enable (the
// reverse is exercised by TestResolve_ReenableDeniedAsset_FullResolution in
// golden_test.go, reusing the PR-03 fixtures), and a host-scoped DENIED
// against a host-neutral enable.
func TestResolve_DeniedCannotBeWeakenedByHostScopedActivation(t *testing.T) {
	t.Run("host-neutral DENIED vs host-scoped enable", func(t *testing.T) {
		profile := mustSkillProfile("company:example", domain.AssetRef{ID: "release-production", Intent: domain.IntentDenied})
		activation := domain.Activation{
			Spec: domain.ActivationSpec{
				Hosts: map[string]domain.HostActivation{
					"claude-code": {Enable: domain.ActivationSelection{Skills: []string{"release-production"}}},
				},
			},
		}
		state, err := Resolve([]domain.Profile{profile}, activation, nil, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if state.IsActive(KindSkill, "release-production") {
			t.Error("a host-scoped enable must not re-enable a host-neutral DENIED")
		}
	})

	t.Run("host-scoped DENIED vs host-neutral enable", func(t *testing.T) {
		profile := mustMCPProfile("company:example", domain.AssetRef{ID: "secure-tool", Intent: domain.IntentDenied, Hosts: []string{"codex"}})
		activation := domain.Activation{
			Spec: domain.ActivationSpec{
				Enable: domain.ActivationSelection{MCPServers: []string{"secure-tool"}},
			},
		}
		state, err := Resolve([]domain.Profile{profile}, activation, nil, "codex", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if state.IsActive(KindMCPServer, "secure-tool") {
			t.Error("a host-neutral enable must not re-enable a host-scoped DENIED at the host it targets")
		}
	})
}

// TestResolve_HostsSelectorRefinement covers: a host-neutral DEFAULT plus a
// host-scoped DENIED for one specific host — that host doesn't get the
// asset, other hosts do. Both AssetRef entries come from the same Profile,
// which also exercises reduceGroup's "sticky beats soft within one scope"
// rule (the DEFAULT and DENIED entries both apply to "codex" and must not
// be reported as an intra-Profile conflict).
func TestResolve_HostsSelectorRefinement(t *testing.T) {
	profile := mustSkillProfile("company:example",
		domain.AssetRef{ID: "shared-skill", Intent: domain.IntentDefault},
		domain.AssetRef{ID: "shared-skill", Intent: domain.IntentDenied, Hosts: []string{"codex"}},
	)

	codexState, err := Resolve([]domain.Profile{profile}, domain.Activation{}, nil, "codex", fixedNow)
	if err != nil {
		t.Fatalf("Resolve(codex): %v", err)
	}
	if len(codexState.Conflicts) != 0 {
		t.Fatalf("Resolve(codex) conflicts = %+v, want none", codexState.Conflicts)
	}
	if codexState.IsActive(KindSkill, "shared-skill") {
		t.Error("codex is host-scoped DENIED and must not get shared-skill")
	}

	claudeState, err := Resolve([]domain.Profile{profile}, domain.Activation{}, nil, "claude-code", fixedNow)
	if err != nil {
		t.Fatalf("Resolve(claude-code): %v", err)
	}
	if len(claudeState.Conflicts) != 0 {
		t.Fatalf("Resolve(claude-code) conflicts = %+v, want none", claudeState.Conflicts)
	}
	if !claudeState.IsActive(KindSkill, "shared-skill") {
		t.Error("claude-code only sees the host-neutral DEFAULT and should get shared-skill")
	}
}

// TestResolve_RequiredDisabledOnlyViaAllowedException is the round-2 audit
// golden case: REQUIRED can be disabled only through an exception the
// defining policy allows.
func TestResolve_RequiredDisabledOnlyViaAllowedException(t *testing.T) {
	profile := mustSkillProfile("company:policy", domain.AssetRef{ID: "must-have", Intent: domain.IntentRequired})
	activation := domain.Activation{
		Spec: domain.ActivationSpec{Disable: domain.ActivationSelection{Skills: []string{"must-have"}}},
	}

	t.Run("exception absent: REQUIRED holds", func(t *testing.T) {
		state, err := Resolve([]domain.Profile{profile}, activation, nil, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !state.IsActive(KindSkill, "must-have") {
			t.Error("without an exception, REQUIRED must not be disabled by an Activation disable entry")
		}
	})

	t.Run("exception present and valid: disable succeeds", func(t *testing.T) {
		exceptions := []domain.Exception{{
			AssetID:       "must-have",
			Scope:         "company:policy",
			Justification: "temporary opt-out approved by policy owner",
			ExpiresAt:     fixedNow.Add(24 * time.Hour),
		}}
		state, err := Resolve([]domain.Profile{profile}, activation, exceptions, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if state.IsActive(KindSkill, "must-have") {
			t.Error("a valid, unexpired, policy-allowing exception should permit disabling a REQUIRED asset")
		}
	})

	t.Run("exception present but wrong scope: REQUIRED still holds", func(t *testing.T) {
		exceptions := []domain.Exception{{
			AssetID:       "must-have",
			Scope:         "some-other-policy",
			Justification: "not the defining policy",
			ExpiresAt:     fixedNow.Add(24 * time.Hour),
		}}
		state, err := Resolve([]domain.Profile{profile}, activation, exceptions, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !state.IsActive(KindSkill, "must-have") {
			t.Error("an exception scoped to a different policy must not permit disabling REQUIRED")
		}
	})
}

// TestResolve_DeniedPlusExceptionInterplay is the round-2 audit golden case:
// DENIED-plus-exception interplay, including an expired exception having no
// effect on resolution (same outcome as no exception at all).
func TestResolve_DeniedPlusExceptionInterplay(t *testing.T) {
	profile := mustMCPProfile("company:policy2", domain.AssetRef{ID: "banned-tool", Intent: domain.IntentDenied})
	activation := domain.Activation{
		Spec: domain.ActivationSpec{Enable: domain.ActivationSelection{MCPServers: []string{"banned-tool"}}},
	}

	t.Run("no exception: DENIED holds", func(t *testing.T) {
		state, err := Resolve([]domain.Profile{profile}, activation, nil, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if state.IsActive(KindMCPServer, "banned-tool") {
			t.Error("without an exception, DENIED must not be re-enabled")
		}
	})

	t.Run("valid unexpired exception: enable succeeds", func(t *testing.T) {
		exceptions := []domain.Exception{{
			AssetID:       "banned-tool",
			Scope:         "company:policy2",
			Justification: "security review granted a temporary allowance",
			ExpiresAt:     fixedNow.Add(1 * time.Hour),
		}}
		state, err := Resolve([]domain.Profile{profile}, activation, exceptions, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !state.IsActive(KindMCPServer, "banned-tool") {
			t.Error("a valid, unexpired, policy-allowing exception should permit re-enabling a DENIED asset")
		}
	})

	t.Run("expired exception: DENIED holds, same as no exception", func(t *testing.T) {
		exceptions := []domain.Exception{{
			AssetID:       "banned-tool",
			Scope:         "company:policy2",
			Justification: "allowance that has since lapsed",
			ExpiresAt:     fixedNow.Add(-1 * time.Hour),
		}}
		state, err := Resolve([]domain.Profile{profile}, activation, exceptions, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if state.IsActive(KindMCPServer, "banned-tool") {
			t.Error("an expired exception must not alter resolution: DENIED must still hold")
		}
	})
}

// TestResolve_AmbiguousConflict_RequiredVsDenied is the top-level golden
// case for the "ambiguous conflicts stay visible and block generation"
// acceptance criterion: two Profiles disagree (one REQUIRED, one DENIED)
// in a way init.md's per-intent table does not, by itself, resolve. Absent
// an exception, Resolve must surface a Conflict rather than silently
// picking a winner; a valid exception scoped to one side breaks the tie
// deterministically.
func TestResolve_AmbiguousConflict_RequiredVsDenied(t *testing.T) {
	requiring := mustSkillProfile("policy:requires", domain.AssetRef{ID: "contested", Intent: domain.IntentRequired})
	denying := mustSkillProfile("policy:denies", domain.AssetRef{ID: "contested", Intent: domain.IntentDenied})
	profiles := []domain.Profile{requiring, denying}

	t.Run("no exception: reported as a conflict, not silently resolved", func(t *testing.T) {
		state, err := Resolve(profiles, domain.Activation{}, nil, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !state.HasConflict(KindSkill, "contested") {
			t.Fatalf("expected a Conflict for contested; state = %+v", state)
		}
		if _, ok := state.Find(KindSkill, "contested"); ok {
			t.Error("a conflicted asset must not also appear as a decided ResolvedAsset")
		}
		var got Conflict
		for _, c := range state.Conflicts {
			if c.AssetID == "contested" {
				got = c
			}
		}
		if len(got.CandidateIntents) != 2 || got.CandidateIntents[0] != domain.IntentDenied || got.CandidateIntents[1] != domain.IntentRequired {
			t.Errorf("CandidateIntents = %v, want [DENIED REQUIRED]", got.CandidateIntents)
		}
	})

	t.Run("exception scoped to the denying policy: REQUIRED wins", func(t *testing.T) {
		exceptions := []domain.Exception{{
			AssetID: "contested", Scope: "policy:denies",
			Justification: "denial excepted for this asset", ExpiresAt: fixedNow.Add(time.Hour),
		}}
		state, err := Resolve(profiles, domain.Activation{}, exceptions, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if state.HasConflict(KindSkill, "contested") {
			t.Fatal("a valid exception scoped to the denying policy should resolve the conflict")
		}
		if !state.IsActive(KindSkill, "contested") {
			t.Error("REQUIRED should win once the DENIED side is excepted")
		}
	})

	t.Run("exception scoped to the requiring policy: DENIED wins", func(t *testing.T) {
		exceptions := []domain.Exception{{
			AssetID: "contested", Scope: "policy:requires",
			Justification: "requirement excepted for this asset", ExpiresAt: fixedNow.Add(time.Hour),
		}}
		state, err := Resolve(profiles, domain.Activation{}, exceptions, "claude-code", fixedNow)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if state.HasConflict(KindSkill, "contested") {
			t.Fatal("a valid exception scoped to the requiring policy should resolve the conflict")
		}
		if state.IsActive(KindSkill, "contested") {
			t.Error("DENIED should win once the REQUIRED side is excepted")
		}
	})
}

// TestResolve_IntraProfileSoftTie_IsAmbiguous covers reduceGroup's other
// conflict path: two soft (DEFAULT/AVAILABLE) entries for the same asset in
// the *same* Profile, both applicable to the target host. Neither
// documented rule prefers one over the other within a single scope.
func TestResolve_IntraProfileSoftTie_IsAmbiguous(t *testing.T) {
	profile := mustSkillProfile("company:example",
		domain.AssetRef{ID: "ambiguous-soft", Intent: domain.IntentDefault},
		domain.AssetRef{ID: "ambiguous-soft", Intent: domain.IntentAvailable, Hosts: []string{"codex"}},
	)
	state, err := Resolve([]domain.Profile{profile}, domain.Activation{}, nil, "codex", fixedNow)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !state.HasConflict(KindSkill, "ambiguous-soft") {
		t.Errorf("expected a Conflict for ambiguous-soft at codex; state = %+v", state)
	}
}
