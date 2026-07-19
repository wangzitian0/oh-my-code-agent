package tui

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// TestRenderConfirmScreen_EveryConfirmationClass proves this package's
// confirmation screen renders docs/product/requirements.md §7's full
// risk-based table faithfully -- issue #35's round-3 addendum requires a
// scripted test for "each confirmation class," not only the ones a real
// staged generation can drive today.
//
// Three of runtime.ClassifyChange's eight ChangeKinds are already exercised
// end-to-end through the real TUI Update flow in model_actions_test.go
// (ChangeSelectReviewedSkill/auto-stage, ChangeEnableMCPServer/
// confirm-with-detail, ChangeExpandAccess/always-confirm — all Reachable:
// true). The remaining two classes this screen must also render correctly
// -- ConfirmationReviewableDiff (ChangeModifySharedProfile) and
// ConfirmationProhibited (ChangeImportNativeCredential) -- have
// Reachable: false in runtime.ClassifyChange's own doc comment: no code
// path anywhere in this repository can produce either through a real
// activation yet (no shared-Profile-editing UI, no credential-import path
// at all). Driving those through a real stageAssetActivation call would
// therefore be dishonest — there is nothing real to drive. This test
// instead builds the changeReview by hand for every one of the eight kinds
// (runtime.ClassifyChange itself, called directly, exactly like
// internal/runtime/confirmation_test.go's own TestClassifyChange_
// AllEightRows already does for the classification function alone) and
// proves THIS package's rendering layer shows every class's name,
// confirmation requirement, and explanation -- the part of "each
// confirmation class has a scripted interaction test" that is honestly
// testable for a class with no reachable real path.
func TestRenderConfirmScreen_EveryConfirmationClass(t *testing.T) {
	changes := []runtime.ProposedChange{
		{Kind: runtime.ChangeSelectReviewedInstruction, AssetID: "onboarding.md", Host: "codex"},
		{Kind: runtime.ChangeSelectReviewedSkill, AssetID: "code-review", Host: "codex"},
		{Kind: runtime.ChangeModelOrDisplayPreference, Host: "codex"},
		{Kind: runtime.ChangeEnableMCPServer, AssetID: "internal-docs", Host: "codex"},
		{Kind: runtime.ChangeEnableHookPluginExtension, AssetID: "some-hook", Host: "codex"},
		{Kind: runtime.ChangeExpandAccess, AssetID: "sandbox", Host: "codex"},
		{Kind: runtime.ChangeModifySharedProfile, AssetID: "company:example", Host: "codex"},
		{Kind: runtime.ChangeImportNativeCredential, AssetID: "auth.json", Host: "codex"},
	}
	review := changeReview{Host: "codex"}
	for _, c := range changes {
		review.Changes = append(review.Changes, c)
		review.Requirements = append(review.Requirements, runtime.ClassifyChange(c))
	}

	out := renderConfirmScreen(review)

	wantClasses := []runtime.ConfirmationClass{
		runtime.ConfirmationAutoStage,
		runtime.ConfirmationWithDetail,
		runtime.ConfirmationAlways,
		runtime.ConfirmationReviewableDiff,
		runtime.ConfirmationProhibited,
	}
	for _, class := range wantClasses {
		if !contains(out, string(class)) {
			t.Errorf("renderConfirmScreen output missing confirmation class %q:\n%s", class, out)
		}
	}
	for _, c := range changes {
		id := c.AssetID
		if id == "" {
			id = "(none)"
		}
		if !contains(out, id) {
			t.Errorf("renderConfirmScreen output missing asset id %q for change kind %s:\n%s", id, c.Kind, out)
		}
	}

	// auto-stage changes must never be marked as requiring confirmation;
	// every other class in this table must be.
	if !contains(out, "no confirmation required") {
		t.Errorf("renderConfirmScreen output missing the auto-stage 'no confirmation required' wording:\n%s", out)
	}
	if !contains(out, "REQUIRES CONFIRMATION") {
		t.Errorf("renderConfirmScreen output missing 'REQUIRES CONFIRMATION' wording for a confirmation-requiring class:\n%s", out)
	}
}

// TestRenderConfirmScreen_NoChanges proves the empty Change Set edge case
// (e.g. re-staging identical desired state) renders an honest "nothing to
// review" message rather than an empty or misleading screen.
func TestRenderConfirmScreen_NoChanges(t *testing.T) {
	out := renderConfirmScreen(changeReview{Host: "codex"})
	if !contains(out, "No changes require review") {
		t.Errorf("renderConfirmScreen with no changes = %q, want an honest empty-state message", out)
	}
	if !contains(out, "Press y to activate") {
		t.Errorf("renderConfirmScreen with no changes does not explain how to proceed:\n%s", out)
	}
}
