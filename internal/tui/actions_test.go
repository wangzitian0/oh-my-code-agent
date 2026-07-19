package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
)

// TestActivationSelectionFor_OnlySkillAndMCPServerAreSelectable proves
// activationSelectionFor's own documented scope: "Instructions have no
// ActivationSelection field, so they are never Activation-selected"
// (internal/resolve/resolve.go's inSelection doc comment) -- any other
// concept must report ok=false, never silently build an empty selection
// that would no-op.
func TestActivationSelectionFor_OnlySkillAndMCPServerAreSelectable(t *testing.T) {
	if sel, ok := activationSelectionFor("skill", "code-review"); !ok || len(sel.Enable.Skills) != 1 || sel.Enable.Skills[0] != "code-review" {
		t.Errorf("activationSelectionFor(skill, code-review) = %+v, %v", sel, ok)
	}
	if sel, ok := activationSelectionFor("mcpServer", "internal-docs"); !ok || len(sel.Enable.MCPServers) != 1 || sel.Enable.MCPServers[0] != "internal-docs" {
		t.Errorf("activationSelectionFor(mcpServer, internal-docs) = %+v, %v", sel, ok)
	}
	for _, concept := range []string{"instruction", "permission", "hook", ""} {
		if _, ok := activationSelectionFor(concept, "whatever"); ok {
			t.Errorf("activationSelectionFor(%q, ...) = ok=true, want false (no ActivationSelection field exists for this concept)", concept)
		}
	}
}

