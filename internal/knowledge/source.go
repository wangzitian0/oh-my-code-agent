package knowledge

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Source is one allowlisted official upstream source a Poller may fetch for
// one host -- closed, fixture-backed, explicitly enumerated ahead of time,
// mirroring internal/auth/allowlist.go's AllowlistedShare discipline for
// symlinks ("no code path accepts an arbitrary caller-supplied path")
// applied here to network fetches instead of filesystem shares.
//
// SourceID matches the KnowledgeEvidenceRef.ID a host's currently published
// Knowledge Pack(s) record for this exact source (docs/knowledge/README.md
// §4's evidence list), so PollSource can look up "what digest did the
// currently published Pack record for this" without any string-matching
// guesswork.
type Source struct {
	Host     string
	SourceID string
	Kind     string
	URL      string
}

// officialSources is the closed, exhaustive set of official sources this
// project currently polls. It is grounded directly in the real, committed
// Knowledge Packs' own evidence citations
// (knowledge/hosts/{codex,claude-code}/cli/*/manifest.json, "kind":
// "official-doc" entries) -- not invented from nothing, matching
// docs/knowledge/README.md §12's governance requirement: "Evidence URLs must
// be allowlisted official domains or pinned official source repositories."
// Adding a new source means adding a real evidence entry to a Pack manifest
// (or amending this list to name one), reviewed the same way any other
// change to this file is -- it is never something a caller can broaden at
// runtime.
//
// Deliberately unexported for the same reason internal/auth/allowlist.go's
// own allowlist var is: an exported, mutable package-level slice could be
// reassigned or appended to by any other package in the module, silently
// defeating the "closed allowlist" guarantee this file exists to provide.
// OfficialSources/OfficialSourcesForHost (below) are the read-only,
// defensive-copy accessors.
var officialSources = []Source{
	{Host: "claude-code", SourceID: "claude-code-settings-doc", Kind: "official-doc", URL: "https://code.claude.com/docs/en/settings"},
	{Host: "claude-code", SourceID: "claude-code-memory-doc", Kind: "official-doc", URL: "https://code.claude.com/docs/en/memory"},
	{Host: "claude-code", SourceID: "claude-code-skills-doc", Kind: "official-doc", URL: "https://code.claude.com/docs/en/skills"},
	{Host: "codex", SourceID: "codex-environment-variables-doc", Kind: "official-doc", URL: "https://learn.chatgpt.com/docs/config-file/environment-variables"},
	{Host: "codex", SourceID: "codex-cli-doc", Kind: "official-doc", URL: "https://learn.chatgpt.com/docs/codex/cli"},
	{Host: "codex", SourceID: "codex-agents-md-doc", Kind: "official-doc", URL: "https://learn.chatgpt.com/docs/agent-configuration/agents-md"},
	{Host: "codex", SourceID: "codex-skills-doc", Kind: "official-doc", URL: "https://learn.chatgpt.com/docs/build-skills"},
}

// OfficialSources returns a defensive copy of the current closed allowlist.
func OfficialSources() []Source {
	return append([]Source(nil), officialSources...)
}

// OfficialSourcesForHost returns a defensive copy of every allowlisted
// Source for host, in the order they were declared.
func OfficialSourcesForHost(host string) []Source {
	var out []Source
	for _, s := range officialSources {
		if s.Host == host {
			out = append(out, s)
		}
	}
	return out
}

// ValidateSource rejects a Source that is not exactly one of the allowlist
// entries above -- host, sourceId, kind, and URL must all match a real
// entry. This is the single choke point PollSource calls before ever
// fetching anything, so no caller (including a future CLI command) can
// construct an ad hoc Source naming an arbitrary URL and have it actually
// fetched.
func ValidateSource(s Source) error {
	for _, allowed := range officialSources {
		if allowed == s {
			return nil
		}
	}
	return fmt.Errorf("knowledge: ValidateSource: %+v is not one of the allowlisted official sources -- refusing to fetch an arbitrary, non-allowlisted source", s)
}

// validateOfficialSourceURL is init()'s own defense-in-depth structural
// check on the literal allowlist above: every URL must be https and target
// one of the domains this project's real, committed Knowledge Pack evidence
// already cites (code.claude.com, learn.chatgpt.com) -- so a future edit to
// this file that accidentally adds a non-HTTPS or unrecognized-domain entry
// fails loudly at package load, the same discipline
// internal/auth/allowlist.go's init()+ValidateAllowlist pair applies to its
// own symlink allowlist.
func validateOfficialSourceURL(s Source) error {
	u, err := url.Parse(s.URL)
	if err != nil {
		return fmt.Errorf("source %q: invalid URL %q: %w", s.SourceID, s.URL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("source %q: URL %q is not https", s.SourceID, s.URL)
	}
	if !allowlistedOfficialDomains[u.Host] {
		return fmt.Errorf("source %q: host %q is not one of the allowlisted official domains", s.SourceID, u.Host)
	}
	if strings.TrimSpace(s.SourceID) == "" {
		return fmt.Errorf("source with URL %q has an empty sourceId", s.URL)
	}
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("source %q has an empty host", s.SourceID)
	}
	return nil
}

// allowlistedOfficialDomains is the closed set of domains this project's
// real committed Knowledge Pack evidence cites today for its two Tier-1
// hosts (docs/knowledge/README.md §12: "Tier 1 (Claude Code, Codex):
// maintainers keep their Knowledge fresh"). Grounded in
// knowledge/hosts/claude-code/cli/2.1/manifest.json and
// knowledge/hosts/codex/cli/0.144/manifest.json's own evidence[].url
// entries -- not invented independently of what this repository already
// treats as official.
var allowlistedOfficialDomains = map[string]bool{
	"code.claude.com":   true,
	"learn.chatgpt.com": true,
}

func init() {
	seen := make(map[string]bool, len(officialSources))
	for _, s := range officialSources {
		if err := domain.ValidateHostID(s.Host); err != nil {
			panic("knowledge: the production official-sources allowlist itself is invalid: " + err.Error())
		}
		if err := validateOfficialSourceURL(s); err != nil {
			panic("knowledge: the production official-sources allowlist itself is invalid: " + err.Error())
		}
		key := s.Host + "/" + s.SourceID
		if seen[key] {
			panic(fmt.Sprintf("knowledge: the production official-sources allowlist declares %q more than once", key))
		}
		seen[key] = true
	}
}
