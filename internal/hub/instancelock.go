package hub

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// InstanceLockPath is the lock/pid file guarding one socket path. It matches
// the "<socket without .sock>.pid" convention cmd/omca/hub.go already wrote
// its pid file under, so an existing deployment's path does not move.
func InstanceLockPath(socketPath string) string {
	return strings.TrimSuffix(socketPath, ".sock") + ".pid"
}

// InstanceLock is an exclusive, kernel-held single-instance lock for one hub
// socket path.
//
// It exists because Hub.Start's socket setup is otherwise not safe against a
// second hub:
//
//	_ = os.Remove(h.config.SocketPath)   // unconditional
//	l, err := net.Listen("unix", ...)
//
// os.Remove unlinks the path whether or not a live daemon is listening on it.
// The second daemon then binds a NEW inode at the same path and wins every
// subsequent connection, while the first keeps running forever holding an
// unlinked socket nobody can reach. Nothing reaps it: IdleReaper stops idle
// managed tools, not the daemon process. The "already running?" net.Dial in
// cmd/omca/hub.go's runHubStart cannot prevent this — it is a check, not a
// lock, so two starts that both dial a cold socket both proceed. Both
// `hub start -d` and bridge.go's ensureHubConnection auto-start reach that
// window, and ensureHubConnection discards its cmd.Start error, so a caller
// cannot even tell how many it spawned.
//
// This was not hypothetical. Two live `omca hub serve` processes were
// observed on one machine holding the same socket path with different
// inodes, one of them a launchd-supervised orphan, with the pid file naming
// a third state — a wedge where `hub stop` kills the reachable daemon and
// launchd declines to restart the unreachable one it still believes is
// healthy.
//
// flock is the right primitive rather than a pid file compared by content:
// the kernel releases it when the holding file descriptor closes, including
// on SIGKILL and on an unclean reboot, so a stale lock cannot outlive its
// process and there is no "is that recorded pid still alive, and is it still
// the same program" heuristic to get wrong.
type InstanceLock struct {
	path string
	file *os.File
}

// InstanceHeldError reports that another live process already holds the lock.
// Pid is that process's recorded id, or 0 when the lock file carried no
// readable pid (a holder that had not written one yet).
type InstanceHeldError struct {
	Path string
	Pid  int
}

func (e *InstanceHeldError) Error() string {
	if e.Pid > 0 {
		return fmt.Sprintf("hub: another hub instance (pid %d) already holds %s", e.Pid, e.Path)
	}
	return fmt.Sprintf("hub: another hub instance already holds %s", e.Path)
}

// AcquireInstanceLock takes the exclusive lock for path without blocking.
//
// A caller that gets a non-nil *InstanceHeldError must not touch the socket:
// a live daemon owns it. Any other error means the lock could not be
// evaluated at all, which is likewise not a licence to proceed — both cases
// fail closed.
func AcquireInstanceLock(path string) (*InstanceLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("hub: open instance lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		// Only EWOULDBLOCK/EAGAIN means "another process holds it". Every
		// other errno -- EBADF, ENOLCK, EOPNOTSUPP on a filesystem without
		// flock (some network mounts) -- is a real failure to evaluate the
		// lock at all. Reporting those as "already running" would make a
		// caller cheerfully exit 0 on a machine where single-instance
		// safety is not available, which is the opposite of fail-closed.
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("hub: lock instance file %s: %w", path, err)
		}
		// flock is advisory, so the holder's pid is still readable here.
		// Best-effort: a holder that has not written one yet reports 0.
		pid := 0
		if b, readErr := os.ReadFile(path); readErr == nil {
			if n, convErr := strconv.Atoi(strings.TrimSpace(string(b))); convErr == nil {
				pid = n
			}
		}
		return nil, &InstanceHeldError{Path: path, Pid: pid}
	}

	// The lock is ours. Replace whatever pid a previous, now-dead holder
	// left behind, so the file always names the process that actually holds
	// the lock rather than a stale one.
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("hub: truncate instance lock %s: %w", path, err)
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("hub: write instance lock %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("hub: sync instance lock %s: %w", path, err)
	}
	return &InstanceLock{path: path, file: f}, nil
}

// Release drops the lock and removes the lock file. Closing the descriptor is
// what actually releases the kernel lock, so Release is safe to call once and
// unnecessary on process exit.
func (l *InstanceLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	// The lock file is deliberately NOT unlinked.
	//
	// Unlinking it reintroduces the fresh-inode race this whole type exists
	// to close: between Close (which releases the kernel lock) and a
	// subsequent os.Remove, another process can open and lock the same path;
	// the Remove would then unlink the inode that process holds, leaving a
	// third free to create and lock a new file at the same name. Two holders,
	// one path — the orphan wedge again, one level down.
	//
	// A persistent lock file is the correct shape for flock: the lock lives
	// on the open file description, not on the name, so the file existing
	// means nothing on its own. A stale pid inside it is harmless because
	// AcquireInstanceLock truncates and rewrites it on every successful
	// acquire, and InstanceHeldError only ever reports a pid when the lock
	// is actually held — a pid read out of an unlocked file is never shown
	// to anyone.
	return err
}
