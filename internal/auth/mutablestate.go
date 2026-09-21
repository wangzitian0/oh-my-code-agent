package auth

import (
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// StateItem is one classified piece of host-written mutable state
// (docs/architecture/runtime.md §9). NativePath is relative to the host's
// primary native home (CODEX_HOME for codex, CLAUDE_CONFIG_DIR for
// claude-code — internal/context/host.go's codexNativeHomes/
// claudeNativeHomes) unless RelativeToHomeDir is true, in which case it is
// relative to $HOME directly (needed for Claude Code's ~/.claude.json,
// which docs/architecture/runtime.md §7.2 documents as carrying "account
// and OAuth state, project trust decisions, and parts of the MCP
// registry" in one mutable user state file).
//
// Correction (2026-07-20): RelativeToHomeDir's own doc text used to claim
// .claude.json sits "OUTSIDE the CLAUDE_CONFIG_DIR-relocatable tree" —
// imprecise. Read-only `strings` extraction against the installed `claude`
// binary (internal/context/host.go's claudeNativeHomes doc comment has the
// full evidence) shows CLAUDE_CONFIG_DIR, when explicitly SET, relocates
// .claude.json right along with the rest of the asset tree; only the
// UNSET/default case resolves it to bare $HOME instead of $HOME/.claude.
// RelativeToHomeDir has no consumer yet that resolves NativePath to a real
// absolute path (grep confirms only this package's own tests reference the
// field), so this was a latent doc inaccuracy, not a behavioral bug — noted
// here so a future consumer doesn't inherit the same wrong assumption
// internal/observe/rules.go's claudeUserRules briefly had.
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

// Matches reports whether a directory entry named name is this item.
//
// A trailing "/" in NativePath means "this directory". A "*" means a family
// of host files that carry a version number in the name, which is the shape
// that made this table rot by construction: the table listed
// "state_5.sqlite" by exact name, so when the same host also wrote
// goals_1.sqlite, logs_2.sqlite and memories_1.sqlite -- plus their -wal and
// -shm sidecars -- every one of them fell out of classification silently. A
// table keyed on a version number is a table that expires on the host's next
// release.
//
// Matching is deliberately anchored rather than a substring test: "*" stands
// for the version segment only, so a pattern can never widen into matching
// an unrelated file that merely shares a prefix.
func (s StateItem) Matches(name string) bool {
	pattern := strings.TrimSuffix(s.NativePath, "/")
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	prefix, suffix, _ := strings.Cut(pattern, "*")
	return len(name) > len(prefix)+len(suffix) &&
		strings.HasPrefix(name, prefix) &&
		strings.HasSuffix(name, suffix)
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
			NativePath: "*.sqlite", Class: domain.MutableStateGenerationLocal,
			Reason: "opaque, host-internal SQLite state; no fixture proves these are safe to share without cross-generation interference, so they default conservative like sessions. Matched as a family rather than by exact name: this row listed only state_5.sqlite, so goals_1.sqlite, logs_2.sqlite and memories_1.sqlite -- all observed in the same home -- fell out of classification entirely, and the next version bump would have dropped state_5 too",
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
			Host: "codex", Category: "sqlite", Name: "SQLite write-ahead log and shared-memory sidecars",
			NativePath: "*.sqlite-wal", Class: domain.MutableStateGenerationLocal,
			Reason: "a database's -wal/-shm sidecars are part of that database; classifying them apart from it would let the pair be split across scopes, which corrupts SQLite rather than merely misreporting it",
		},
		{
			Host: "codex", Category: "sqlite", Name: "SQLite shared-memory sidecars",
			NativePath: "*.sqlite-shm", Class: domain.MutableStateGenerationLocal,
			Reason: "see *.sqlite-wal: a sidecar shares its database's scope by construction",
		},
		{
			Host: "codex", Category: "credentials", Name: "captured shell environment snapshots",
			NativePath: "shell_snapshots/", Class: domain.MutableStateProhibitedImport,
			Reason: "these files are verbatim dumps of a shell environment, so they contain whatever secrets that shell carried -- a real snapshot on the maintainer's machine was found holding a 1Password service-account token in plaintext, world-readable. Same standard as auth.json (ADR 0003 decision item 3): state that mixes credential material with anything else is never copied or symlinked into isolation",
		},
		{
			Host: "codex", Category: "cache", Name: "installed plugin trees",
			NativePath: "plugins/", Class: domain.MutableStateWorkspaceShared,
			Reason: "a plugin install is a recreatable download, not per-checkout work: the same tree serves every worktree the same person opens. Observed at 26.6MB in one home, the second-largest single entry. Workspace rather than identity scope because a plugin set is part of how one domain of repositories is worked on, and should not follow a person into an unrelated employer's checkouts",
		},
		{
			Host: "codex", Category: "cache", Name: "model catalog cache",
			NativePath: "models_cache.json", Class: domain.MutableStateWorkspaceShared,
			Reason: "a fetched catalog of available models, recreatable and identical across checkouts; same reasoning as the plugin tree",
		},
		{
			Host: "codex", Category: "cache", Name: "host scratch directory",
			NativePath: "tmp/", Class: domain.MutableStateGenerationLocal,
			Reason: "scratch the host recreates on demand -- observed at 64.5MB, the single largest entry on this machine, including a full git clone under .tmp/plugins. Generation-local rather than shared precisely because nothing should have to survive here; classifying it is what makes it eligible to be pruned instead of invisible",
		},
		{
			Host: "codex", Category: "cache", Name: "host scratch directory (dot-prefixed variant)",
			NativePath: ".tmp/", Class: domain.MutableStateGenerationLocal,
			Reason: "the same scratch directory, observed under both spellings in different host versions; see tmp/",
		},
		{
			Host: "codex", Category: "sessions", Name: "command history",
			NativePath: "history.jsonl", Class: domain.MutableStateGenerationLocal,
			Reason: "named in the sessions row's own default list; given its own row so it is classified rather than merely mentioned",
		},
		{
			Host: "codex", Category: "installation-metadata", Name: "version marker file",
			NativePath: "version.json", Class: domain.MutableStateHostGlobalExternal,
			Reason: "the host's own record of which version wrote this home; owned by the host, regenerated in a fresh home, never migrated",
		},
		{
			Host: "codex", Category: "cache", Name: "host-managed skill tree",
			NativePath: "skills/", Class: domain.MutableStateGenerationLocal,
			Reason: "skills under the native home are host-managed content a generation is supposed to control, and the isolation invariant exists precisely to stop one generation's skill set leaking into another; conservative until a fixture proves a narrower share is safe",
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
			Host: "claude-code", Category: "cache", Name: "installed plugin trees",
			NativePath: "plugins/", Class: domain.MutableStateWorkspaceShared,
			Reason: "same reasoning as codex's plugin tree: a recreatable install, identical across every checkout one person opens, and scoped to a workspace rather than an identity so a plugin set does not follow a person between unrelated domains of work",
		},
		{
			Host: "claude-code", Category: "installation-metadata", Name: "settings backups and update markers",
			NativePath: "backups/", Class: domain.MutableStateGenerationLocal,
			Reason: "point-in-time copies of a generation's own settings; sharing them across generations would let one generation restore another's configuration, which is the drift the immutable-generation model exists to prevent",
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
