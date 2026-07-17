package runtime

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestDiffProposedChanges_HostScoped_SameKeyDifferentHosts is a regression
// test for a real Copilot review finding on this PR: GenerationSourceEntry
// carried no Host field, so a shared multi-host Generation's flat
// Spec.Sources list made two hosts' entries with the same (Concept, Source)
// indistinguishable -- exactly the differentiated-per-host-loadout scenario
// M2's own exit gate exists to prove. This builds one pendingGen where the
// SAME mcpServer asset ID is Included for codex but NOT for claude-code, and
// proves DiffProposedChanges("codex", ...) reports it as a change while
// DiffProposedChanges("claude-code", ...) does not.
func TestDiffProposedChanges_HostScoped_SameKeyDifferentHosts(t *testing.T) {
	pendingGen := domain.Generation{
		Spec: domain.GenerationSpec{
			Sources: []domain.GenerationSourceEntry{
				{Concept: "mcpServer", Source: "codegraph", Host: "codex", Included: true, Reason: "resolved desired state: active for codex"},
				{Concept: "mcpServer", Source: "codegraph", Host: "claude-code", Included: false, Reason: "resolved desired state: denied for claude-code"},
			},
		},
	}
	currentGen := domain.Generation{} // first-ever activation for both hosts

	codexChanges := DiffProposedChanges(currentGen, pendingGen, "codex")
	if len(codexChanges) != 1 {
		t.Fatalf("codex: got %d changes, want 1: %+v", len(codexChanges), codexChanges)
	}
	if codexChanges[0].Kind != ChangeEnableMCPServer || codexChanges[0].AssetID != "codegraph" || codexChanges[0].Host != "codex" {
		t.Errorf("codex change = %+v, want ChangeEnableMCPServer for codegraph/codex", codexChanges[0])
	}

	claudeChanges := DiffProposedChanges(currentGen, pendingGen, "claude-code")
	if len(claudeChanges) != 0 {
		t.Errorf("claude-code: got %d changes, want 0 (codegraph is Included:false for claude-code): %+v", len(claudeChanges), claudeChanges)
	}
}

// TestDiffProposedChanges_HostScoped_AlreadyActiveForOtherHostDoesNotSuppress
// is this fix's second failure mode: before the Host field existed, a
// change newly-included for host A could be wrongly treated as
// "already active" (and so silently dropped from the proposed-changes list,
// bypassing confirmation) merely because the SAME (Concept, Source) key was
// already active for host B in currentGen. This proves a change newly
// active for claude-code is still reported even though the identical key
// was already active for codex in currentGen.
func TestDiffProposedChanges_HostScoped_AlreadyActiveForOtherHostDoesNotSuppress(t *testing.T) {
	currentGen := domain.Generation{
		Spec: domain.GenerationSpec{
			Sources: []domain.GenerationSourceEntry{
				{Concept: "mcpServer", Source: "codegraph", Host: "codex", Included: true, Reason: "already active for codex"},
			},
		},
	}
	pendingGen := domain.Generation{
		Spec: domain.GenerationSpec{
			Sources: []domain.GenerationSourceEntry{
				{Concept: "mcpServer", Source: "codegraph", Host: "codex", Included: true, Reason: "still active for codex"},
				{Concept: "mcpServer", Source: "codegraph", Host: "claude-code", Included: true, Reason: "newly active for claude-code"},
			},
		},
	}

	claudeChanges := DiffProposedChanges(currentGen, pendingGen, "claude-code")
	if len(claudeChanges) != 1 {
		t.Fatalf("claude-code: got %d changes, want 1 (codegraph newly active for claude-code, not suppressed by codex's unrelated activation): %+v", len(claudeChanges), claudeChanges)
	}

	codexChanges := DiffProposedChanges(currentGen, pendingGen, "codex")
	if len(codexChanges) != 0 {
		t.Errorf("codex: got %d changes, want 0 (codegraph was already active for codex in currentGen): %+v", len(codexChanges), codexChanges)
	}
}
