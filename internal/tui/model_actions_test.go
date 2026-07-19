package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// This file's tests are issue #35's own explicit, non-negotiable bar (round-3
// final review addendum): scripted interaction tests -- constructing a
// Model, feeding it tea.KeyMsg values via Update, and asserting the
// resulting Model state/View() output, mirroring model_test.go's own
// PR-30 style exactly -- for every confirmation class and the stage/
// restart/rollback flows, driven against REAL, sandboxed
// internal/runtime/internal/profiles machinery end-to-end (actiontest_
// helpers_test.go's setupActionTestEnv), never a mocked-out activation
// path.

func fixedClock(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

func pressKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return next.(Model)
}

const skillAvailableProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    skills:
      - id: code-review
        intent: AVAILABLE
`

const skillAndPermissionProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    skills:
      - id: code-review
        intent: AVAILABLE
  policy:
    permissions:
      sandbox:
        intent: REQUIRED
        value: workspace-write
`

const mcpServerAvailableProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    mcpServers:
      - id: internal-docs
        intent: AVAILABLE
`

// newActionModel builds a Model over ctx's freshly-built report.Artifact
// (refreshArtifact -- the same real detect-observe-compose-Build pipeline
// `omca`'s TUI entry point uses), with ctx and now attached, active on the
// given view.
func newActionModel(t *testing.T, ctx ActionContext, now time.Time, active ViewKind) Model {
	t.Helper()
	artifact, err := refreshArtifact(ctx, now)
	if err != nil {
		t.Fatalf("refreshArtifact: %v", err)
	}
	return NewModel(artifact).WithActionContext(ctx).WithClock(fixedClock(now)).SetActive(active)
}

// TestModel_ActivateAvailableSkill_AutoStageClass_OneApprovalActivates is
// issue #35's own worked scenario: "activate an AVAILABLE asset ->
// confirmation screen appears listing the right classes -> approve ->
// pending is staged." code-review is a skill with Profile intent AVAILABLE
// (not yet Active) -- runtime.ClassifyChange's auto-stage class, requiring
// no confirmation at all, the SAME kind cmd/omca/activate.go's `omca
// activate` allows through without any --confirm flag.
func TestModel_ActivateAvailableSkill_AutoStageClass_OneApprovalActivates(t *testing.T) {
	ctx := setupActionTestEnv(t, skillAvailableProfileYAML)
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewAssets)

	// Before activating: the Assets view's own hint line names the exact
	// asset 'a' is about to stage.
	if !contains(m.View(), `activate skill "code-review"`) {
		t.Fatalf("Assets view does not show the expected activation hint:\n%s", m.View())
	}

	m = pressKey(t, m, "a")
	if m.mode != modeConfirm {
		t.Fatalf("mode after 'a' = %v, want modeConfirm", m.mode)
	}
	if m.review == nil {
		t.Fatal("review is nil after staging")
	}
	if len(m.review.Changes) != 1 {
		t.Fatalf("len(review.Changes) = %d, want 1: %+v", len(m.review.Changes), m.review.Changes)
	}
	change := m.review.Changes[0]
	req := m.review.Requirements[0]
	if change.Kind != runtime.ChangeSelectReviewedSkill || change.AssetID != "code-review" {
		t.Errorf("change = %+v, want Kind=%s AssetID=code-review", change, runtime.ChangeSelectReviewedSkill)
	}
	if req.Class != runtime.ConfirmationAutoStage || req.RequiresConfirmation {
		t.Errorf("requirement = %+v, want Class=%s RequiresConfirmation=false", req, runtime.ConfirmationAutoStage)
	}

	view := m.View()
	if !contains(view, "Review Change Set") || !contains(view, "code-review") || !contains(view, string(runtime.ConfirmationAutoStage)) || !contains(view, "no confirmation required") {
		t.Errorf("confirm screen does not render the auto-stage change as expected:\n%s", view)
	}
	if contains(view, "REQUIRES CONFIRMATION") {
		t.Errorf("confirm screen wrongly marks an auto-stage change as requiring confirmation:\n%s", view)
	}

	wantGenID := m.pendingGen.Metadata.ID
	if wantGenID == "" {
		t.Fatal("pendingGen.Metadata.ID is empty before approval")
	}

	m = pressKey(t, m, "y")
	if m.mode != modeBrowse {
		t.Errorf("mode after 'y' = %v, want modeBrowse", m.mode)
	}
	if !contains(m.message, "activated") {
		t.Errorf("message after approval = %q, want it to report activation", m.message)
	}

	curDir, err := runtime.CurrentGenerationDir(ctx.WorktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	curGen, err := runtime.ReadGenerationManifest(curDir)
	if err != nil {
		t.Fatalf("ReadGenerationManifest: %v", err)
	}
	if curGen.Metadata.ID != wantGenID {
		t.Errorf("current generation after approval = %s, want the staged generation %s", curGen.Metadata.ID, wantGenID)
	}

	// No OMCA_RUN_ID was set in this env, so there is genuinely no
	// managed-session signal to report for any host -- restartStatuses must
	// stay empty rather than fabricating a verdict (restartStatusForHost's
	// own "nothing to report" contract).
	if len(m.restartStatuses) != 0 {
		t.Errorf("restartStatuses = %+v, want empty (no OMCA_RUN_ID in this session)", m.restartStatuses)
	}
	if contains(m.message, "RESTART REQUIRED") {
		t.Errorf("message wrongly claims a restart is required with no managed-session signal: %q", m.message)
	}
}

