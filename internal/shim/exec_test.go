package shim

import (
	"reflect"
	"testing"
)

// TestInjectEnv_ReplacesExistingKey proves InjectEnv actually overrides a
// key already present in environ (the realistic case: a nested `omca run`
// inside an already-managed shell whose CODEX_HOME already points at a
// DIFFERENT generation) rather than merely appending, which would leave
// the stale value shadowing or duplicated ahead of the injected one for a
// consumer that reads the first occurrence instead of the last.
func TestInjectEnv_ReplacesExistingKey(t *testing.T) {
	environ := []string{"HOME=/Users/alice", "CODEX_HOME=/stale/generation", "PATH=/usr/bin"}
	got := InjectEnv(environ, map[string]string{"CODEX_HOME": "/fresh/generation"})

	count := 0
	found := false
	for _, kv := range got {
		if kv == "CODEX_HOME=/stale/generation" {
			t.Errorf("InjectEnv left the stale CODEX_HOME entry in place: %v", got)
		}
		if kv == "CODEX_HOME=/fresh/generation" {
			found = true
			count++
		}
	}
	if !found {
		t.Errorf("InjectEnv did not add CODEX_HOME=/fresh/generation: %v", got)
	}
	if count != 1 {
		t.Errorf("InjectEnv produced %d CODEX_HOME entries, want exactly 1: %v", count, got)
	}
}

// TestInjectEnv_PreservesUnrelatedEntries proves InjectEnv leaves every
// entry not named by overrides untouched, in its original relative order.
func TestInjectEnv_PreservesUnrelatedEntries(t *testing.T) {
	environ := []string{"HOME=/Users/alice", "PATH=/usr/bin", "LANG=en_US.UTF-8"}
	got := InjectEnv(environ, map[string]string{"CODEX_HOME": "/gen"})

	want := append(append([]string{}, environ...), "CODEX_HOME=/gen")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("InjectEnv = %v, want %v", got, want)
	}
}

// TestInjectEnv_MultipleOverrides_DeterministicOrder proves multiple
// overrides are appended in a stable (sorted-key) order across repeated
// calls, so callers building the same override set never see
// nondeterministic env slices.
func TestInjectEnv_MultipleOverrides_DeterministicOrder(t *testing.T) {
	overrides := map[string]string{"OMCA_RUN_ID": "generation:sha256:abc", "CODEX_HOME": "/gen/codex-home"}
	first := InjectEnv(nil, overrides)
	second := InjectEnv(nil, overrides)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("InjectEnv is not deterministic across identical calls: %v != %v", first, second)
	}
	want := []string{"CODEX_HOME=/gen/codex-home", "OMCA_RUN_ID=generation:sha256:abc"}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("InjectEnv = %v, want %v (sorted-by-key)", first, want)
	}
}

// TestExecReplace_EmptyBinaryPath proves ExecReplace fails closed on an
// empty binary path rather than handing "" to syscall.Exec.
func TestExecReplace_EmptyBinaryPath(t *testing.T) {
	if err := ExecReplace("", []string{"x"}, nil); err == nil {
		t.Fatal("ExecReplace(\"\", ...): want error, got nil")
	}
}

// TestExecReplace_EmptyArgv proves ExecReplace fails closed on an empty
// argv rather than handing a nil argv[0] to syscall.Exec.
func TestExecReplace_EmptyArgv(t *testing.T) {
	if err := ExecReplace("/bin/true", nil, nil); err == nil {
		t.Fatal("ExecReplace(_, nil, _): want error, got nil")
	}
}
