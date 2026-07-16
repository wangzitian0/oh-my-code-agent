package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	got := String()
	if got == "" {
		t.Fatal("String() returned empty string")
	}
	if !strings.HasPrefix(got, "omca ") {
		t.Fatalf("String() = %q, want prefix %q", got, "omca ")
	}
}