// TestModel_ActivateSkillWithRequiredPermission_AutoStageAndAlwaysConfirm_
// OneApprovalActivatesBoth proves issue #35's "one human approval can
// execute a complete reviewed Change Set" AC across TWO DIFFERENT
// confirmation classes in a single Change Set: activating the AVAILABLE
// code-review skill also, in the SAME staged generation, newly includes a
// REQUIRED sandbox permission the Profile declares independently of any
// selection (runtime.ClassifyChange's always-confirm class,
// ChangeExpandAccess) -- a single 'y' keypress must approve and activate
// BOTH at once, not one at a time.
func TestModel_ActivateSkillWithRequiredPermission_AutoStageAndAlwaysConfirm_OneApprovalActivatesBoth(t *testing.T) {
	ctx := setupActionTestEnv(t, skillAndPermissionProfileYAML)
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewAssets)

	m = pressKey(t, m, "a")
	if m.mode != modeConfirm || m.review == nil {
		t.Fatalf("mode/review after 'a' = %v/%v, want modeConfirm/non-nil", m.mode, m.review)
	}
	if len(m.review.Changes) != 2 {
		t.Fatalf("len(review.Changes) = %d, want 2 (skill + permission): %+v", len(m.review.Changes), m.review.Changes)
	}

	var sawSkill, sawPermission bool
	for i, c := range m.review.Changes {
		req := m.review.Requirements[i]
		switch c.Kind {
		case runtime.ChangeSelectReviewedSkill:
			sawSkill = true
			if req.Class != runtime.ConfirmationAutoStage || req.RequiresConfirmation {
				t.Errorf("skill change requirement = %+v, want auto-stage/no confirmation", req)
			}
		case runtime.ChangeExpandAccess:
			sawPermission = true
			if req.Class != runtime.ConfirmationAlways || !req.RequiresConfirmation {
				t.Errorf("permission change requirement = %+v, want always-confirm/requires confirmation", req)
			}
			if c.AssetID != "sandbox" {
				t.Errorf("permission change AssetID = %q, want %q", c.AssetID, "sandbox")
			}
		default:
			t.Errorf("unexpected change kind %s", c.Kind)
		}
	}
	if !sawSkill || !sawPermission {
		t.Fatalf("did not see both expected changes: sawSkill=%v sawPermission=%v (%+v)", sawSkill, sawPermission, m.review.Changes)
	}

	view := m.View()
	if !contains(view, string(runtime.ConfirmationAutoStage)) || !contains(view, string(runtime.ConfirmationAlways)) {
		t.Errorf("confirm screen does not show both confirmation classes:\n%s", view)
	}

	m = pressKey(t, m, "y")
	if !contains(m.message, "activated") {
		t.Fatalf("single approval did not activate the combined Change Set: message=%q", m.message)
	}

	curDir, err := runtime.CurrentGenerationDir(ctx.WorktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	curGen, err := runtime.ReadGenerationManifest(curDir)
	if err != nil {
		t.Fatalf("ReadGenerationManifest: %v", err)
	}
	var sawSkillSource, sawPermissionSource bool
	for _, s := range curGen.Spec.Sources {
		if s.Host != "codex" || !s.Included {
			continue
		}
		switch {
		case s.Concept == "skill" && s.Source == "code-review":
			sawSkillSource = true
		case s.Concept == "permission" && s.Source == "sandbox":
			sawPermissionSource = true
		}
	}
	if !sawSkillSource || !sawPermissionSource {
		t.Errorf("activated generation's Sources do not include both the skill and the permission: %+v", curGen.Spec.Sources)
	}
}

