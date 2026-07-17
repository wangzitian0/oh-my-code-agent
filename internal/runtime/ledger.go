package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// LedgerEntry is one durable, ordered record of a generation transition
// under a worktree's ledger/ directory (docs/architecture/README.md §8's
// runtime-and-state layout; docs/architecture/runtime.md §5's layout diagram
// names `ledger/` a sibling of `current`/`pending`; §5.4's Activation flow
// ends with "-> append Ledger entry"). This PR (issue #18, PR-14) defines
// the entry shape and an append-only writer/reader; it does NOT itself call
// AppendLedgerEntry from any activation transaction -- there is no
// Activation transaction yet (PR-15, "Activation transaction: CAS, atomic
// switch, rollback"). A LedgerEntry here just names what happened, to which
// host, when, and which generation was involved, so PR-15 can append a real
// entry at each transaction step without having to invent the entry shape
// itself.
type LedgerEntry struct {
	// Host is the canonical host ID this entry concerns.
	Host string `json:"host"`
	// GenerationID is the generation this entry is about.
	GenerationID string `json:"generationId"`
	// Kind names what happened: not a closed enum on purpose (the schema
	// documents around this project are still v1alpha1, and PR-15/PR-22 own
	// the real transition vocabulary -- "activated", "rolledback", a
	// verify-outcome kind, etc.). This package's own tests only ever write
	// "pending" (a generation was set as host's pending pointer). Left
	// free-text so PR-15 does not need another schema change just to record
	// a new kind of transition.
	Kind string `json:"kind"`
	// RecordedAt is the caller-injected wall-clock time this entry was
	// appended, RFC3339 UTC -- this package never reads the clock itself
	// (matching every other timestamp in this package, e.g.
	// BootstrapRequest.Now).
	RecordedAt string `json:"recordedAt"`
	// Detail is optional free text (a human-readable reason, or an error
	// summary for a failed transition).
	Detail string `json:"detail,omitempty"`
}

// ledgerPath is host's append-only ledger file under worktreeStateDir/
// ledger/. One file per host, not one shared file for the whole worktree,
// matching this package's established per-host convention for current/
// pending pointers (current.go, pending.go; see doc.go's "Per-host, not
// per-worktree" section) -- docs/architecture/runtime.md §5's diagram names
// the `ledger/` directory but does not prescribe a file-naming scheme inside
// it, so this is a documented judgment call consistent with the rest of the
// package rather than a literal spec requirement.
func ledgerPath(worktreeStateDir, host string) string {
	return filepath.Join(worktreeStateDir, "ledger", host+".jsonl")
}

// AppendLedgerEntry appends entry to host's append-only ledger under
// worktreeStateDir, creating the ledger directory and file on first use.
// Entries already recorded are never rewritten, reordered, or removed: this
// function only ever opens the file in O_APPEND mode and writes one more
// JSON-Lines record, so a ledger's on-disk history is exactly the sequence
// of AppendLedgerEntry calls that produced it -- an audit trail, not a
// mutable log.
//
// entry.Host must equal host (a caller-composition-bug check, the same
// discipline BootstrapRequest.validate() applies to Observations): passing
// a mismatched Host would silently record a transition against the wrong
// host's ledger file while claiming to be about a different one.
func AppendLedgerEntry(worktreeStateDir, host string, entry LedgerEntry) (err error) {
	if worktreeStateDir == "" {
		return fmt.Errorf("runtime: AppendLedgerEntry: worktreeStateDir is required")
	}
	if err := domain.ValidateHostID(host); err != nil {
		return fmt.Errorf("runtime: AppendLedgerEntry: %w", err)
	}
	if entry.Host != host {
		return fmt.Errorf("runtime: AppendLedgerEntry: entry.Host %q does not match host %q", entry.Host, host)
	}
	if entry.GenerationID == "" {
		return fmt.Errorf("runtime: AppendLedgerEntry: entry.GenerationID is required")
	}
	if entry.Kind == "" {
		return fmt.Errorf("runtime: AppendLedgerEntry: entry.Kind is required")
	}
	if entry.RecordedAt == "" {
		return fmt.Errorf("runtime: AppendLedgerEntry: entry.RecordedAt is required (this package never reads the clock implicitly)")
	}

	dir := filepath.Join(worktreeStateDir, "ledger")
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return fmt.Errorf("runtime: AppendLedgerEntry: %w", mkErr)
	}

	f, openErr := os.OpenFile(ledgerPath(worktreeStateDir, host), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if openErr != nil {
		return fmt.Errorf("runtime: AppendLedgerEntry: %w", openErr)
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("runtime: AppendLedgerEntry: %w", cerr)
		}
	}()

	data, marshalErr := json.Marshal(entry)
	if marshalErr != nil {
		return fmt.Errorf("runtime: AppendLedgerEntry: %w", marshalErr)
	}
	data = append(data, '\n')
	if _, writeErr := f.Write(data); writeErr != nil {
		return fmt.Errorf("runtime: AppendLedgerEntry: %w", writeErr)
	}
	return nil
}

// ReadLedger reads every entry from host's ledger under worktreeStateDir, in
// append order (oldest first) -- the order AppendLedgerEntry wrote them. A
// ledger that has never had an entry appended returns (nil, nil), not an
// error: an empty ledger is a normal, expected state (e.g. a worktree with a
// compiled pending generation that has never been activated), unlike a
// missing current/pending pointer, which callers treat as "not yet managed."
func ReadLedger(worktreeStateDir, host string) ([]LedgerEntry, error) {
	data, err := os.ReadFile(ledgerPath(worktreeStateDir, host))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("runtime: ReadLedger: %w", err)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil, nil
	}
	lines := strings.Split(trimmed, "\n")
	entries := make([]LedgerEntry, 0, len(lines))
	for i, line := range lines {
		if line == "" {
			continue
		}
		var e LedgerEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("runtime: ReadLedger: line %d: %w", i+1, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}
