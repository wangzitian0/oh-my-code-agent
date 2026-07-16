package context

import "testing"

func TestEnvironmentGet(t *testing.T) {
	env := Environment{Vars: []string{"PATH=/a:/b", "HOME=/home/x"}}

	if got := env.Get("PATH"); got != "/a:/b" {
		t.Errorf("Get(PATH) = %q, want %q", got, "/a:/b")
	}
	if got := env.Get("HOME"); got != "/home/x" {
		t.Errorf("Get(HOME) = %q, want %q", got, "/home/x")
	}
	if got := env.Get("MISSING"); got != "" {
		t.Errorf("Get(MISSING) = %q, want empty", got)
	}
}

func TestEnvironmentGetLastOccurrenceWins(t *testing.T) {
	env := Environment{Vars: []string{"FOO=1", "FOO=2"}}
	if got := env.Get("FOO"); got != "2" {
		t.Errorf("Get(FOO) = %q, want %q (last-occurrence-wins)", got, "2")
	}
}

func TestEnvironmentGetPrefixCollision(t *testing.T) {
	// "HOME_EXTRA=x" must never satisfy Get("HOME"): the match is on the
	// "KEY=" prefix, not any prefix of the raw string.
	env := Environment{Vars: []string{"HOME_EXTRA=x"}}
	if got := env.Get("HOME"); got != "" {
		t.Errorf("Get(HOME) = %q, want empty (must not match HOME_EXTRA)", got)
	}
}

func TestRealEnvironmentReflectsProcessEnv(t *testing.T) {
	t.Setenv("OMCA_CONTEXT_TEST_MARKER", "marker-value")
	env := RealEnvironment()
	if got := env.Get("OMCA_CONTEXT_TEST_MARKER"); got != "marker-value" {
		t.Errorf("RealEnvironment().Get(marker) = %q, want %q", got, "marker-value")
	}
}
