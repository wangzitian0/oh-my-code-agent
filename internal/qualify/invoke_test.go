package qualify

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunInvocationSkippedWhenNotAttempted(t *testing.T) {
	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	manifest := InvocationManifest{
		Invoke: InvokeSpec{Attempted: false, Reason: "no safe non-interactive path for this precedence question"},
	}
	result, err := RunInvocation(context.Background(), sb, manifest, os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("RunInvocation: %v", err)
	}
	if !result.Skipped || result.Attempted {
		t.Errorf("result = %+v, want Skipped=true, Attempted=false", result)
	}
	if result.SkipReason == "" {
		t.Error("SkipReason is empty, want the manifest's stated reason")
	}
}

func TestRunInvocationRejectsDisallowedArgs(t *testing.T) {
	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	manifest := InvocationManifest{
		Invoke: InvokeSpec{Attempted: true, Command: "echo", Args: []string{"--dangerously-bypass-approvals-and-sandbox"}},
	}
	if _, err := RunInvocation(context.Background(), sb, manifest, os.Getenv("PATH")); err == nil {
		t.Error("RunInvocation(disallowed arg) error = nil, want refusal")
	}
}

func TestRunInvocationSkipsWhenBinaryNotFound(t *testing.T) {
	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	manifest := InvocationManifest{
		Invoke: InvokeSpec{Attempted: true, Command: "definitely-not-a-real-binary-omca-qualify", Args: []string{"--version"}},
	}
	result, err := RunInvocation(context.Background(), sb, manifest, os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("RunInvocation: %v", err)
	}
	if !result.Skipped {
		t.Errorf("result = %+v, want Skipped=true when the binary is not on PATH", result)
	}
}

// TestRunInvocationRunsIsolatedFakeBinary exercises the real exec path
// (not skipped, not an error) against a harmless, hermetic fake "host
// binary" this test writes itself — never the real codex/claude — so the
// package's own test suite never depends on either being installed.
func TestRunInvocationRunsIsolatedFakeBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a POSIX shell script")
	}
	binDir := t.TempDir()
	fakeBinaryPath := filepath.Join(binDir, "fake-host")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"fake-host 9.9.9\"; fi\n"
	if err := os.WriteFile(fakeBinaryPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	manifest := InvocationManifest{
		Invoke: InvokeSpec{Attempted: true, Command: "fake-host", Args: []string{"--version"}},
	}

	result, err := RunInvocation(context.Background(), sb, manifest, binDir)
	if err != nil {
		t.Fatalf("RunInvocation: %v", err)
	}
	if result.Skipped {
		t.Fatalf("result = %+v, want not skipped", result)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; stderr=%q", result.ExitCode, result.Stderr)
	}
	if result.Stdout != "fake-host 9.9.9\n" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "fake-host 9.9.9\n")
	}
}
