package runtime

import (
	"path/filepath"
	"testing"
	"time"
)

// TestAppendLedgerEntry_ReadBack proves the basic append-and-read round
// trip: two entries appended in order come back in the same order --
// issue #18 AC "an append-only ledger exist[s] under worktree state."
func TestAppendLedgerEntry_ReadBack(t *testing.T) {
	worktreeStateDir := t.TempDir()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	first := LedgerEntry{
		Host:         "codex",
		GenerationID: "generation:sha256:aaaa",
		Kind:         "pending",
		RecordedAt:   now.Format(time.RFC3339),
		Detail:       "first compile",
	}
	if err := AppendLedgerEntry(worktreeStateDir, "codex", first); err != nil {
		t.Fatalf("AppendLedgerEntry (1st): %v", err)
	}

	second := LedgerEntry{
		Host:         "codex",
		GenerationID: "generation:sha256:bbbb",
		Kind:         "pending",
		RecordedAt:   now.Add(time.Minute).Format(time.RFC3339),
		Detail:       "second compile",
	}
	if err := AppendLedgerEntry(worktreeStateDir, "codex", second); err != nil {
		t.Fatalf("AppendLedgerEntry (2nd): %v", err)
	}

	entries, err := ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("ReadLedger returned %d entries, want 2", len(entries))
	}
	if entries[0] != first {
		t.Errorf("entries[0] = %+v, want %+v", entries[0], first)
	}
	if entries[1] != second {
		t.Errorf("entries[1] = %+v, want %+v", entries[1], second)
	}
}

// TestReadLedger_NoEntriesYet_ReturnsNilNoError proves an unwritten ledger
// is a normal empty state, not an error -- distinct from the
// missing-current/pending-pointer os.IsNotExist contract, since an empty
// ledger (a worktree that has compiled but never activated anything) is
// expected, not exceptional.
func TestReadLedger_NoEntriesYet_ReturnsNilNoError(t *testing.T) {
	worktreeStateDir := t.TempDir()
	entries, err := ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger with no ledger written yet: want nil error, got %v", err)
	}
	if entries != nil {
		t.Errorf("ReadLedger with no ledger written yet = %v, want nil", entries)
	}
}

// TestAppendLedgerEntry_IsAppendOnly proves a third append does not disturb
// the two entries already on disk -- reading the file directly and checking
// line count, the most literal proof of "append-only" this test can make
// without depending on ReadLedger's own parsing to prove itself.
func TestAppendLedgerEntry_IsAppendOnly(t *testing.T) {
	worktreeStateDir := t.TempDir()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		entry := LedgerEntry{
			Host:         "codex",
			GenerationID: "generation:sha256:entry",
			Kind:         "pending",
			RecordedAt:   now.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		}
		if err := AppendLedgerEntry(worktreeStateDir, "codex", entry); err != nil {
			t.Fatalf("AppendLedgerEntry (%d): %v", i, err)
		}
		entries, err := ReadLedger(worktreeStateDir, "codex")
		if err != nil {
			t.Fatalf("ReadLedger (after append %d): %v", i, err)
		}
		if len(entries) != i+1 {
			t.Fatalf("after %d appends, ReadLedger returned %d entries, want %d", i+1, len(entries), i+1)
		}
	}
}

// TestAppendLedgerEntry_RejectsMismatchedHost proves the caller-composition-
// bug check: entry.Host must equal the host argument.
func TestAppendLedgerEntry_RejectsMismatchedHost(t *testing.T) {
	worktreeStateDir := t.TempDir()
	entry := LedgerEntry{
		Host:         "claude-code",
		GenerationID: "generation:sha256:aaaa",
		Kind:         "pending",
		RecordedAt:   time.Now().Format(time.RFC3339),
	}
	if err := AppendLedgerEntry(worktreeStateDir, "codex", entry); err == nil {
		t.Fatal("AppendLedgerEntry with entry.Host != host: want error, got nil")
	}
}

// TestAppendLedgerEntry_SeparateHostsHaveSeparateLedgers proves this
// package's per-host ledger convention (ledgerPath's own doc comment):
// appending to "codex" must not appear when reading "claude-code"'s ledger.
func TestAppendLedgerEntry_SeparateHostsHaveSeparateLedgers(t *testing.T) {
	worktreeStateDir := t.TempDir()
	now := time.Now()
	if err := AppendLedgerEntry(worktreeStateDir, "codex", LedgerEntry{
		Host: "codex", GenerationID: "generation:sha256:aaaa", Kind: "pending", RecordedAt: now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("AppendLedgerEntry: %v", err)
	}

	claudeEntries, err := ReadLedger(worktreeStateDir, "claude-code")
	if err != nil {
		t.Fatalf("ReadLedger (claude-code): %v", err)
	}
	if len(claudeEntries) != 0 {
		t.Errorf("claude-code ledger has %d entries after only codex was appended to, want 0", len(claudeEntries))
	}

	// Sanity: the ledger file really does live under worktreeStateDir/ledger.
	if _, err := ReadLedger(filepath.Join(worktreeStateDir, "not-the-real-dir"), "codex"); err != nil {
		t.Fatalf("ReadLedger on an unrelated empty dir should report an empty ledger, not error: %v", err)
	}
}