// TestModel_ActivateAvailableMCPServer_ConfirmWithDetailClass proves the
// confirm-with-detail class (runtime.ClassifyChange's "enabling an MCP
// server" row) renders and gates correctly: internal-docs is an mcpServer
// with Profile intent AVAILABLE.
func TestModel_ActivateAvailableMCPServer_ConfirmWithDetailClass(t *testing.T) {
	ctx := setupActionTestEnv(t, mcpServerAvailableProfileYAML)
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewAssets)

	m = pressKey(t, m, "a")
	if m.mode != modeConfirm || m.review == nil {
		t.Fatalf("mode/review after 'a' = %v/%v", m.mode, m.review)
	}
	if len(m.review.Changes) != 1 {
		t.Fatalf("len(review.Changes) = %d, want 1: %+v", len(m.review.Changes), m.review.Changes)
	}
	change, req := m.review.Changes[0], m.review.Requirements[0]
	if change.Kind != runtime.ChangeEnableMCPServer || change.AssetID != "internal-docs" {
		t.Errorf("change = %+v, want Kind=%s AssetID=internal-docs", change, runtime.ChangeEnableMCPServer)
	}
	if req.Class != runtime.ConfirmationWithDetail || !req.RequiresConfirmation {
		t.Errorf("requirement = %+v, want Class=%s RequiresConfirmation=true", req, runtime.ConfirmationWithDetail)
	}

	view := m.View()
	if !contains(view, string(runtime.ConfirmationWithDetail)) || !contains(view, "internal-docs") || !contains(view, "REQUIRES CONFIRMATION") {
		t.Errorf("confirm screen does not render the confirm-with-detail change as expected:\n%s", view)
	}

	m = pressKey(t, m, "y")
	if !contains(m.message, "activated") {
		t.Fatalf("approval did not activate: message=%q", m.message)
	}
	if _, err := runtime.CurrentGenerationDir(ctx.WorktreeStateDir, "codex"); err != nil {
		t.Fatalf("CurrentGenerationDir after activation: %v", err)
	}
}

