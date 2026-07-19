package transport

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/conformance"
)

// fakeAdapterExternalBinary is built once, in TestMain, and reused by every
// test in this package that needs a real external adapter subprocess
// (conformance_test.go, sandbox_test.go) — the same
// "build fixture binaries once in TestMain" convention
// cmd/omca/shim_test.go's own TestMain already established for this
// project's other subprocess-based tests.
var fakeAdapterExternalBinary string

func TestMain(m *testing.M) {
	// os.Exit does not run deferred functions, so the actual cleanup-owning
	// work runs in runTestMain below and this wrapper only exits once that
	// function (and therefore its own deferred os.RemoveAll) has already
	// returned -- otherwise the fixture temp directory leaks on every code
	// path (a real Copilot review finding on this PR).
	os.Exit(runTestMain(m))
}

func runTestMain(m *testing.M) int {
	tmp, err := os.MkdirTemp("", "omca-plugin-transport-fixtures")
	if err != nil {
		fmt.Fprintln(os.Stderr, "omca-plugin-transport-fixtures: MkdirTemp:", err)
		return 1
	}
	defer os.RemoveAll(tmp)

	fakeAdapterExternalBinary = filepath.Join(tmp, "fakeadapterexternal")
	if err := buildFixtureBinary(packageDir(), "./testdata/fakeadapterexternal", fakeAdapterExternalBinary); err != nil {
		fmt.Fprintln(os.Stderr, "omca-plugin-transport-fixtures: building fakeadapterexternal:", err)
		return 1
	}

	return m.Run()
}

// packageDir locates this package's own source directory via runtime.Caller
// (the same technique cmd/omca/shim_test.go's identical helper uses), so
// `go build` below resolves correctly regardless of the test binary's
// working directory.
func packageDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func buildFixtureBinary(dir, pkg, out string) error {
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, output)
	}
	return nil
}

// TestRemoteAdapter_ConformanceParity is issue #29's first, load-bearing
// acceptance criterion: an external adapter binary speaking contract v1 over
// stdio must pass the SAME conformance suite as an in-process adapter. It
// launches the real, compiled fakeadapterexternal fixture binary (which
// wraps conformance.NewFakeAdapter() — the exact reference implementation
// conformance.Run is already proven against, internal/plugin/conformance/
// fake.go's own doc comment) as a genuine OS subprocess, wraps it with
// RemoteAdapter, and runs conformance.Run against that RemoteAdapter value
// -- the identical function internal/plugin/conformance/fake_test.go (or any
// first-party adapter's own test) runs against an in-process
// plugin.HostAdapter. There is no second, transport-specific conformance
// suite anywhere in this package: passing this test on a RemoteAdapter IS
// the parity proof.
func TestRemoteAdapter_ConformanceParity(t *testing.T) {
	cmd := exec.Command(fakeAdapterExternalBinary)
	remote, err := NewRemoteAdapter(cmd)
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

// TestRemoteAdapter_HandshakeManifest_MatchesExternalBinarysDeclaration
// proves the "core loads it without recompilation" half more directly than
// the full conformance run does: the manifest RemoteAdapter learned purely
// from the handshake round trip matches exactly what
// testdata/fakeadapterexternal/main.go declared about itself, and nothing
// about this test needed the external binary's own source compiled into
// this test binary -- only its already-built path.
func TestRemoteAdapter_HandshakeManifest_MatchesExternalBinarysDeclaration(t *testing.T) {
	cmd := exec.Command(fakeAdapterExternalBinary)
	remote, err := NewRemoteAdapter(cmd)
	if err != nil {
		t.Fatalf("NewRemoteAdapter: unexpected error: %v", err)
	}
	defer remote.Close()

	manifest := remote.Manifest()
	if manifest.AdapterID != "conformance-fake" {
		t.Errorf("Manifest().AdapterID = %q, want %q", manifest.AdapterID, "conformance-fake")
	}
	if manifest.ContractVersion == "" {
		t.Error("Manifest().ContractVersion is empty")
	}
	if len(manifest.Hosts) != 1 || manifest.Hosts[0].HostID != "codex" {
		t.Errorf("Manifest().Hosts = %+v, want one host selector for codex", manifest.Hosts)
	}
	if remote.ID() != manifest.AdapterID {
		t.Errorf("ID() = %q, want it to match the handshake manifest's AdapterID %q", remote.ID(), manifest.AdapterID)
	}
}

// TestNewRemoteAdapter_SubprocessExitsImmediately_CleanErrorNotCrash proves
// the loading-time half of "a contract violation produces a clear
// diagnostic, not a crash" against a REAL subprocess (not a scripted pipe,
// unlike client_test.go's malformed-response proofs): a binary that exits
// before ever answering the handshake must make NewRemoteAdapter return a
// clear error, never panic and never hang.
func TestNewRemoteAdapter_SubprocessExitsImmediately_CleanErrorNotCrash(t *testing.T) {
	// "true" exits 0 immediately without reading stdin or writing anything
	// to stdout -- standing in for a crashed or misbehaving adapter binary
	// without needing a dedicated fixture just for this one negative case.
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("no `true` binary on this system's PATH to use as a fixture: %v", err)
	}
	cmd := exec.Command(truePath)
	_, err = NewRemoteAdapter(cmd)
	if err == nil {
		t.Fatal("NewRemoteAdapter against a subprocess that exits without answering: want an error, got nil")
	}
}
