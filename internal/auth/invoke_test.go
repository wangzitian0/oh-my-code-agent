package auth

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
)

// writeFakeLoginBinary writes a small, hermetic POSIX shell script standing
// in for a real codex/claude installation — mirroring
// internal/qualify/invoke_test.go's own TestRunInvocationRunsIsolatedFakeBinary
// fixture pattern (this project's established "fakehost"-style approach: a
// fake binary the test itself writes, on a PATH the test constructs from
// scratch, never the real installed host CLI). Invoked with "--version" it
// prints a version line; invoked any other way, it appends one "invoked:
// <args joined by space>" line to markerFile, proving both that the
// invocation happened exactly once and exactly which arguments it received
// — the same evidentiary technique cmd/omca/testdata/fakehost's
// FAMEHOST_MARKER mechanism uses for its own non-recursion proof.
func writeFakeLoginBinary(t *testing.T, dir, name, markerFile string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo \"9.9.9\"; exit 0; fi\n" +
		"echo \"invoked: $*\" >> \"" + markerFile + "\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestInvoke_Rung3_RunsFakeLoginBinary proves the "invoking" half of issue
// #27's round-3 scoping: Decide's rung-3 InvocationPlan, run through Invoke
// against a fake binary standing in for `codex`, is actually exec'd with the
// exact arguments Decide identified — never the real codex CLI (PATH here
// is constructed from scratch, pointing only at this test's own tmp dir).
func TestInvoke_Rung3_RunsFakeLoginBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a POSIX shell script")
	}
	binDir := t.TempDir()
	markerFile := filepath.Join(t.TempDir(), "marker.log")
	writeFakeLoginBinary(t, binDir, "codex", markerFile)

	qual := KeyringQualification("darwin-arm64")
	decision, err := Decide(context.Background(), "codex", hostcontext.Environment{}, nil, qual)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Rung != RungIdentityRuntimeLogin || decision.Invocation == nil {
		t.Fatalf("decision = %+v, want RungIdentityRuntimeLogin with a populated Invocation", decision)
	}

	result, err := Invoke(context.Background(), *decision.Invocation, binDir, []string{"HOME=" + t.TempDir()})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Skipped {
		t.Fatalf("result = %+v, want not skipped (fake binary is on the supplied PATH)", result)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; stderr=%q", result.ExitCode, result.Stderr)
	}

	markerBytes, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("reading marker file: %v", err)
	}
	marker := strings.TrimSpace(string(markerBytes))
	if marker != "invoked: login" {
		t.Errorf("marker file contents = %q, want %q -- the fake binary must have been invoked exactly once with exactly the args Decide identified", marker, "invoked: login")
	}
}

// TestInvoke_Rung3_ClaudeCode mirrors the codex case for claude-code's
// two-word login subcommand ("auth login"), proving Invoke passes multi-arg
// InvocationPlans through unmodified.
func TestInvoke_Rung3_ClaudeCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a POSIX shell script")
	}
	binDir := t.TempDir()
	markerFile := filepath.Join(t.TempDir(), "marker.log")
	writeFakeLoginBinary(t, binDir, "claude", markerFile)

	qual := KeyringQualification("darwin-arm64")
	decision, err := Decide(context.Background(), "claude-code", hostcontext.Environment{}, nil, qual)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	result, err := Invoke(context.Background(), *decision.Invocation, binDir, []string{"HOME=" + t.TempDir()})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Skipped || result.ExitCode != 0 {
		t.Fatalf("result = %+v, want a clean, non-skipped run", result)
	}

	markerBytes, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("reading marker file: %v", err)
	}
	if got := strings.TrimSpace(string(markerBytes)); got != "invoked: auth login" {
		t.Errorf("marker file contents = %q, want %q", got, "invoked: auth login")
	}
}

// TestInvoke_SkipsWhenBinaryNotFound proves Invoke fails closed (a Skipped
// result, not an attempted exec and not a hard error) when the resolved
// PATH does not contain the plan's command -- e.g. the identity's host
// binary genuinely is not installed.
func TestInvoke_SkipsWhenBinaryNotFound(t *testing.T) {
	plan := InvocationPlan{Host: "codex", Command: "definitely-not-installed-omca-auth-test", Args: []string{"login"}}
	result, err := Invoke(context.Background(), plan, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !result.Skipped {
		t.Errorf("result = %+v, want Skipped=true", result)
	}
	if result.Attempted != true {
		// Attempted records that Invoke tried to resolve the binary, distinct
		// from actually running it -- matches internal/qualify.RunInvocation's
		// own Attempted/Skipped semantics.
		t.Errorf("Attempted = %v, want true", result.Attempted)
	}
}

// TestInvoke_NeverInheritsAmbientEnvironment proves Invoke's child process
// receives EXACTLY the env slice the caller supplied, never anything from
// the calling `go test` process's own real environment — the same
// discipline internal/qualify.RunInvocation and internal/context.probeVersion
// already enforce. This test sets a real ambient env var on the test
// process itself and asserts the fake binary (which dumps its environment
// when invoked with "dump") never sees it.
func TestInvoke_NeverInheritsAmbientEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a POSIX shell script")
	}
	t.Setenv("OMCA_AUTH_TEST_AMBIENT_MARKER", "must-not-leak-into-child")

	binDir := t.TempDir()
	dumpPath := filepath.Join(binDir, "codex")
	script := "#!/bin/sh\nenv\n"
	if err := os.WriteFile(dumpPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	plan := InvocationPlan{Host: "codex", Command: "codex", Args: nil}
	result, err := Invoke(context.Background(), plan, binDir, []string{"HOME=" + t.TempDir()})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if strings.Contains(result.Stdout, "OMCA_AUTH_TEST_AMBIENT_MARKER") {
		t.Errorf("child process env leaked the ambient test-process env var; stdout:\n%s", result.Stdout)
	}
}
