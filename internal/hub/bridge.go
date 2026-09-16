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
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}

	// 1. Ensure Hub is running; auto-start if socket is absent or unreachable
	conn, err := ensureHubConnection(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("hub bridge: connect to hub daemon: %w", err)
	}
	defer conn.Close()

	// 2. Send initial attach handshake
	attach := AttachMessage{
		Type:     "bridge_attach",
		Server:   serverName,
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

	// Try to auto-start hub daemon
	exe, err := os.Executable()
	if err == nil {
		cmd := exec.Command(exe, "hub", "start", "-d")
		_ = cmd.Start()
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

	return nil, fmt.Errorf("hub daemon socket unreachable at %s: %w", socketPath, err)
}
