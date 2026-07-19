package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/conformance"
	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/transport"
)

// minimalObserverBinary is built once, in TestMain, from this package's own
// main.go/adapter.go -- the same "build a real subprocess binary from
// source once, reuse it across tests" convention
// internal/plugin/transport/conformance_test.go's own TestMain and
// cmd/omca/shim_test.go's TestMain already established, rather than a third
// variant of that pattern.
var minimalObserverBinary string

func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

func runTestMain(m *testing.M) int {
	tmp, err := os.MkdirTemp("", "minimal-observer-fixture")
	if err != nil {
		fmt.Fprintln(os.Stderr, "minimal-observer-fixture: MkdirTemp:", err)
		return 1
	}
	defer os.RemoveAll(tmp)

	minimalObserverBinary = filepath.Join(tmp, "minimal-observer")
	if err := buildSelf(packageDir(), minimalObserverBinary); err != nil {
		fmt.Fprintln(os.Stderr, "minimal-observer-fixture: building minimal-observer:", err)
		return 1
	}

	return m.Run()
}

// packageDir locates this package's own source directory via
// runtime.Caller, so the `go build` below resolves correctly regardless of
// the test binary's working directory.
func packageDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func buildSelf(dir, out string) error {
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, output)
	}
	return nil
}

// TestMinimalObserver_ConformanceOverRealTransport is this example's own
// "its example skeleton compiles and passes conformance" proof
// (issue #30's first acceptance criterion): it launches the actual, real
// minimal-observer binary as a genuine OS subprocess, wraps it with
// transport.RemoteAdapter (the M6 out-of-process transport, PR-25), and runs
// the exact same internal/plugin/conformance.Run suite every first-party and
// third-party adapter is checked against -- mirroring
// internal/plugin/transport/conformance_test.go's own
// TestRemoteAdapter_ConformanceParity pattern for its in-tree fixture. There
// is no separate, weaker check for this example: passing this test IS the
// mechanical proof, not a claim made only in the guide's prose.
func TestMinimalObserver_ConformanceOverRealTransport(t *testing.T) {
	cmd := exec.Command(minimalObserverBinary)
	remote, err := transport.NewRemoteAdapter(cmd)
	if err != nil {
		t.Fatalf("NewRemoteAdapter: unexpected error: %v", err)
	}
	defer func() {
		if err := remote.Close(); err != nil {
			t.Errorf("Close: unexpected error: %v", err)
		}
	}()

	conformance.Run(t, remote)
}
