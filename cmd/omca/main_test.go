package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run([version]) = %d, want 0", code)
	}
	if !strings.HasPrefix(stdout.String(), "omca ") {
		t.Errorf("stdout = %q, want prefix %q", stdout.String(), "omca ")
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), usage) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), usage)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run([bogus]) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), usage) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), usage)
	}
}
