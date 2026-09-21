package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"
)

// RunBridge connects standard input/output of the calling process to the resident Hub.
func RunBridge(ctx context.Context, serverName string, socketPath string, hostName string, stdin io.Reader, stdout io.Writer) error {
	return RunBridgeWithProfile(ctx, "", "", serverName, socketPath, hostName, stdin, stdout)
}

// RunBridgeWithProfile connects standard input/output with profile and working directory metadata.
func RunBridgeWithProfile(ctx context.Context, profile string, cwd string, serverName string, socketPath string, hostName string, stdin io.Reader, stdout io.Writer) error {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// 1. Ensure Hub is running; auto-start if socket is absent or unreachable
	conn, err := ensureHubConnection(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("hub bridge: connect to hub daemon: %w", err)
	}
	defer conn.Close()

	// 2. Send initial attach handshake with profile and cwd metadata
	attach := AttachMessage{
		Type:     "bridge_attach",
		Server:   serverName,
		Profile:  profile,
		Cwd:      cwd,
		HostName: hostName,
	}
	attachData, err := json.Marshal(attach)
	if err != nil {
		return fmt.Errorf("hub bridge: marshal attach: %w", err)
	}
	attachData = append(attachData, '\n')
	if _, err := conn.Write(attachData); err != nil {
		return fmt.Errorf("hub bridge: send attach frame: %w", err)
	}

	// 3. Bidirectional pipe: stdin -> socket, socket -> stdout
	errCh := make(chan error, 2)

	go func() {
		_, err := io.Copy(conn, stdin)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		if err != nil {
			errCh <- err
		}
	}()

	go func() {
		_, err := io.Copy(stdout, conn)
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		if err == nil || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

func ensureHubConnection(ctx context.Context, socketPath string) (net.Conn, error) {
	conn, err := net.Dial("unix", socketPath)
	if err == nil {
		return conn, nil
	}

	// Try to auto-start the hub daemon. Two bridges racing here is expected
	// and safe: `hub serve` takes an exclusive instance lock before it
	// touches the socket (instancelock.go), so the loser exits instead of
	// unlinking the winner's socket, and the poll below simply connects to
	// whichever one won. A spawn failure is recorded rather than discarded,
	// because it is the difference between "the hub is slow to come up" and
	// "this build cannot start a hub at all" — the poll timeout below cannot
	// tell those apart on its own.
	var startErr error
	exe, err := os.Executable()
	if err != nil {
		startErr = fmt.Errorf("locate own executable: %w", err)
	} else {
		cmd := exec.Command(exe, "hub", "start", "-d")
		if err := cmd.Start(); err != nil {
			startErr = fmt.Errorf("spawn %s hub start -d: %w", exe, err)
		}
	}

	// Poll socket up to 3 seconds
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}

		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			return conn, nil
		}
	}

	if startErr != nil {
		return nil, fmt.Errorf("hub daemon socket unreachable at %s (auto-start failed: %v): %w", socketPath, startErr, err)
	}
	return nil, fmt.Errorf("hub daemon socket unreachable at %s: %w", socketPath, err)
}
