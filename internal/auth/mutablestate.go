package auth

import "github.com/wangzitian0/oh-my-code-agent/internal/domain"

// StateItem is one classified piece of host-written mutable state
// (docs/architecture/runtime.md §9). NativePath is relative to the host's
// primary native home (CODEX_HOME for codex, CLAUDE_CONFIG_DIR for
// claude-code — internal/context/host.go's codexNativeHomes/
// claudeNativeHomes) unless RelativeToHomeDir is true, in which case it is
// relative to $HOME directly (needed for Claude Code's ~/.claude.json,
// which docs/architecture/runtime.md §7.2 explicitly documents as a
// separate file OUTSIDE the CLAUDE_CONFIG_DIR-relocatable tree: "account
// and OAuth state, project trust decisions, and parts of the MCP registry
// share one mutable user state file").
type StateItem struct {
	Host     string
	Category string
	Name     string
	// NativePath is the real, observed location, gathered by a read-only
	// directory-structure listing of the maintainer's own installed ~/.codex
	// and ~/.claude on 2026-07-18 (filenames/directory names only — no file
	// content was read), the same static-inspection evidentiary standard
	// fixtures/README.md already uses for an unqualified finding.
	NativePath        string
	RelativeToHomeDir bool
	Class             domain.MutableStateClass
	Reason            string
}

// RequiredCategories are the mutable-state classes docs/architecture/
// runtime.md §9 lists by name: "sessions and archived sessions; logs and
// crash reports; SQLite databases; model or provider caches; trust
// decisions; memory; and installation metadata." ClassificationTable's
// combined output (across both hosts) must cover every one of these at
// least once — see TestClassificationTable_CoversRequiredCategories.
var RequiredCategories = []string{
	"sessions", "logs", "sqlite", "cache", "trust", "memory", "installation-metadata",
}

