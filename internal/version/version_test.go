package version

import "testing"

func TestString(t *testing.T) {
	got := String()
	if got == "" {
		t.Fatal("String() returned empty string")
	}
	if got[:5] != "omca " {
		t.Fatalf("String() = %q, want prefix %q", got, "omca ")
	}
}
