package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunDiff_CurrentPending_Human(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runDiff(&stdout, &stderr, []string{"current", "pending"})
	if code != 0 {
		t.Fatalf("runDiff current pending = %d; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "CURRENT") || !strings.Contains(stdout.String(), "PENDING") {
		t.Errorf("output missing plane names:\n%s", stdout.String())
	}
}

func TestRunDiff_UnknownPlaneName(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runDiff(&stdout, &stderr, []string{"bogus", "pending"})
	if code != 2 {
		t.Fatalf("runDiff bogus pending = %d, want 2", code)
	}
}

func TestRunDiff_WrongArgCount(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runDiff(&stdout, &stderr, []string{"current"})
	if code != 2 {
		t.Fatalf("runDiff current (missing second plane) = %d, want 2", code)
	}
}
