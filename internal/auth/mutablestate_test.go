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
	// "cursor" is a valid domain.KnownHostIDs entry this package has no
	// classification rows for -- must return an empty, non-error result
	// rather than a fabricated table.
	items, err := ClassificationTable("cursor")
	if err != nil {
		t.Fatalf("ClassificationTable(cursor): %v", err)
	}
	if len(items) != 0 {
		t.Errorf("ClassificationTable(cursor) = %d items, want 0", len(items))
	}
}

// TestStateItem_Matches_VersionedFamilies is the regression test for the way
// this table rotted by construction.
//
// The sqlite row listed "state_5.sqlite" by exact name. The same host home
// also held goals_1.sqlite, logs_2.sqlite and memories_1.sqlite plus their
// -wal/-shm sidecars, and every one of them fell out of classification
// silently -- 6.4MB of it on the machine this was measured against. A table
// keyed on a version number expires on the host's next release.
func TestStateItem_Matches_VersionedFamilies(t *testing.T) {
	sqlite := StateItem{NativePath: "*.sqlite"}
	for _, name := range []string{"state_5.sqlite", "logs_2.sqlite", "goals_1.sqlite", "memories_1.sqlite"} {
		if !sqlite.Matches(name) {
			t.Errorf("*.sqlite does not match %q; a versioned family must match by shape, not by the one version that existed when the row was written", name)
		}
	}
	// A pattern must not widen into neighbours that merely share a suffix
	// or prefix.
	for _, name := range []string{".sqlite", "state_5.sqlite-wal", "notes.sqlite.bak"} {
		if sqlite.Matches(name) {
			t.Errorf("*.sqlite matched %q; the wildcard stands for the version segment only, not an open substring test", name)
		}
	}

	wal := StateItem{NativePath: "*.sqlite-wal"}
	if !wal.Matches("logs_2.sqlite-wal") || wal.Matches("logs_2.sqlite") {
		t.Error("the -wal sidecar pattern must match sidecars and only sidecars")
	}
}

// TestStateItem_Matches_DirectoriesAndExactNames keeps the two existing
// shapes working, so adding patterns did not change what was already right.
func TestStateItem_Matches_DirectoriesAndExactNames(t *testing.T) {
	dir := StateItem{NativePath: "cache/"}
	if !dir.Matches("cache") {
		t.Error(`a trailing "/" means "this directory" and must still match the bare name`)
	}
	if dir.Matches("cache2") || dir.Matches("my-cache") {
		t.Error("a directory row must stay an exact match, not a prefix or substring one")
	}

	exact := StateItem{NativePath: "auth.json"}
	if !exact.Matches("auth.json") || exact.Matches("auth.json.bak") {
		t.Error("an exact row must match exactly; auth.json.bak is not auth.json")
	}
}

// TestClassificationTable_ClassifiesTheLargestObservedEntries pins the rows
// added after measuring a real installation.
//
// Before them, 103.7MB of 117.6MB held state had no row at all -- 88%. The
// point is not the specific classes so much as that the table stops being
// silent about the things that actually take space.
func TestClassificationTable_ClassifiesTheLargestObservedEntries(t *testing.T) {
	for _, tc := range []struct{ host, entry string }{
		{"codex", "tmp"}, {"codex", ".tmp"}, {"codex", "plugins"},
		{"codex", "models_cache.json"}, {"codex", "shell_snapshots"},
		{"codex", "skills"}, {"codex", "history.jsonl"}, {"codex", "version.json"},
		{"codex", "logs_2.sqlite"}, {"codex", "logs_2.sqlite-wal"},
		{"claude-code", "plugins"}, {"claude-code", "backups"},
	} {
		items, err := ClassificationTable(tc.host)
		if err != nil {
			t.Fatalf("ClassificationTable(%s): %v", tc.host, err)
		}
		var matched bool
		for _, it := range items {
			if it.Matches(tc.entry) {
				matched = true
				if err := domain.ValidateMutableStateClass(it.Class); err != nil {
					t.Errorf("%s/%s carries an invalid class: %v", tc.host, tc.entry, err)
				}
				if it.Reason == "" {
					t.Errorf("%s/%s has no Reason; a classification without a stated basis is an assertion", tc.host, tc.entry)
				}
				break
			}
		}
		if !matched {
			t.Errorf("%s/%s has no row in the classification table; it was observed on a real installation and would be reported unclassified", tc.host, tc.entry)
		}
	}
}

// TestClassificationTable_ShellSnapshotsAreProhibited is a security
// assertion, not a tidiness one.
//
// A shell snapshot is a verbatim dump of an environment, so it holds
// whatever secrets that shell carried. One on the maintainer's machine was
// found containing a 1Password service-account token in plaintext, and it
// was world-readable. That is the same hazard auth.json carries, so it takes
// the same class.
func TestClassificationTable_ShellSnapshotsAreProhibited(t *testing.T) {
	items, err := ClassificationTable("codex")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Matches("shell_snapshots") {
			if it.Class != domain.MutableStateProhibitedImport {
				t.Errorf("shell_snapshots class = %q, want %q: these files carry whatever secrets the captured shell held", it.Class, domain.MutableStateProhibitedImport)
			}
			return
		}
	}
	t.Fatal("shell_snapshots has no row; environment dumps must be classified, not left to default")
}
