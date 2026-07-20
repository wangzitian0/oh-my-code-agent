package observe

import "testing"

// ruleCoversFile reports whether any ruleCandidateFiles rule in rules names
// filename among its candidate files.
func ruleCoversFile(rules []sourceRule, filename string) bool {
	for _, r := range rules {
		if r.kind != ruleCandidateFiles {
			continue
		}
		for _, f := range r.files {
			if f == filename {
				return true
			}
		}
	}
	return false
}

// TestClaudeUserRules_ClaudeJSON_MovedNotDuplicated is the direct,
// filesystem-free regression proof for this fix: claudeUserRules must no
// longer look for .claude.json under the "CLAUDE_CONFIG_DIR" NativeHome
// (the bug — that location is $HOME/.claude by default, never where
// .claude.json actually lives when CLAUDE_CONFIG_DIR is unset), and must
// look for it exclusively under the new "HOME/.claude.json" NativeHome —
// not both, which would double-report the same file whenever
// CLAUDE_CONFIG_DIR happens to be unset and get date out of sync with
// internal/context/host.go's claudeNativeHomes, which only ever reports
// .claude.json's rule-relevant location under the "HOME/.claude.json" name.
func TestClaudeUserRules_ClaudeJSON_MovedNotDuplicated(t *testing.T) {
	configDirRules := claudeUserRules("CLAUDE_CONFIG_DIR")
	if ruleCoversFile(configDirRules, ".claude.json") {
		t.Error(`claudeUserRules("CLAUDE_CONFIG_DIR") still looks for .claude.json — this is the old, wrong location (it defaults to $HOME/.claude, but real Claude Code resolves .claude.json to bare $HOME/.claude.json when CLAUDE_CONFIG_DIR is unset)`)
	}

	homeClaudeJSONRules := claudeUserRules("HOME/.claude.json")
	if !ruleCoversFile(homeClaudeJSONRules, ".claude.json") {
		t.Fatal(`claudeUserRules("HOME/.claude.json") does not look for .claude.json — the fix's new NativeHome case is missing its rule`)
	}

	// Both the mcp_server and policy concepts must be represented under the
	// new location — PR-16 re-tags the same file's parsed content Policy as
	// well as MCP, and that must have moved along with the MCP rule, not
	// been left duplicated under the old CLAUDE_CONFIG_DIR case or dropped
	// entirely.
	wantConcepts := map[string]bool{conceptMCPServer: false, conceptPolicy: false}
	for _, r := range homeClaudeJSONRules {
		if r.kind != ruleCandidateFiles {
			continue
		}
		for _, f := range r.files {
			if f == ".claude.json" {
				if _, ok := wantConcepts[r.concept]; ok {
					wantConcepts[r.concept] = true
				}
			}
		}
	}
	for concept, found := range wantConcepts {
		if !found {
			t.Errorf(`claudeUserRules("HOME/.claude.json") has no .claude.json rule for concept %q`, concept)
		}
	}
	if ruleCoversFile(configDirRules, ".claude.json") == ruleCoversFile(homeClaudeJSONRules, ".claude.json") {
		t.Fatal("sanity check failed: .claude.json coverage must differ between the two NativeHome cases (moved, not present in both or neither)")
	}
}

// TestClaudeUserRules_UnknownNativeHome_ReturnsNil pins the existing
// forward-compatibility default (codexUserRules's doc comment) for the new
// case too: a NativeHome name this package does not recognize must return
// nil, not panic or silently reuse another case's rules.
func TestClaudeUserRules_UnknownNativeHome_ReturnsNil(t *testing.T) {
	if rules := claudeUserRules("something-unrecognized"); rules != nil {
		t.Errorf("claudeUserRules(unrecognized) = %+v, want nil", rules)
	}
}