// TestActionContext_Enabled proves ActionContext.enabled requires BOTH a
// non-empty WorktreeStateDir and a non-empty Worktree.Root -- the zero
// value (what NewModel leaves Model with until WithActionContext attaches
// a real one) must report false.
// TestActionContext_Enabled is a regression test (Copilot review finding on
// this PR): enabled() previously returned true once WorktreeStateDir and
// Worktree.Root were both set, even with ConfigRoot/ShimDir/Worktree.ID
// still empty -- every one of those fields is actually read elsewhere in
// this package's action layer (compositionDirsForAction, omcaCommandPathForAction,
// mergedActivation.Metadata.Worktree), so a zero-value one would silently
// run an action against a relative path or an incorrect OMCABinaryPath
// instead of this ActionContext being correctly treated as inert. Every
// field enabled() checks must independently gate it off.
func TestActionContext_Enabled(t *testing.T) {
	full := ActionContext{
		WorktreeStateDir: "/tmp/state",
		Worktree:         hostcontext.Worktree{Root: "/tmp/root", ID: "worktree:sha256:x"},
		ShimDir:          "/tmp/state/shims",
		ConfigRoot:       "/tmp/config",
	}
	cases := []struct {
		name string
		ctx  ActionContext
		want bool
	}{
		{"zero value", ActionContext{}, false},
		{"only state dir", ActionContext{WorktreeStateDir: "/tmp/x"}, false},
		{"only worktree root", ActionContext{Worktree: hostcontext.Worktree{Root: "/tmp/x"}}, false},
		{"missing worktree ID", func() ActionContext { c := full; c.Worktree.ID = ""; return c }(), false},
		{"missing shim dir", func() ActionContext { c := full; c.ShimDir = ""; return c }(), false},
		{"missing config root", func() ActionContext { c := full; c.ConfigRoot = ""; return c }(), false},
		{"every field set", full, true},
	}
	for _, c := range cases {
		if got := c.ctx.enabled(); got != c.want {
			t.Errorf("%s: enabled() = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestFirstActivatableCandidate proves the Desired-Graph-based selection
// this package's action layer uses (see firstActivatableCandidate's own
// doc comment for why it reads resolve.ResolvedState rather than the
// Effective Graph's Candidate.Disposition bucket): only an AVAILABLE,
// not-yet-Active skill/mcpServer asset is offered, in (Kind, ID) order,
// never an already-Active one, a DENIED/REQUIRED one, or an instruction.
func TestFirstActivatableCandidate(t *testing.T) {
	artifact := report.Artifact{
		Debug: map[string]report.HostDebug{
			"codex": {
				Desired: resolve.ResolvedState{
					Host: "codex",
					Assets: []resolve.ResolvedAsset{
						{Kind: resolve.KindInstruction, ID: "onboarding.md", Active: false, Intent: domain.IntentAvailable},
						{Kind: resolve.KindMCPServer, ID: "already-active", Active: true, Intent: domain.IntentAvailable},
						{Kind: resolve.KindMCPServer, ID: "internal-docs", Active: false, Intent: domain.IntentAvailable},
						{Kind: resolve.KindSkill, ID: "code-review", Active: false, Intent: domain.IntentAvailable},
						{Kind: resolve.KindSkill, ID: "required-skill", Active: true, Intent: domain.IntentRequired},
					},
				},
			},
		},
	}

	// resolve.ResolvedState.Assets is documented sorted by (Kind, ID); Kind
	// "mcpServer" < "skill" lexicographically, so internal-docs (the first
	// not-yet-Active mcpServer) must win over code-review.
	concept, id, ok := firstActivatableCandidate(artifact, "codex")
	if !ok || concept != "mcpServer" || id != "internal-docs" {
		t.Errorf("firstActivatableCandidate = %q, %q, %v, want mcpServer, internal-docs, true", concept, id, ok)
	}

	if _, _, ok := firstActivatableCandidate(artifact, "no-such-host"); ok {
		t.Error("firstActivatableCandidate for an unknown host: want ok=false")
	}

	onlyInactiveInstruction := report.Artifact{
		Debug: map[string]report.HostDebug{
			"codex": {Desired: resolve.ResolvedState{Assets: []resolve.ResolvedAsset{
				{Kind: resolve.KindInstruction, ID: "onboarding.md", Active: false, Intent: domain.IntentAvailable},
			}}},
		},
	}
	if _, _, ok := firstActivatableCandidate(onlyInactiveInstruction, "codex"); ok {
		t.Error("firstActivatableCandidate found an instruction asset, want false (instructions have no ActivationSelection field)")
	}
}

// TestStageAssetActivation_DetectFails_DoesNotPersistActivation is a
// regression test (Copilot review finding on this PR): stageAssetActivation
// previously persisted the merged Activation to disk BEFORE detecting/
// observing the target host, so a common, easy-to-hit failure (the host
// isn't installed) still durably mutated desired/activation.yaml even
// though staging itself failed -- surprising a later CLI/TUI run with a
// selection that was never actually staged. This test points at a host
// with nothing on PATH (guaranteed detection failure) and asserts both
// that stageAssetActivation errors AND that activation.yaml was never
// created.
func TestStageAssetActivation_DetectFails_DoesNotPersistActivation(t *testing.T) {
	ctx := setupActionTestEnv(t, "")
	// Override Env so codex is NOT on PATH -- detectAndObserveHost must
	// fail with "not installed" (setupActionTestEnv's own fake codex
	// binary is deliberately excluded here).
	ctx.Env = hostcontext.Environment{Vars: []string{
		"HOME=" + t.TempDir(),
		"PATH=" + t.TempDir(),
	}}

	_, err := stageAssetActivation(ctx, "codex", "skill", "code-review", time.Now())
	if err == nil {
		t.Fatal("stageAssetActivation against an uninstalled host: want an error, got nil")
	}

	activationYAML := filepath.Join(ctx.WorktreeStateDir, "desired", "activation.yaml")
	if _, statErr := os.Stat(activationYAML); statErr == nil {
		t.Errorf("%s exists after a failed stageAssetActivation call (detect never succeeded) -- the activation selection was persisted despite staging failing", activationYAML)
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("stat %s: %v", activationYAML, statErr)
	}
}