// TestModel_CancelReview_LeavesPendingStagedAndCurrentUntouched proves the
// 'n'/esc cancel path: the pending generation stays staged (an operator can
// still activate it later, e.g. via `omca activate`), but nothing is
// activated.
func TestModel_CancelReview_LeavesPendingStagedAndCurrentUntouched(t *testing.T) {
	ctx := setupActionTestEnv(t, mcpServerAvailableProfileYAML)
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewAssets)

	m = pressKey(t, m, "a")
	if m.mode != modeConfirm {
		t.Fatalf("mode after 'a' = %v, want modeConfirm", m.mode)
	}
	wantPendingID := m.pendingGen.Metadata.ID

	m = pressKey(t, m, "n")
	if m.mode != modeBrowse || m.review != nil {
		t.Fatalf("mode/review after 'n' = %v/%v, want modeBrowse/nil", m.mode, m.review)
	}
	if !contains(m.message, "cancelled") || !contains(m.message, "remains staged") {
		t.Errorf("message after cancel = %q, want it to report cancellation and that pending remains staged", m.message)
	}

	if _, err := runtime.CurrentGenerationDir(ctx.WorktreeStateDir, "codex"); err == nil {
		t.Error("current generation exists after a cancelled review; activation must not have proceeded")
	}
	pendingDir, err := runtime.PendingGenerationDir(ctx.WorktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("PendingGenerationDir after cancel: %v", err)
	}
	pendingGen, err := runtime.ReadGenerationManifest(pendingDir)
	if err != nil {
		t.Fatalf("ReadGenerationManifest after cancel: %v", err)
	}
	if pendingGen.Metadata.ID != wantPendingID {
		t.Errorf("pending generation after cancel = %s, want it to remain %s", pendingGen.Metadata.ID, wantPendingID)
	}
}

// TestModel_ActivateThenRollback_RestoresParent is issue #35's own
// stage/rollback flow, driven end-to-end through the TUI: stage+activate
// generation A (code-review skill), stage+activate a second generation B
// after the Profile evolves, then roll back from the Generations view and
// prove current reverts to A -- mirroring cmd/omca/activate_test.go's
// TestRunRollback_RestoresParent's own real-world sequence, but driven by
// Model.Update key presses instead of CLI flags.
func TestModel_ActivateThenRollback_RestoresParent(t *testing.T) {
	ctx := setupActionTestEnv(t, skillAvailableProfileYAML)
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewAssets)

	// First activation: generation A.
	m = pressKey(t, m, "a")
	if m.mode != modeConfirm {
		t.Fatalf("mode after first 'a' = %v, want modeConfirm", m.mode)
	}
	m = pressKey(t, m, "y")
	if !contains(m.message, "activated") {
		t.Fatalf("first activation failed: message=%q", m.message)
	}
	genADir, err := runtime.CurrentGenerationDir(ctx.WorktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir after first activation: %v", err)
	}
	genA, err := runtime.ReadGenerationManifest(genADir)
	if err != nil {
		t.Fatalf("ReadGenerationManifest(genA): %v", err)
	}

	// The Profile evolves: a second AVAILABLE skill appears (code-review is
	// now Active, so firstActivatableCandidate must offer the new one, not
	// re-offer code-review).
	rewriteProfile(t, ctx, `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    skills:
      - id: code-review
        intent: AVAILABLE
      - id: another-skill
        intent: AVAILABLE
`)
	// m.Artifact was refreshed by approveReview above, but it was built
	// BEFORE the Profile rewrite; re-refresh so firstActivatableCandidate
	// sees the new Desired Graph, exactly like a human would see after
	// reopening/refreshing the TUI.
	fresh, err := refreshArtifact(ctx, now)
	if err != nil {
		t.Fatalf("refreshArtifact after Profile rewrite: %v", err)
	}
	m.Artifact = fresh

	m2 := pressKey(t, m, "a")
	if m2.mode != modeConfirm || m2.review == nil {
		t.Fatalf("mode/review after second 'a' = %v/%v", m2.mode, m2.review)
	}
	if len(m2.review.Changes) != 1 || m2.review.Changes[0].AssetID != "another-skill" {
		t.Fatalf("second review.Changes = %+v, want exactly one change naming another-skill", m2.review.Changes)
	}
	m2 = pressKey(t, m2, "y")
	if !contains(m2.message, "activated") {
		t.Fatalf("second activation failed: message=%q", m2.message)
	}

	genBDir, err := runtime.CurrentGenerationDir(ctx.WorktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir after second activation: %v", err)
	}
	genB, err := runtime.ReadGenerationManifest(genBDir)
	if err != nil {
		t.Fatalf("ReadGenerationManifest(genB): %v", err)
	}
	if genB.Metadata.ID == genA.Metadata.ID {
		t.Fatal("second activation did not actually produce a new generation")
	}
	if genB.Metadata.Parent == nil || *genB.Metadata.Parent != genA.Metadata.ID {
		got := "nil"
		if genB.Metadata.Parent != nil {
			got = *genB.Metadata.Parent
		}
		t.Fatalf("genB.Metadata.Parent = %s, want genA's ID %q", got, genA.Metadata.ID)
	}

	// Roll back from the Generations view.
	m3 := m2.SetActive(ViewGenerations)
	m3 = pressKey(t, m3, "r")
	if !contains(m3.message, "rolled back") {
		t.Fatalf("rollback did not report success: message=%q", m3.message)
	}

	afterDir, err := runtime.CurrentGenerationDir(ctx.WorktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir after rollback: %v", err)
	}
	afterGen, err := runtime.ReadGenerationManifest(afterDir)
	if err != nil {
		t.Fatalf("ReadGenerationManifest after rollback: %v", err)
	}
	if afterGen.Metadata.ID != genA.Metadata.ID {
		t.Errorf("current generation after rollback = %s, want the parent generation %s", afterGen.Metadata.ID, genA.Metadata.ID)
	}
}

