package domain

import "testing"

func TestValidateHostID(t *testing.T) {
	cases := []struct {
		id      string
		wantErr bool
	}{
		{"claude-code", false},
		{"codex", false},
		{"opencode", false},
		{"cursor", false},
		{"github-copilot", false},
		{"antigravity-cli", false},
		{"pi", false},
		{"openclaw", false},
		{"hermes-agent", false},
		{"claude", true},
		{"", true},
		{"gpt-5", true},
	}
	for _, c := range cases {
		err := ValidateHostID(c.id)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateHostID(%q) error = %v, wantErr %v", c.id, err, c.wantErr)
		}
	}
}

func TestValidateHostIDs(t *testing.T) {
	if err := ValidateHostIDs([]string{"codex", "claude-code"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateHostIDs([]string{"codex", "not-a-host"}); err == nil {
		t.Fatal("expected an error for an unknown host id in the list")
	}
	if err := ValidateHostIDs(nil); err != nil {
		t.Fatalf("empty selector should be valid: %v", err)
	}
}
