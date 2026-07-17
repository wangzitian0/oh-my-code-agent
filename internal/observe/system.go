package observe

// SystemRoot is one machine/managed-scope location this package may
// inventory (docs/ontology/README.md §2's `managed` scope: "Organization,
// fleet, or machine — Admin service, MDM, /etc, system policy"; §6.1/§6.2's
// "managed CLAUDE.md" / "managed policy" and "/etc/codex" rows). Like
// hostcontext.NativeHome, this package never resolves SystemRoot locations
// itself from an environment variable, a hardcoded absolute path, or any
// other ambient machine state: Request.SystemRoots is the only source, kept
// consistent with request.go's "every path it walks comes from this struct"
// invariant. DefaultSystemRoots below is an opt-in convenience a real caller
// may use to populate that field with this platform's conventional
// locations — Observe itself never calls it.
type SystemRoot struct {
	// Name identifies which system root this is, e.g. "ETC_CODEX" or
	// "CLAUDE_MANAGED", mirroring hostcontext.NativeHome.Name.
	Name string
	// Path is the resolved absolute path.
	Path string
}

// DefaultSystemRoots returns this platform's conventional machine/managed
// scope location(s) for host, or nil if this package has no documented
// system-scope source for host. This is a convenience for a real caller
// (e.g. a future `omca observe` CLI command) to populate Request.SystemRoots
// with — Observe never calls it itself, keeping every path Observe actually
// walks explicit and caller-supplied (see the SystemRoot doc comment and
// request.go). Every returned path is a fixed, well-known machine location
// (never derived from HOME or any other per-user env var), so calling this
// is safe even outside a sandboxed test: reading a system-wide path (as
// opposed to writing to it, which this package never does anywhere) carries
// none of the "don't touch the real ~/.claude or ~/.codex" risk documented
// in fixtures/README.md, which is specifically about per-user HOME-rooted
// state a live Claude Code/Codex session also reads and writes. Test code in
// this package must still never call this directly against paths it does
// not control — see system_test.go, which always builds its own synthetic
// SystemRoot pointing at a t.TempDir(), exactly like every other test in
// this package.
//
// Only macOS locations are known (docs/project/roadmap.md: "First
// Implementation Slice" is macOS-only, matching
// internal/context/host.go's isExecutableFile same-scoped limitation).
func DefaultSystemRoots(host string) []SystemRoot {
	switch host {
	case "codex":
		// docs/ontology/README.md §6.2: Settings "/etc/codex/config.toml";
		// Skills "/etc/codex/skills". One root covers both, plus the
		// separately-named managed `requirements.toml` guardrail file
		// (Settings row: "Managed requirements.toml is a guardrail, not an
		// ordinary lower layer").
		return []SystemRoot{{Name: "ETC_CODEX", Path: "/etc/codex"}}
	case "claude-code":
		// docs/ontology/README.md §6.1: Instructions "managed CLAUDE.md";
		// Hooks/plugins "plugin manifests and enabled-plugin settings";
		// Policy/state "settings permissions/sandbox/trust" — all under one
		// managed root. The exact directory (not independently confirmed by
		// this project beyond the official settings doc's general "managed
		// policy" language, unlike Codex's /etc/codex which the ontology
		// table names literally) follows Claude Code's documented managed
		// settings location on macOS.
		return []SystemRoot{{Name: "CLAUDE_MANAGED", Path: "/Library/Application Support/ClaudeCode"}}
	default:
		return nil
	}
}

// codexSystemRules returns the source rules checked under Codex's
// machine/managed-scope root (docs/ontology/README.md §6.2). Unlike
// codexUserRules/codexWorkspaceRules, Instructions has no documented
// system-scope source for Codex (the ontology table names no managed/system
// Instructions physical source), so there is no conceptInstruction rule
// here — a deliberate omission, not an oversight (see coverage.go, which
// marks this cell UNKNOWN rather than silently absent from the table).
func codexSystemRules() []sourceRule {
	return []sourceRule{
		{concept: conceptMCPServer, kind: ruleCandidateFiles, files: []string{"config.toml"}},
		{concept: conceptHook, kind: ruleCandidateFiles, files: []string{"config.toml"}},
		{concept: conceptPolicy, kind: ruleCandidateFiles, files: []string{"config.toml"}},
		// "Managed requirements.toml is a guardrail" (Settings row) is its
		// own, separately-named file distinct from config.toml.
		{concept: conceptPolicy, kind: ruleCandidateFiles, files: []string{"requirements.toml"}},
		{concept: conceptSkill, kind: ruleWalkDir, dir: "skills", marker: "SKILL.md"},
		{concept: conceptPlugin, kind: ruleWalkDir, dir: "", marker: "plugin.json"},
	}
}

// claudeSystemRules returns the source rules checked under Claude Code's
// managed-scope root (docs/ontology/README.md §6.1). MCP has no
// independently confirmed system-scope physical filename beyond the same
// managed-settings.json this function already reads for Hooks/Policy — this
// package does not fold an unconfirmed "managed MCP" claim into that file's
// mcp_server tag (see doc.go's knownUnknowns-style note); Skills' "enterprise"
// scope is likewise named by the ontology table without a stated path, so
// there is no conceptSkill rule here either. Both omissions are deliberate,
// documented gaps (coverage.go marks both cells UNKNOWN), not oversights.
func claudeSystemRules() []sourceRule {
	return []sourceRule{
		{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"CLAUDE.md"}},
		{concept: conceptHook, kind: ruleCandidateFiles, files: []string{"managed-settings.json"}},
		{concept: conceptPolicy, kind: ruleCandidateFiles, files: []string{"managed-settings.json"}},
		{concept: conceptPlugin, kind: ruleCandidateFiles, files: []string{"managed-settings.json"}},
	}
}