// TestModel_ApproveReview_RestartRequired_ShownPerHost proves issue #35's
// "restart_required per host" AC using the SAME env-var signal
// cmd/omca/doctor.go's checkRestartRequired reads (OMCA_RUN_ID plus this
// host's own native-home env var): a session recorded as still running
// generation "stale-session-generation" is, after a real activation moves
// codex's own current generation to something else, correctly reported as
// requiring a restart.
func TestModel_ApproveReview_RestartRequired_ShownPerHost(t *testing.T) {
	ctx := setupActionTestEnv(t, skillAvailableProfileYAML)
	ctx.Env.Vars = append(ctx.Env.Vars, "OMCA_RUN_ID=stale-session-generation", "CODEX_HOME=/tmp/does-not-matter-for-this-test")
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewAssets)

	m = pressKey(t, m, "a")
	m = pressKey(t, m, "y")
	if !contains(m.message, "activated") {
		t.Fatalf("activation failed: message=%q", m.message)
	}

	if len(m.restartStatuses) != 1 {
		t.Fatalf("restartStatuses = %+v, want exactly one entry for codex", m.restartStatuses)
	}
	status := m.restartStatuses[0]
	if status.Host != "codex" {
		t.Errorf("restartStatuses[0].Host = %q, want codex", status.Host)
	}
	if !status.RestartRequired {
		t.Errorf("RestartRequired = false, want true (session generation %q != freshly-activated generation)", status.SessionGenerationID)
	}
	if !contains(m.message, "RESTART REQUIRED") {
		t.Errorf("message does not mention the restart requirement: %q", m.message)
	}
}

// TestModel_ActivateSelected_NoActionContext_IsInert proves the read-only
// backward-compatibility contract ActionContext.enabled documents: a Model
// with no ActionContext attached (every PR-30 test, and any caller that
// never opts in) leaves the 'a'/'r' keys as harmless no-ops that report a
// clear message rather than panicking on empty paths.
func TestModel_ActivateSelected_NoActionContext_IsInert(t *testing.T) {
	m := NewModel(loadFixtureArtifact(t)).SetActive(ViewAssets)
	m = pressKey(t, m, "a")
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse (no ActionContext attached)", m.mode)
	}
	if !contains(m.message, "not available") {
		t.Errorf("message = %q, want an explanation that actions are unavailable", m.message)
	}

	m2 := NewModel(loadFixtureArtifact(t)).SetActive(ViewGenerations)
	m2 = pressKey(t, m2, "r")
	if !contains(m2.message, "not available") {
		t.Errorf("message = %q, want an explanation that actions are unavailable", m2.message)
	}
}

