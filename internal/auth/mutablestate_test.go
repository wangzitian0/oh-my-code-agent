package auth

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func TestClassificationTable_CoversRequiredCategories(t *testing.T) {
	covered := map[string]bool{}
	for _, host := range []string{"codex", "claude-code"} {
		items, err := ClassificationTable(host)
		if err != nil {
			t.Fatalf("ClassificationTable(%q): %v", host, err)
		}
		if len(items) == 0 {
			t.Fatalf("ClassificationTable(%q) is empty", host)
		}
		for _, item := range items {
			covered[item.Category] = true
		}
	}
	for _, category := range RequiredCategories {
		if !covered[category] {
			t.Errorf("no StateItem across either host covers required category %q (docs/architecture/runtime.md §9)", category)
		}
	}
}

func TestClassificationTable_EveryItemIsWellFormed(t *testing.T) {
	for _, host := range []string{"codex", "claude-code"} {
		items, err := ClassificationTable(host)
		if err != nil {
			t.Fatalf("ClassificationTable(%q): %v", host, err)
		}
		for i, item := range items {
			if item.Host != host {
				t.Errorf("%s items[%d].Host = %q, want %q", host, i, item.Host, host)
			}
			if item.NativePath == "" {
				t.Errorf("%s items[%d]: NativePath is empty", host, i)
			}
			if item.Reason == "" {
				t.Errorf("%s items[%d]: Reason is empty", host, i)
			}
			if err := domain.ValidateMutableStateClass(item.Class); err != nil {
				t.Errorf("%s items[%d]: %v", host, i, err)
			}
		}
	}
}

// TestClassificationTable_ClaudeJSON_IsProhibitedImport is a direct
// regression test for ADR 0003 decision item 4: Claude Code's single mixed
// account/OAuth/trust/MCP-registry state file must never be classified as
// something a generation could safely broad-share.
func TestClassificationTable_ClaudeJSON_IsProhibitedImport(t *testing.T) {
	items, err := ClassificationTable("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.NativePath == ".claude.json" {
			found = true
			if item.Class != domain.MutableStateProhibitedImport {
				t.Errorf(".claude.json Class = %q, want %q", item.Class, domain.MutableStateProhibitedImport)
			}
			if !item.RelativeToHomeDir {
				t.Error(".claude.json RelativeToHomeDir = false, want true (its own unset-default fallback is bare $HOME, distinct from the asset tree's $HOME/.claude default)")
			}
		}
	}
	if !found {
		t.Fatal("no StateItem for .claude.json found in claude-code's classification table")
	}
}

// TestClassificationTable_CredentialsAreNeverSharing proves every item this
// package classifies as credentials-adjacent (auth.json, .claude.json) is a
// non-sharing class, structurally consistent with ADR 0003 decision item 3.
func TestClassificationTable_CredentialsAreNeverSharing(t *testing.T) {
	for _, host := range []string{"codex", "claude-code"} {
		items, err := ClassificationTable(host)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Category != "credentials" && item.NativePath != ".claude.json" {
				continue
			}
			if item.Class.SharesAcrossGenerations() {
				t.Errorf("%s %s (%s): Class %q shares across generations, want a non-sharing class for credential-adjacent state", host, item.Name, item.NativePath, item.Class)
			}
		}
	}
}

func TestClassificationTable_UnknownHost(t *testing.T) {
	if _, err := ClassificationTable("not-a-real-host"); err == nil {
		t.Error("ClassificationTable(unknown host) error = nil, want error")
	}
}

func TestClassificationTable_KnownButUnimplementedHost(t *testing.T) {
	// "opencode" is a valid domain.KnownHostIDs entry this package has no
	// classification rows for -- must return an empty, non-error result
	// rather than a fabricated table.
	items, err := ClassificationTable("opencode")
	if err != nil {
		t.Fatalf("ClassificationTable(opencode): %v", err)
	}
	if len(items) != 0 {
		t.Errorf("ClassificationTable(opencode) = %d items, want 0", len(items))
	}
}
