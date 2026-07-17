package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ActivationInProgressError reports that an activation lock for
// (WorktreeStateDir, Host) is already held by another process (or another
// in-flight call within this one), so Activate/Rollback refused to start a
// second transaction rather than risk the two racing.
//
// # The bug this closes
//
// Activate reads "pending" and "current" ONCE near its own start, then --
// after a CAS check that only ever compares freshly-recomputed source
// digests against what it already read -- unconditionally writes "current"
// using that same stale in-memory pendingDir/pendingGen. Rollback has the
// identical read-then-write shape for the same "current" pointer. Neither
// function, nor anything in cmd/omca, ever serialized concurrent callers
// against each other: two concurrent Activate calls (or an Activate racing a
// Rollback) for the same (WorktreeStateDir, Host) could both pass their own
// independent checks and both write "current" -- whichever wrote last won,
// silently overwriting the other transaction's already-activated,
// already-ledgered generation with no error and no conflict record, and
// leaving a Ledger entry that falsely claims a generation is current when it
// is not. activate_test.go's TestActivate_ConcurrentActivations_
// LockPreventsSilentClobber reproduces exactly this and proves the lock
// below closes it.
type ActivationInProgressError struct {
	WorktreeStateDir string
	Host             string
}

func (e *ActivationInProgressError) Error() string {
	return fmt.Sprintf("runtime: %s: another activation or rollback is already in progress for this host under %s -- wait for it to finish and try again", e.Host, e.WorktreeStateDir)
}

// activationLockPath is the advisory lock file guarding host's "current"
// pointer under worktreeStateDir -- a sibling of the "current"/"pending"
// pointer directories and "ledger/" (current.go's pointerLinkPath,
// pending.go, ledger.go's ledgerPath all follow this same "one directory per
// concern, one file per host inside it" convention; this file matches it
// rather than inventing a new layout). It is never read for its content --
// only its existence and the OS-level advisory lock held on its file
// descriptor matter -- so an empty file is exactly as good as any other and
// nothing ever needs to clean it up between runs (see acquireActivationLock's
// doc comment on why a stale lock file left behind by a crashed process is
// harmless).
func activationLockPath(worktreeStateDir, host string) string {
	return filepath.Join(worktreeStateDir, "locks", host+".activate.lock")
}

// activationLock is one held advisory lock, returned by
// acquireActivationLock and released via its own release method (always via
// defer at the call site, including every error/early-return path).
type activationLock struct {
	f *os.File
}

// acquireActivationLock takes host's activation lock under worktreeStateDir
// (activationLockPath), non-blocking: if another process (or another
// in-flight call in this one) already holds it, this returns immediately
// with a *ActivationInProgressError rather than hanging -- a concurrent
// `omca activate`/`omca rollback` invocation that loses the race gets a
// clear, actionable error instead of either blocking indefinitely or (the
// bug this exists to fix) silently corrupting state. Activate and Rollback
// both call this with the same (worktreeStateDir, host) key, since both
// mutate the same "current" pointer through the same race window.
//
// # Why syscall.Flock, not a bespoke coordination protocol
//
// This project's CI matrix is macOS + Linux only (internal/shim/exec.go's
// own "syscall.Exec exists on every unix GOOS this project's CI touches...
// but not windows -- no other package in this module guards Windows either"
// scope note applies here too); the stdlib syscall package's Flock is
// available on both darwin and linux with identical semantics
// (LOCK_EX|LOCK_NB to attempt an exclusive non-blocking acquire,
// syscall.EWOULDBLOCK on contention), so no new dependency (e.g.
// golang.org/x/sys/unix) is needed. An flock(2)-based lock is also
// self-releasing if the holding process crashes or is killed -- the kernel
// drops the lock the moment the file descriptor closes, for any reason --
// which is exactly why this is preferred over a plain O_CREATE|O_EXCL marker
// file: that scheme would need a separate stale-lock detection/cleanup path
// for "the process that created the marker died before removing it," a
// problem flock(2) does not have.
func acquireActivationLock(worktreeStateDir, host string) (*activationLock, error) {
	dir := filepath.Join(worktreeStateDir, "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("runtime: acquiring activation lock for %s: %w", host, err)
	}
	path := activationLockPath(worktreeStateDir, host)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("runtime: acquiring activation lock for %s: %w", host, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, &ActivationInProgressError{WorktreeStateDir: worktreeStateDir, Host: host}
		}
		return nil, fmt.Errorf("runtime: acquiring activation lock for %s: %w", host, err)
	}
	return &activationLock{f: f}, nil
}

// release drops the advisory lock and closes its file descriptor. Safe to
// call on a nil *activationLock (a no-op) so a defer at a call site that
// never successfully acquired one stays trivially correct.
func (l *activationLock) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}