// TestModel_ActivateSelected_NoAvailableAsset_ReportsMessage proves the
// honest "nothing to do" path: a host with no AVAILABLE-and-not-yet-Active
// skill/mcpServer asset leaves Model in modeBrowse with an explanatory
// message, never a confirm screen for nothing.
func TestModel_ActivateSelected_NoAvailableAsset_ReportsMessage(t *testing.T) {
	ctx := setupActionTestEnv(t, "") // no Profile at all -> nothing AVAILABLE
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewAssets)

	m = pressKey(t, m, "a")
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse", m.mode)
	}
	if !contains(m.message, "no AVAILABLE") {
		t.Errorf("message = %q, want it to explain there is nothing AVAILABLE to activate", m.message)
	}
}

// TestModel_RollbackSelected_NoParent_ReportsError proves rollbackSelected
// surfaces a real runtime.Rollback failure (no current generation at all
// yet) as a message rather than panicking or silently succeeding.
func TestModel_RollbackSelected_NoParent_ReportsError(t *testing.T) {
	ctx := setupActionTestEnv(t, "")
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewGenerations)

	m = pressKey(t, m, "r")
	if !contains(m.message, "rollback failed") {
		t.Errorf("message = %q, want it to report the rollback failure", m.message)
	}
}

// TestModel_HostCursor_WrapsBothDirections proves up/down (and k/j) cycle
// hostCursor across every host in Artifact.Debug, wrapping in both
// directions -- the shared navigation Assets' activate action and
// Generations' rollback action both key off (currentHost).
func TestModel_HostCursor_WrapsBothDirections(t *testing.T) {
	a := loadFixtureArtifact(t) // single host: "codex" (fixture_test.go)
	m := NewModel(a)
	host, ok := m.currentHost()
	if !ok || host != "codex" {
		t.Fatalf("currentHost() = %q, %v, want codex, true", host, ok)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := next.(Model)
	host2, ok2 := m2.currentHost()
	if !ok2 || host2 != "codex" {
		t.Errorf("currentHost() after down = %q, %v, want codex, true (only one host, must wrap to itself)", host2, ok2)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m3 := next.(Model)
	host3, ok3 := m3.currentHost()
	if !ok3 || host3 != "codex" {
		t.Errorf("currentHost() after up = %q, %v, want codex, true", host3, ok3)
	}
}

// TestModel_ConfirmMode_IgnoresNavigationKeys proves modeConfirm blocks
// ordinary view navigation until the review is explicitly approved or
// cancelled -- an operator must see and act on the whole Change Set before
// doing anything else.
func TestModel_ConfirmMode_IgnoresNavigationKeys(t *testing.T) {
	ctx := setupActionTestEnv(t, skillAvailableProfileYAML)
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewAssets)
	m = pressKey(t, m, "a")
	if m.mode != modeConfirm {
		t.Fatalf("mode after 'a' = %v, want modeConfirm", m.mode)
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m2 := next.(Model)
	if cmd != nil {
		t.Errorf("Update(right) during modeConfirm returned a non-nil Cmd")
	}
	if m2.active != ViewAssets || m2.mode != modeConfirm {
		t.Errorf("Update(right) during modeConfirm changed state: active=%v mode=%v, want unchanged", m2.active, m2.mode)
	}
}

// TestModel_ConfirmMode_QuitStillQuits proves ctrl+c/q still quit the
// program even mid-review, matching PR-30's own quit-key contract.
func TestModel_ConfirmMode_QuitStillQuits(t *testing.T) {
	ctx := setupActionTestEnv(t, skillAvailableProfileYAML)
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	m := newActionModel(t, ctx, now, ViewAssets)
	m = pressKey(t, m, "a")
	if m.mode != modeConfirm {
		t.Fatalf("mode after 'a' = %v, want modeConfirm", m.mode)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("Update(ctrl+c) during modeConfirm returned a nil Cmd, want tea.Quit")
	}
}
