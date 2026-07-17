package runtime

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestClassifyChange_AllEightRows proves ClassifyChange is total against
// docs/product/requirements.md §7's full table: every ChangeKind maps to
// the table's documented class, confirmation requirement, and Reachable
// flag (issue #19 round-2: "implement the classification function for ALL
// EIGHT rows").
func TestClassifyChange_AllEightRows(t *testing.T) {
	cases := []struct {
		kind          ChangeKind
		wantClass     ConfirmationClass
		wantRequires  bool
		wantReachable bool
	}{
		{ChangeSelectReviewedInstruction, ConfirmationAutoStage, false, true},
		{ChangeSelectReviewedSkill, ConfirmationAutoStage, false, true},
		{ChangeModelOrDisplayPreference, ConfirmationAutoStage, false, false},
		{ChangeEnableMCPServer, ConfirmationWithDetail, true, true},
		{ChangeEnableHookPluginExtension, ConfirmationAlways, true, false},
		{ChangeExpandAccess, ConfirmationAlways, true, true},
		{ChangeModifySharedProfile, ConfirmationReviewableDiff, true, false},
		{ChangeImportNativeCredential, ConfirmationProhibited, true, false},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			got := ClassifyChange(ProposedChange{Kind: c.kind, AssetID: "example"})
			if got.Class != c.wantClass {
				t.Errorf("Class = %q, want %q", got.Class, c.wantClass)
			}
			if got.RequiresConfirmation != c.wantRequires {
				t.Errorf("RequiresConfirmation = %v, want %v", got.RequiresConfirmation, c.wantRequires)
			}
			if got.Reachable != c.wantReachable {
				t.Errorf("Reachable = %v, want %v", got.Reachable, c.wantReachable)
			}
			if got.Explanation == "" {
				t.Error("Explanation is empty")
			}
		})
	}
}

// TestConfirmation_AlreadyReviewedInstructionsAndSkills_AutoStage is the
// round-2 addendum's own first named class: "already-reviewed Instructions/
// Skills stage automatically."
func TestConfirmation_AlreadyReviewedInstructionsAndSkills_AutoStage(t *testing.T) {
	changes := []ProposedChange{
		{Kind: ChangeSelectReviewedInstruction, AssetID: "engineering-baseline"},
		{Kind: ChangeSelectReviewedSkill, AssetID: "code-review"},
	}
	if err := RequireConfirmation(changes, nil); err != nil {
		t.Fatalf("RequireConfirmation for auto-stage changes with no confirmation supplied: want nil, got %v", err)
	}
}

// TestConfirmation_EnableMCPServer_RequiresDetailConfirmation is the round-2
// addendum's own second named class: "enabling an MCP server confirms
// command, network destinations, and secret references."
func TestConfirmation_EnableMCPServer_RequiresDetailConfirmation(t *testing.T) {
	change := ProposedChange{Kind: ChangeEnableMCPServer, AssetID: "internal-docs", Detail: map[string]string{"reason": "resolved desired state"}}

	err := RequireConfirmation([]ProposedChange{change}, nil)
	if err == nil {
		t.Fatal("RequireConfirmation for an MCP server enable with no confirmation supplied: want an error, got nil")
	}
	var confErr *ConfirmationRequiredError
	if !errors.As(err, &confErr) {
		t.Fatalf("error = %v (%T), want a *ConfirmationRequiredError", err, err)
	}
	if len(confErr.Requirements) != 1 {
		t.Fatalf("Requirements has %d entries, want 1", len(confErr.Requirements))
	}
	req := confErr.Requirements[0]
	if req.Class != ConfirmationWithDetail {
		t.Errorf("Class = %q, want %q", req.Class, ConfirmationWithDetail)
	}
	wantKeys := map[string]bool{"command": true, "networkDestinations": true, "secretReferences": true}
	if len(req.RequiredDetailKeys) != len(wantKeys) {
		t.Fatalf("RequiredDetailKeys = %v, want exactly %v", req.RequiredDetailKeys, wantKeys)
	}
	for _, k := range req.RequiredDetailKeys {
		if !wantKeys[k] {
			t.Errorf("unexpected RequiredDetailKeys entry %q", k)
		}
	}

	// Once explicitly confirmed, the same change must be allowed through.
	confirmed := map[ConfirmationKey]bool{change.Key(): true}
	if err := RequireConfirmation([]ProposedChange{change}, confirmed); err != nil {
		t.Errorf("RequireConfirmation after explicit confirmation: want nil, got %v", err)
	}
}

// TestConfirmation_HookAndPermissionExpansion_AlwaysConfirm is the round-2
// addendum's own third named class: "Hook activation and permission
// expansion always confirm."
func TestConfirmation_HookAndPermissionExpansion_AlwaysConfirm(t *testing.T) {
	changes := []ProposedChange{
		{Kind: ChangeEnableHookPluginExtension, AssetID: "some-hook"},
		{Kind: ChangeExpandAccess, AssetID: "sandbox"},
	}
	err := RequireConfirmation(changes, nil)
	if err == nil {
		t.Fatal("RequireConfirmation for a Hook enable and a permission expansion with no confirmation supplied: want an error, got nil")
	}
	var confErr *ConfirmationRequiredError
	if !errors.As(err, &confErr) {
		t.Fatalf("error = %v (%T), want a *ConfirmationRequiredError", err, err)
	}
	if len(confErr.Requirements) != 2 {
		t.Fatalf("Requirements has %d entries, want 2", len(confErr.Requirements))
	}
	for _, r := range confErr.Requirements {
		if r.Class != ConfirmationAlways {
			t.Errorf("Class = %q, want %q", r.Class, ConfirmationAlways)
		}
	}

	confirmed := map[ConfirmationKey]bool{changes[0].Key(): true, changes[1].Key(): true}
	if err := RequireConfirmation(changes, confirmed); err != nil {
		t.Errorf("RequireConfirmation after explicit confirmation: want nil, got %v", err)
	}
}

