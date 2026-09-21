package main

import (
	"bytes"
	"net"
	"os"
	"strings"
	"testing"
)

func TestRunHub_Usage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runHub(&bytes.Buffer{}, &stdout, &stderr, nil)
	if code != 2 {
		t.Errorf("expected exit 2 on empty args, got %d", code)
	}
	if !strings.Contains(stderr.String(), "usage: omca hub") {
		t.Errorf("expected usage message, got %q", stderr.String())
	}
}

func TestRunHub_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"unknown"})
	if code != 2 {
		t.Errorf("expected exit 2 on unknown subcommand, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown hub subcommand") {
		t.Errorf("expected unknown subcommand error, got %q", stderr.String())
	}
}

func TestRunHubBridge_Flags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"bridge"})
	if code != 2 {
		t.Errorf("expected exit 2 on missing server flag, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--server=<name> is required") {
		t.Errorf("expected required server flag error, got %q", stderr.String())
	}
}

func TestRunHubStatus_Stopped(t *testing.T) {
	sock := "/tmp/omca-nonexistent-status.sock"

	var stdout, stderr bytes.Buffer
	code := runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"status", "--socket=" + sock})
	if code != 0 {
		t.Errorf("expected exit 0 on status check, got %d", code)
	}
	if !strings.Contains(stdout.String(), "NOT running") {
		t.Errorf("expected NOT running output, got %q", stdout.String())
	}

	stdout.Reset()
	code = runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"status", "--socket=" + sock, "--json"})
	if code != 0 {
		t.Errorf("expected exit 0 on json status, got %d", code)
	}
	if !strings.Contains(stdout.String(), `"status": "stopped"`) {
		t.Errorf("expected stopped json output, got %q", stdout.String())
	}
}

func TestRunHubStop_Stopped(t *testing.T) {
	sock := "/tmp/omca-nonexistent-stop.sock"

	var stdout, stderr bytes.Buffer
	code := runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"stop", "--socket=" + sock})
	if code != 0 {
		t.Errorf("expected exit 0 on stop check, got %d", code)
	}
	if !strings.Contains(stdout.String(), "is not running") {
		t.Errorf("expected 'is not running' output, got %q", stdout.String())
	}
}

func TestRunHubTop_Stopped(t *testing.T) {
	sock := "/tmp/omca-nonexistent-top.sock"
	var stdout, stderr bytes.Buffer
	code := runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"top", "--socket=" + sock})
	if code != 1 {
		t.Errorf("expected exit 1 on top stopped, got %d", code)
	}
}

func TestRunHubWorker_Usage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"worker"})
	if code != 2 {
		t.Errorf("expected exit 2 on empty worker command, got %d", code)
	}
}

func TestRunHubWorker_Stopped(t *testing.T) {
	sock := "/tmp/omca-nonexistent-worker.sock"

	var stdout, stderr bytes.Buffer
	code := runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"worker", "list", "--socket=" + sock})
	if code != 1 {
		t.Errorf("expected exit 1 on worker list stopped, got %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	code = runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"worker", "kill", "w-1", "--socket=" + sock})
	if code != 1 {
		t.Errorf("expected exit 1 on worker kill stopped, got %d", code)
	}
}

func TestRunHubStart_AlreadyRunning(t *testing.T) {
	sock := "/tmp/omca-start-mock.sock"
	_ = os.Remove(sock)
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer l.Close()
	defer os.Remove(sock)

	var stdout, stderr bytes.Buffer
	code := runHub(&bytes.Buffer{}, &stdout, &stderr, []string{"start", "--socket=" + sock})
	if code != 0 {
		t.Errorf("expected exit 0 on start already running, got %d", code)
	}
	if !strings.Contains(stdout.String(), "already running") {
		t.Errorf("expected 'already running' output, got %q", stdout.String())
	}
}
