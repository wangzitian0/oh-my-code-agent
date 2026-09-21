package hub

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
)

// TestHub_SecondInstance_RefusedAndFirstSocketSurvives is the regression test
// for the orphan-daemon wedge instancelock.go documents.
//
// Before the instance lock, Hub.Start unconditionally unlinked the socket
// path and bound a new inode there. A second hub therefore "succeeded" while
// silently stranding the first: still running, still holding an unlinked
// socket, unreachable, and never reaped. This asserts the two properties
// that make that impossible — the second Start fails, and the first hub is
// still serving on the same path afterwards.
func TestHub_SecondInstance_RefusedAndFirstSocketSurvives(t *testing.T) {
	sockPath := shortSocketPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := New(&Config{SocketPath: sockPath, Port: 0, Tools: make(map[string]ToolConfig)})
	if err := first.Start(ctx); err != nil {
		t.Fatalf("first Hub.Start: %v", err)
	}
	defer func() { _ = first.Close() }()

	second := New(&Config{SocketPath: sockPath, Port: 0, Tools: make(map[string]ToolConfig)})
	err := second.Start(ctx)
	if err == nil {
		_ = second.Close()
		t.Fatal("second Hub.Start succeeded; it must refuse while another instance holds the socket, or it orphans the first daemon")
	}
	var held *InstanceHeldError
	if !errors.As(err, &held) {
		t.Fatalf("second Hub.Start error = %v, want an *InstanceHeldError so a caller can tell 'already running' from a real failure", err)
	}
	if held.Pid != os.Getpid() {
		t.Errorf("InstanceHeldError.Pid = %d, want this process %d (the lock file must name the live holder)", held.Pid, os.Getpid())
	}

	// The decisive property: the first hub is still reachable on the same
	// path. A refused second start must not have unlinked or replaced it.
	conn, dialErr := net.Dial("unix", sockPath)
	if dialErr != nil {
		t.Fatalf("first hub is unreachable at %s after a refused second start (%v); the socket was replaced or removed", sockPath, dialErr)
	}
	_ = conn.Close()
}

// TestHub_RefusesLiveSocketHeldWithoutLock covers the one case the instance
// lock cannot decide on its own: a daemon from a build that predates the lock
// is listening on the socket while holding no lock at all. During that
// rollout Start would acquire the lock cleanly and then unlink a live
// daemon's socket, which is the exact orphan the lock exists to prevent.
//
// It also asserts the part that is easy to get wrong when refusing late:
// Start must hand the lock back, or a transient old daemon would leave the
// path permanently unstartable by every future hub.
func TestHub_RefusesLiveSocketHeldWithoutLock(t *testing.T) {
	sockPath := shortSocketPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Stand in for a pre-lock daemon: listening, holding no instance lock.
	legacy, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("simulate legacy daemon listener: %v", err)
	}
	defer func() { _ = legacy.Close() }()

	h := New(&Config{SocketPath: sockPath, Port: 0, Tools: make(map[string]ToolConfig)})
	if err := h.Start(ctx); err == nil {
		_ = h.Close()
		t.Fatal("Hub.Start succeeded against a live socket held by a lock-unaware daemon; it would have unlinked that daemon's socket and orphaned it")
	}

	// The legacy daemon must still be reachable.
	conn, dialErr := net.Dial("unix", sockPath)
	if dialErr != nil {
		t.Fatalf("legacy daemon unreachable after the refused start (%v); its socket was replaced or removed", dialErr)
	}
	_ = conn.Close()

	// And the lock must be free again, or the refusal has wedged the path.
	lock, lockErr := AcquireInstanceLock(InstanceLockPath(sockPath))
	if lockErr != nil {
		t.Fatalf("instance lock still held after a refused start (%v); a refusal must not leak the lock", lockErr)
	}
	_ = lock.Release()
}

// TestInstanceLock_ReleasedLockIsReacquirable proves the lock does not leak:
// a hub that shut down cleanly must leave the path free for the next one,
// otherwise the fix would trade an orphan daemon for an unstartable one.
func TestInstanceLock_ReleasedLockIsReacquirable(t *testing.T) {
	path := InstanceLockPath(shortSocketPath(t))

	lock, err := AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("first AcquireInstanceLock: %v", err)
	}
	if _, err := AcquireInstanceLock(path); err == nil {
		t.Fatal("second AcquireInstanceLock succeeded while the first was held")
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	again, err := AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("AcquireInstanceLock after Release: %v; a released lock must be reacquirable", err)
	}
	if err := again.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}