// TestConfirmation_ModifySharedProfile_ReviewableDiff and
// TestConfirmation_ImportNativeCredential_Prohibited round out the
// remaining two §7 rows this PR classifies but does not wire into any real
// activation code path (no shared-Profile-editing UI and no credential-
// import path exist yet -- see ClassifyChange's own doc comment).
func TestConfirmation_ModifySharedProfile_ReviewableDiff(t *testing.T) {
	req := ClassifyChange(ProposedChange{Kind: ChangeModifySharedProfile, AssetID: "company:example"})
	if req.Class != ConfirmationReviewableDiff {
		t.Errorf("Class = %q, want %q", req.Class, ConfirmationReviewableDiff)
	}
	if req.Reachable {
		t.Error("Reachable = true, want false: no shared-Profile-editing code path exists yet")
	}
}

func TestConfirmation_ImportNativeCredential_Prohibited(t *testing.T) {
	req := ClassifyChange(ProposedChange{Kind: ChangeImportNativeCredential, AssetID: "auth.json"})
	if req.Class != ConfirmationProhibited {
		t.Errorf("Class = %q, want %q", req.Class, ConfirmationProhibited)
	}
	if req.Reachable {
		t.Error("Reachable = true, want false: no credential-import code path exists anywhere in this repository")
	}
}

// TestDiffProposedChanges_NewMCPServerSkillAndPermission_ClassifiedCorrectly
// is the real integration between Compile's output and the confirmation
// machinery: a pending generation that newly activates an mcpServer, a
// skill, and a permission (relative to an empty "current") produces exactly
// the ProposedChanges RequireConfirmation then gates on.
func TestDiffProposedChanges_NewMCPServerSkillAndPermission_ClassifiedCorrectly(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{
		{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       "Profile",
			Metadata:   domain.Metadata{ID: "company:example"},
			Spec: domain.ProfileSpec{
				Assets: domain.ProfileAssets{
					Skills:     []domain.AssetRef{{ID: "code-review", Intent: domain.IntentRequired}},
					MCPServers: []domain.AssetRef{{ID: "internal-docs", Intent: domain.IntentRequired}},
				},
				Policy: domain.ProfilePolicy{
					Permissions: map[string]domain.PermissionRef{
						"sandbox": {Intent: domain.IntentRequired, Value: "workspace-write"},
					},
				},
			},
		},
	}
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)
	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	changes := DiffProposedChanges(domain.Generation{}, gen, "codex")

	kinds := map[ChangeKind]bool{}
	for _, c := range changes {
		kinds[c.Kind] = true
	}
	for _, want := range []ChangeKind{ChangeEnableMCPServer, ChangeSelectReviewedSkill, ChangeExpandAccess} {
		if !kinds[want] {
			t.Errorf("DiffProposedChanges did not include %q; got %+v", want, changes)
		}
	}

	if err := RequireConfirmation(changes, nil); err == nil {
		t.Fatal("RequireConfirmation over an unconfirmed mcpServer+skill+permission diff: want an error, got nil")
	}
	confirmed := make(map[ConfirmationKey]bool, len(changes))
	for _, c := range changes {
		confirmed[c.Key()] = true
	}
	if err := RequireConfirmation(changes, confirmed); err != nil {
		t.Errorf("RequireConfirmation after confirming every required class: want nil, got %v", err)
	}
}

// TestRequireConfirmation_ConfirmationIsPerAsset_NotPerKind is a regression
// test for a real Copilot review finding on this PR: RequireConfirmation
// used to key its confirmed set by ChangeKind alone, so confirming ONE MCP
// server enable would silently also satisfy confirmation for every OTHER
// MCP server enable in the same activation -- exactly the weakened-safety-
// property bug docs/product/requirements.md §7 exists to prevent (an
// operator reviews and approves one specific server's command/network/
// secret exposure, not "MCP servers in general"). This proves confirming
// server A does NOT satisfy the requirement for server B.
func TestRequireConfirmation_ConfirmationIsPerAsset_NotPerKind(t *testing.T) {
	changeA := ProposedChange{Kind: ChangeEnableMCPServer, AssetID: "server-a", Host: "codex"}
	changeB := ProposedChange{Kind: ChangeEnableMCPServer, AssetID: "server-b", Host: "codex"}
	changes := []ProposedChange{changeA, changeB}

	confirmed := map[ConfirmationKey]bool{changeA.Key(): true} // only server-a confirmed

	err := RequireConfirmation(changes, confirmed)
	if err == nil {
		t.Fatal("RequireConfirmation with only server-a confirmed: want an error naming server-b, got nil")
	}
	var confErr *ConfirmationRequiredError
	if !errors.As(err, &confErr) {
		t.Fatalf("error = %v (%T), want a *ConfirmationRequiredError", err, err)
	}
	if len(confErr.Changes) != 1 || confErr.Changes[0].AssetID != "server-b" {
		t.Fatalf("outstanding changes = %+v, want exactly one entry for server-b", confErr.Changes)
	}

	// Confirming both by their distinct keys clears the requirement.
	confirmed[changeB.Key()] = true
	if err := RequireConfirmation(changes, confirmed); err != nil {
		t.Errorf("RequireConfirmation after confirming both server-a and server-b: want nil, got %v", err)
	}
}
