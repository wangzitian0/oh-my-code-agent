package main

import (
	"os"
	"testing"
)

func TestRunVersion(t *testing.T) {
	code := run([]string{"version"}, os.Stdout, os.Stderr)
	if code != 0 {
		t.Fatalf("run([version]) = %d, want 0", code)
	}
}

func TestRunNoArgs(t *testing.T) {
	code := run(nil, os.Stdout, os.Stderr)
	if code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	code := run([]string{"bogus"}, os.Stdout, os.Stderr)
	if code != 2 {
		t.Fatalf("run([bogus]) = %d, want 2", code)
	}
}
