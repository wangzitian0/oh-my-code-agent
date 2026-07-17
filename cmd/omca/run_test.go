package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// TestParseRunArgs_ModeBeforeHost proves `--mode X <host>` parses (this
// issue's own synopsis order).
func TestParseRunArgs_ModeBeforeHost(t *testing.T) {
	got, err := parseRunArgs([]string{"--mode", "native", "codex"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	want := runArgs{Mode: "native", Host: "codex"}
	if got.Mode != want.Mode || got.Host != want.Host {
		t.Errorf("parseRunArgs = %+v, want %+v", got, want)
	}
}

// TestParseRunArgs_HostBeforeMode proves `<host> --mode X` also parses
// (docs/architecture/runtime.md §11's own example order: "omca run codex
// --mode isolated").
func TestParseRunArgs_HostBeforeMode(t *testing.T) {
	got, err := parseRunArgs([]string{"codex", "--mode=isolated"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	want := runArgs{Mode: "isolated", Host: "codex"}
	if got.Mode != want.Mode || got.Host != want.Host {
		t.Errorf("parseRunArgs = %+v, want %+v", got, want)
	}
}

// TestParseRunArgs_DefaultModeIsIsolated proves the default mode is
// "isolated" (docs/architecture/runtime.md §11: "isolated is the default
// managed path") when --mode is never given.
func TestParseRunArgs_DefaultModeIsIsolated(t *testing.T) {
	got, err := parseRunArgs([]string{"claude"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if got.Mode != "isolated" {
		t.Errorf("Mode = %q, want %q", got.Mode, "isolated")
	}
}

// TestParseRunArgs_PassthroughAfterDoubleDash proves a literal "--"
// forwards everything after it verbatim, even values that look like flags.
func TestParseRunArgs_PassthroughAfterDoubleDash(t *testing.T) {
	got, err := parseRunArgs([]string{"codex", "--", "--mode", "not-a-flag-here"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	want := []string{"--mode", "not-a-flag-here"}
	if !reflect.DeepEqual(got.Passthrough, want) {
		t.Errorf("Passthrough = %v, want %v", got.Passthrough, want)
	}
}

// TestParseRunArgs_MissingModeValue proves a trailing bare --mode is a
// clean parse error, not an index-out-of-range panic.
func TestParseRunArgs_MissingModeValue(t *testing.T) {
	if _, err := parseRunArgs([]string{"codex", "--mode"}); err == nil {
		t.Fatal("parseRunArgs([codex, --mode]): want error, got nil")
	}
}

// TestNormalizeHostArg_ClaudeAlias proves "claude" (the binary name) is
// accepted as a friendlier alias for the canonical host ID "claude-code".
func TestNormalizeHostArg_ClaudeAlias(t *testing.T) {
	got, err := normalizeHostArg("claude")
	if err != nil {
		t.Fatalf("normalizeHostArg(claude): %v", err)
	}
	if got != "claude-code" {
		t.Errorf("normalizeHostArg(claude) = %q, want %q", got, "claude-code")
	}
}

// TestNormalizeHostArg_MissingHost proves an empty host argument is a
// clear, actionable error rather than silently picking a default host.
func TestNormalizeHostArg_MissingHost(t *testing.T) {
	if _, err := normalizeHostArg(""); err == nil {
		t.Fatal("normalizeHostArg(\"\"): want error, got nil")
	}
}

// TestNormalizeHostArg_Unrecognized proves an unrecognized host argument
// (e.g. a typo, or a host this project does not detect) is rejected.
func TestNormalizeHostArg_Unrecognized(t *testing.T) {
	if _, err := normalizeHostArg("opencode"); err == nil {
		t.Fatal("normalizeHostArg(opencode): want error, got nil")
	}
}

// TestRunRun_UnknownMode_Errors proves an unrecognized --mode value is a
// clean, pre-exec error rather than falling through to an implicit
// default.
func TestRunRun_UnknownMode_Errors(t *testing.T) {
	setupManagedTestEnv(t, true, false)

	var stdout, stderr bytes.Buffer
	code := runRun(&stdout, &stderr, []string{"codex", "--mode", "bogus"})
	if code != 2 {
		t.Fatalf("runRun(--mode bogus) = %d, want 2; stderr:\n%s", code, stderr.String())
	}
}

// TestRunRun_NativeMode_WarnsBeforeFailingToResolve proves `--mode native`
// prints its unmanaged warning to stderr (issue #14's literal AC) even on
// the error path (here, no real "codex" binary reachable once the shim
// directory — which does not exist in this fixture, but the code path is
// identical — is excluded), and that the warning lands on stderr, never
// stdout, so a caller capturing stdout never sees it mixed in.
//
// This test cannot exercise the success path (a real exec that replaces
// the test process) — that is cmd/omca/shim_test.go's job via a real
// subprocess; this proves only the pre-exec warning-and-diagnostics
// behavior that IS safe to run in-process.
func TestRunRun_NativeMode_WarnsBeforeFailingToResolve(t *testing.T) {
	setupManagedTestEnv(t, false, false) // no fake codex binary on PATH at all

	var stdout, stderr bytes.Buffer
	code := runRun(&stdout, &stderr, []string{"codex", "--mode", "native"})
	if code != 1 {
		t.Fatalf("runRun(--mode native) with no codex on PATH = %d, want 1; stderr:\n%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty (native mode must never write to stdout)", stdout.String())
	}
	if !strings.Contains(stderr.String(), "UNMANAGED") {
		t.Errorf("stderr does not contain the unmanaged warning: %q", stderr.String())
	}
}

// TestRunRun_IsolatedMode_HostNotInstalled proves `--mode isolated`
// (the default) fails clearly rather than compiling a generation for a
// host it cannot find.
func TestRunRun_IsolatedMode_HostNotInstalled(t *testing.T) {
	setupManagedTestEnv(t, false, false)

	var stdout, stderr bytes.Buffer
	code := runRun(&stdout, &stderr, []string{"codex"})
	if code != 1 {
		t.Fatalf("runRun(codex) with codex not installed = %d, want 1; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not installed") {
		t.Errorf("stderr does not mention 'not installed': %q", stderr.String())
	}
}
