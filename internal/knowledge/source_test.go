package knowledge

import "testing"

func TestOfficialSources_NonEmpty(t *testing.T) {
	if len(OfficialSources()) == 0 {
		t.Fatal("OfficialSources() is empty, want the real allowlisted sources")
	}
}

func TestOfficialSources_ReturnsDefensiveCopy(t *testing.T) {
	got := OfficialSources()
	got[0].URL = "https://not-real.example/tampered"

	again := OfficialSources()
	if again[0].URL == "https://not-real.example/tampered" {
		t.Fatal("mutating the slice OfficialSources() returned affected the package's own allowlist -- want a defensive copy")
	}
}

func TestOfficialSourcesForHost_FiltersByHost(t *testing.T) {
	codex := OfficialSourcesForHost("codex")
	if len(codex) == 0 {
		t.Fatal("OfficialSourcesForHost(codex) is empty")
	}
	for _, s := range codex {
		if s.Host != "codex" {
			t.Errorf("OfficialSourcesForHost(codex) returned a %q entry", s.Host)
		}
	}

	unknown := OfficialSourcesForHost("no-such-host")
	if len(unknown) != 0 {
		t.Errorf("OfficialSourcesForHost(no-such-host) = %v, want empty", unknown)
	}
}

func TestOfficialSourcesForHost_ClaudeCode(t *testing.T) {
	sources := OfficialSourcesForHost("claude-code")
	if len(sources) == 0 {
		t.Fatal("OfficialSourcesForHost(claude-code) is empty")
	}
	for _, s := range sources {
		if s.Host != "claude-code" {
			t.Errorf("got host %q", s.Host)
		}
	}
}

func TestValidateSource_AcceptsEveryAllowlistedEntry(t *testing.T) {
	for _, s := range OfficialSources() {
		if err := ValidateSource(s); err != nil {
			t.Errorf("ValidateSource(%+v): %v", s, err)
		}
	}
}

func TestValidateSource_RejectsArbitraryCallerSuppliedURL(t *testing.T) {
	s := Source{Host: "codex", SourceID: "codex-cli-doc", Kind: "official-doc", URL: "https://attacker.example/malicious"}
	if err := ValidateSource(s); err == nil {
		t.Fatal("ValidateSource: want an error for a URL not on the allowlist, got nil")
	}
}

func TestValidateSource_RejectsUnknownSourceID(t *testing.T) {
	s := Source{Host: "codex", SourceID: "not-a-real-source-id", Kind: "official-doc", URL: "https://learn.chatgpt.com/docs/codex/cli"}
	if err := ValidateSource(s); err == nil {
		t.Fatal("ValidateSource: want an error for a sourceId not on the allowlist, got nil")
	}
}

func TestValidateSource_RejectsWrongHostForRealURL(t *testing.T) {
	// The URL and sourceId are each individually real, but paired with the
	// wrong host -- ValidateSource must reject the whole struct, not just
	// check the URL in isolation.
	s := Source{Host: "claude-code", SourceID: "codex-cli-doc", Kind: "official-doc", URL: "https://learn.chatgpt.com/docs/codex/cli"}
	if err := ValidateSource(s); err == nil {
		t.Fatal("ValidateSource: want an error when host does not match the sourceId's real allowlisted host")
	}
}

func TestOfficialSources_EveryURLIsHTTPSAndOnAnAllowlistedDomain(t *testing.T) {
	for _, s := range OfficialSources() {
		if err := validateOfficialSourceURL(s); err != nil {
			t.Errorf("validateOfficialSourceURL(%+v): %v", s, err)
		}
	}
}