// ClassificationTable returns every classified StateItem for host. Codex
// and Claude Code do not each have a native example of every category (e.g.
// no *.sqlite file was observed anywhere under the maintainer's real
// ~/.claude; no distinct trust-decision file separate from config.toml was
// observed under ~/.codex) — this function reports that honestly (a missing
// category for one host, not a fabricated path) rather than inventing a
// location neither host actually uses; RequiredCategories' coverage is
// checked across BOTH hosts combined.
func ClassificationTable(host string) ([]StateItem, error) {
	switch host {
	case "codex":
		return codexClassification(), nil
	case "claude-code":
		return claudeClassification(), nil
	default:
		if err := domain.ValidateHostID(host); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

// codexClassification classifies CODEX_HOME's mutable state. Evidence: a
// read-only `find ~/.codex -maxdepth 2 -type d` / `ls -la ~/.codex` listing
// (directory and file NAMES only; no content read).
func codexClassification() []StateItem {
	return []StateItem{
		{
			Host: "codex", Category: "credentials", Name: "OAuth/API-key token cache",
			NativePath: "auth.json", Class: domain.MutableStateProhibitedImport,
			Reason: "ADR 0003 decision item 3: automatic copying or broad symlinking of a native auth.json is prohibited outright, regardless of fallback rung",
		},
		{
			Host: "codex", Category: "sessions", Name: "session transcripts",
			NativePath: "sessions/", Class: domain.MutableStateGenerationLocal,
			Reason: "no fixture yet proves cross-generation session replay is safe; conservative default keeps each generation's own session history scoped to itself (`archived_sessions/`, `session_index.jsonl`, `history.jsonl` follow the same default)",
		},
		{
			Host: "codex", Category: "logs", Name: "login/diagnostic logs",
			NativePath: "log/", Class: domain.MutableStateGenerationLocal,
			Reason: "a generation's own run diagnostics (e.g. codex-login.log); not shared, so one generation's failure diagnostics never get attributed to another",
		},
		{
			Host: "codex", Category: "sqlite", Name: "state/memory/log SQLite databases",
			NativePath: "state_5.sqlite", Class: domain.MutableStateGenerationLocal,
			Reason: "opaque, host-internal SQLite state (also observed: memories_1.sqlite, logs_2.sqlite, goals_1.sqlite); no fixture proves these are safe to share without cross-generation interference, so they default conservative like sessions",
		},
		{
			Host: "codex", Category: "cache", Name: "model/plugin/app metadata cache",
			NativePath: "cache/", Class: domain.MutableStateWorktreeShared,
			Reason: "recreatable, non-sensitive metadata (models_cache.json and cache/ subdirectories); low risk to share across generations in the same worktree to avoid redundant re-fetches -- this is the concrete allowlisted class Allowlist backs with a fixture (allowlist.go)",
		},
		{
			Host: "codex", Category: "trust", Name: "project-directory trust posture",
			NativePath: "config.toml", Class: domain.MutableStateHostGlobalExternal,
			Reason: "no distinct native trust-decision file was identified separately from config.toml in this evidence snapshot; config.toml is already treated as native global configuration this compiler never copies into a generation (docs/architecture/runtime.md §2 threat model)",
		},
		{
			Host: "codex", Category: "memory", Name: "long-term memory notes",
			NativePath: "memories/", Class: domain.MutableStateGenerationLocal,
			Reason: "no fixture yet proves cross-generation or cross-identity memory sharing is safe; conservative default until one exists, mirroring sessions/sqlite above",
		},
		{
			Host: "codex", Category: "installation-metadata", Name: "installation id and version marker",
			NativePath: "installation_id", Class: domain.MutableStateHostGlobalExternal,
			Reason: "identifies the real host binary installation itself, shared across every identity/worktree on the machine (version.json is the same class); an isolated CODEX_HOME simply does not have one and Codex is expected to generate its own if needed -- OMCA never migrates this",
		},
	}
}

// claudeClassification classifies Claude Code's mutable state, split across
// CLAUDE_CONFIG_DIR (default ~/.claude) and the separate ~/.claude.json
// state file docs/architecture/runtime.md §7.2 names explicitly. Evidence:
// a read-only `ls -la`/`find -maxdepth 2` listing of the maintainer's real
// ~/.claude and ~/.claude.json (names only; no content read).
func claudeClassification() []StateItem {
	return []StateItem{
		{
			Host: "claude-code", Category: "trust", Name: "account/OAuth/trust/MCP-registry state file",
			NativePath: ".claude.json", RelativeToHomeDir: true, Class: domain.MutableStateProhibitedImport,
			Reason: "ADR 0003 decision item 4: this single native file mixes identity-shared account/OAuth credential state with project trust decisions and MCP registry entries; no fixture proves a safe narrow extraction, so per the ADR's own 'cannot be shared safely' branch this file is never copied/symlinked as a whole -- the identity gets rung 3's explicit login flow instead of an unsafe share",
		},
		{
			Host: "claude-code", Category: "sessions", Name: "per-project session transcripts and history",
			NativePath: "projects/", Class: domain.MutableStateGenerationLocal,
			Reason: "conservative default, matching Codex's sessions classification (sessions/, history.jsonl, shell-snapshots/ follow the same default); no fixture yet proves cross-generation session continuity is safe",
		},
		{
			Host: "claude-code", Category: "logs", Name: "daemon and debug logs",
			NativePath: "daemon.log", Class: domain.MutableStateGenerationLocal,
			Reason: "a generation's own run diagnostics (debug/ follows the same default); not shared",
		},
		{
			Host: "claude-code", Category: "cache", Name: "stats/history/PR-status caches",
			NativePath: "cache/", Class: domain.MutableStateWorktreeShared,
			Reason: "recreatable, non-sensitive cache data (stats-cache.json, gh-pr-status-cache.json, paste-cache/, file-history/ follow the same default); the second concrete allowlisted-class example this project ships a fixture for (allowlist.go), reusing the identical rationale as Codex's cache/",
		},
		{
			Host: "claude-code", Category: "memory", Name: "user-global CLAUDE.md memory",
			NativePath: "CLAUDE.md", Class: domain.MutableStateProhibitedImport,
			Reason: "user-global Instructions/Memory content is exactly what docs/architecture/runtime.md's opening invariant forbids as an implicit parent ('observe native configuration, but do not inherit it implicitly'); already excluded from every claude-code generation via CLAUDE_CONFIG_DIR relocation and tracked as issue #47's own capability gap -- classified prohibited import here for consistency, not a new gap",
		},
		{
			Host: "claude-code", Category: "installation-metadata", Name: "remote-managed settings snapshot",
			NativePath: "remote-settings.json", Class: domain.MutableStateHostGlobalExternal,
			Reason: "a server-managed policy/settings snapshot analogous to /etc-style managed policy (docs/adr/0002-ownership.md's `external`); OMCA never writes or copies it into a generation",
		},
	}
}
