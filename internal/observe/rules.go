package observe

import "path/filepath"

// Concept IDs this package tags every Observation with. PR-08 (issue #12)
// established the first three, matching docs/ontology/README.md §1.1's
// entity table (instruction, skill, mcp_server). PR-16 (issue #20, "Deep
// observation") adds the three remaining concepts issue #20's round-2 audit
// names explicitly: hook, policy (this package's chosen ID for "permissions/
// trust" — docs/ontology/README.md §1.1's `policy` entity, "Enforced allow,
// deny, approval, sandbox, trust, or managed constraint", is the closest
// ontology-defined match for that bucket), and plugin. Observe does not
// validate these against internal/ontology's registry: it only ever emits
// these six literal, hardcoded values, so there is nothing to validate
// against a loaded schema at runtime — matching internal/qualify/observe.go's
// ObserveSandbox, whose ObservationRule.Concept is likewise a plain string
// supplied by the caller, not ontology-checked.
const (
	conceptInstruction = "instruction"
	conceptSkill       = "skill"
	conceptMCPServer   = "mcp_server"
	conceptHook        = "hook"
	conceptPolicy      = "policy"
	conceptPlugin      = "plugin"
)

// knownConcepts is the closed set of concept IDs this package ever emits,
// used to validate caller-supplied SessionInput.Concept values (session.go)
// against a typo or an unimplemented concept, the same "closed vocabulary,
// fail loud on anything else" stance ValidateHostID/ValidateScopeKind take
// elsewhere in this module.
var knownConcepts = map[string]bool{
	conceptInstruction: true,
	conceptSkill:       true,
	conceptMCPServer:   true,
	conceptHook:        true,
	conceptPolicy:      true,
	conceptPlugin:      true,
}

// ruleKind distinguishes sourceRule's two shapes: a fixed list of candidate
// filenames checked directly under a root, or a subdirectory walked
// recursively for every (or every marker-named) regular file.
type ruleKind int

const (
	ruleCandidateFiles ruleKind = iota
	ruleWalkDir
)

// sourceRule names one physical source this package knows to look for under
// a root directory, and which concept it represents there. Exactly one of
// the two shapes applies, selected by kind:
//
//   - ruleCandidateFiles: check root/<name> for each name in files,
//     individually; emit one Observation per name that exists as a regular
//     file. Unlike Codex's documented "override else AGENTS.md" FIRST_MATCH
//     precedence (docs/ontology/README.md §6.2), this package emits a
//     record for every candidate name found, not just the one that would
//     win — precedence resolution is explicitly out of scope for this PR
//     (see doc.go); losslessly reporting every discovered candidate is what
//     lets a later precedence pass explain why one was excluded.
//   - ruleWalkDir: walk root/dir (dir == "" means the root itself)
//     recursively. If marker is non-empty, only a regular file whose base
//     name equals marker counts (skill packages, keyed on "SKILL.md" —
//     Skills are enumerated at arbitrary nesting depth because a skills
//     root legitimately holds many packages, docs/ontology/README.md §6.1/
//     §6.2 "nested skills"). If marker is empty, every regular file found
//     counts (Claude Code's rules/ directory of instruction files, whose
//     member filenames are not fixed by any spec).
type sourceRule struct {
	concept string
	kind    ruleKind

	files []string // ruleCandidateFiles

	dir    string // ruleWalkDir
	marker string // ruleWalkDir, optional

	// discoverOnly marks a rule whose matched files must never have their
	// content read, even though they are otherwise ordinary
	// ruleCandidateFiles entries — used for a source this package knows, from
	// its physical location and documented purpose alone, mixes permission/
	// trust state with credential material (e.g. Codex's $CODEX_HOME/auth.json
	// per docs/ontology/README.md §6.2's Policy/state row: "$CODEX_HOME
	// auth/logs/sessions"). Every matched candidate still produces a record
	// (existence is E0 evidence in its own right, per walk.go's "silently
	// dropping a source this package can prove is there would defeat the
	// lossless inventory goal"), but buildObservation is always forced down
	// its unreadable-content (E0) branch for these, regardless of whether the
	// file is actually readable by this process — this is the conservative
	// "don't guess safe" stance the PR-16 (issue #20) safety rules require
	// for a file this package cannot prove is credential-free.
	discoverOnly bool
}

// codexUserRules returns the source rules for one of Codex's native home
// locations (internal/context.NativeHome.Name), or nil if this package has
// nothing to look for under a native home by that name — a forward-
// compatible default if internal/context ever reports an additional native
// home this package does not yet know about, rather than an error.
func codexUserRules(nativeHomeName string) []sourceRule {
	switch nativeHomeName {
	case "CODEX_HOME":
		// docs/ontology/README.md §6.2: Instructions
		// "$CODEX_HOME/AGENTS.override.md else AGENTS.md"; MCP
		// "[mcp_servers.<id>] in user ... config" physically lives in
		// $CODEX_HOME/config.toml; Skills "$CODEX_HOME/skills" (the
		// undocumented-in-ontology-table root fixtures/README.md's static
		// binary inspection found — see fixtures/codex/0.144.5/skill-collision).
		//
		// PR-16 (issue #20) additions below config.toml's original mcp_server
		// rule: the same physical config.toml file is also documented as
		// where Codex's Hooks ("scoped hooks.json/hook config") and Policy/
		// state ("approval_policy, sandbox_mode, ... trusted projects")
		// concepts live — this package has no TOML parser (walk.go's
		// parseContent doc comment) and does not split one file's content by
		// concept, so it deliberately reports the SAME opaque file content
		// under all three concept tags rather than guessing which byte range
		// belongs to which concept: a real gap (tracked as a follow-up issue,
		// see doc.go), not a silent omission. auth.json is Policy/state's
		// "$CODEX_HOME auth" — a dedicated, standalone credential file (never
		// mixed with legitimate non-secret config the way .claude.json is),
		// so it is discoverOnly: existence is recorded at E0, content is
		// never read.
		return []sourceRule{
			{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"AGENTS.override.md", "AGENTS.md"}},
			{concept: conceptMCPServer, kind: ruleCandidateFiles, files: []string{"config.toml"}},
			{concept: conceptHook, kind: ruleCandidateFiles, files: []string{"config.toml"}},
			{concept: conceptPolicy, kind: ruleCandidateFiles, files: []string{"config.toml"}},
			{concept: conceptPolicy, kind: ruleCandidateFiles, files: []string{"auth.json"}, discoverOnly: true},
			{concept: conceptSkill, kind: ruleWalkDir, dir: "skills", marker: "SKILL.md"},
			// plugin: this package's own convention pick (walk the whole
			// CODEX_HOME for a "plugin.json" marker, mirroring how
			// $CODEX_HOME/skills itself was only found by static binary
			// inspection, not an ontology table entry) standing in for
			// docs/ontology/README.md §6.2's documented-but-root-less
			// ".codex-plugin/plugin.json packages" — NOT independently
			// confirmed against Codex's official docs or the installed
			// binary. See doc.go and coverage.go, which mark this cell
			// PARTIAL rather than EXACT for exactly this reason.
			{concept: conceptPlugin, kind: ruleWalkDir, dir: "", marker: "plugin.json"},
		}
	case "HOME/.agents/skills":
		// docs/architecture/runtime.md §7.1: "user Skills can also be
		// discovered from $HOME/.agents/skills" — this NativeHome's Path is
		// already the skills root itself (internal/context/host.go's
		// codexNativeHomes), so dir is "" (walk the root, not a
		// subdirectory of it).
		return []sourceRule{
			{concept: conceptSkill, kind: ruleWalkDir, dir: "", marker: "SKILL.md"},
		}
	default:
		return nil
	}
}

// codexWorkspaceRules returns the source rules checked directly under the
// worktree root for Codex's repository sources (docs/ontology/README.md
// §6.2: "trusted project .codex/config.toml"; "repo .agents/skills
// discovered from cwd/ancestors/root"). This function itself only checks the
// worktree root; the full root-to-cwd chain each intermediate directory adds
// is a separate, later Observe step (directory.go's codexDirectoryChainRules,
// added by PR-16/issue #20 — PR-08 originally deferred it, see doc.go).
func codexWorkspaceRules() []sourceRule {
	return []sourceRule{
		{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"AGENTS.override.md", "AGENTS.md"}},
		{concept: conceptMCPServer, kind: ruleCandidateFiles, files: []string{filepath.Join(".codex", "config.toml")}},
		// PR-16: same multiplexed-file rationale as codexUserRules above —
		// project .codex/config.toml is also where Codex's trusted-project
		// entry (Policy/state: "trusted projects") and any project-scoped
		// hook config would live.
		{concept: conceptHook, kind: ruleCandidateFiles, files: []string{filepath.Join(".codex", "config.toml")}},
		{concept: conceptPolicy, kind: ruleCandidateFiles, files: []string{filepath.Join(".codex", "config.toml")}},
		{concept: conceptSkill, kind: ruleWalkDir, dir: filepath.Join(".agents", "skills"), marker: "SKILL.md"},
	}
}

// codexDirectoryChainRules is what this package applies at every
// intermediate directory in the root-to-cwd chain (directory.go). Codex's
// documented "root to cwd" resolution behavior in
// docs/ontology/README.md §6.2 covers Instructions ("from project root to
// cwd, each directory uses override then AGENTS.md"), Settings/MCP
// ("trusted project .codex/config.toml from root to cwd"), and Skills
// ("repo .agents/skills discovered from cwd/ancestors/root") alike, so this
// is deliberately identical to codexWorkspaceRules rather than a narrower
// subset — kept as its own named function only so directory.go's call site
// documents which ontology claim it is exercising, independent of whether
// codexWorkspaceRules itself later changes shape.
func codexDirectoryChainRules() []sourceRule {
	return codexWorkspaceRules()
}

// claudeUserRules returns the source rules for one of Claude Code's native
// home locations, or nil if this package has nothing to look for there (see
// codexUserRules's doc comment for the forward-compatibility rationale).
func claudeUserRules(nativeHomeName string) []sourceRule {
	switch nativeHomeName {
	case "CLAUDE_CONFIG_DIR":
		// docs/ontology/README.md §6.1: Instructions "~/.claude/CLAUDE.md
		// and ~/.claude/rules/"; MCP "user/local state in ~/.claude.json"
		// — under CLAUDE_CONFIG_DIR relocation this file sits directly at
		// $CLAUDE_CONFIG_DIR/.claude.json, not nested under a further
		// .claude/ subdirectory (fixtures/README.md's static inspection
		// finding, knowledge/hosts/claude-code/cli/2.1/manifest.json's
		// knownUnknowns; matches fixtures/claude-code/2.1.211/mcp-merge's
		// claude-config/.claude.json layout); Skills
		// "~/.claude/skills/<id>/SKILL.md".
		//
		// PR-16 (issue #20) additions: settings.json/settings.local.json are
		// docs/ontology/README.md §6.1's "Hooks/plugins: hooks in scoped
		// settings; plugin manifests and enabled-plugin settings" and
		// "Policy/state: settings permissions/sandbox/trust" physical
		// sources — genuinely new files PR-08 never observed (it only
		// covered CLAUDE.md/rules/.claude.json/skills). .claude.json is also
		// re-tagged conceptPolicy here: the SAME file's already-parsed
		// content is documented as carrying "OAuth, project trust, cache" —
		// no new read, just a second concept-scoped record over content this
		// package already reads for MCP, relying on the same
		// internal/domain/redact boundary redact_test.go's
		// TestObserve_RedactionSafe_ParsedJSONEnvBlock already proves catches
		// a token-shaped key in this exact file.
		return []sourceRule{
			{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"CLAUDE.md"}},
			{concept: conceptInstruction, kind: ruleWalkDir, dir: "rules"},
			{concept: conceptMCPServer, kind: ruleCandidateFiles, files: []string{".claude.json"}},
			{concept: conceptPolicy, kind: ruleCandidateFiles, files: []string{".claude.json"}},
			{concept: conceptSkill, kind: ruleWalkDir, dir: "skills", marker: "SKILL.md"},
			{concept: conceptHook, kind: ruleCandidateFiles, files: []string{"settings.json", "settings.local.json"}},
			{concept: conceptPolicy, kind: ruleCandidateFiles, files: []string{"settings.json", "settings.local.json"}},
			{concept: conceptPlugin, kind: ruleCandidateFiles, files: []string{"settings.json", "settings.local.json"}},
		}
	case "HOME/.agents/skills":
		// Shared cross-host root: this project's own task scope for PR-08
		// names $HOME/.agents/skills as one of Claude Code's sources too,
		// alongside internal/context/host.go's claudeNativeHomes reporting
		// it as a NativeHome for this host.
		return []sourceRule{
			{concept: conceptSkill, kind: ruleWalkDir, dir: "", marker: "SKILL.md"},
		}
	default:
		return nil
	}
}

// claudeWorkspaceRules returns the source rules checked directly under the
// worktree root for Claude Code's repository sources (docs/ontology/README.md
// §6.1: "project/ancestor CLAUDE.md or .claude/CLAUDE.md; .claude/rules/";
// MCP "project .mcp.json"; Skills ".claude/skills/<id>/SKILL.md"; Hooks/
// plugins and Policy/state's project-scoped settings.json/settings.local.json,
// added by PR-16/issue #20). This function itself only checks the worktree
// root; the ancestor/nested Instructions chain is a separate, later Observe
// step (directory.go's claudeDirectoryChainRules) — settings.json/.mcp.json/
// skills are NOT documented as resolving through the ancestor chain the way
// CLAUDE.md is, so they stay root-only here even after PR-16.
func claudeWorkspaceRules() []sourceRule {
	return []sourceRule{
		{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md")}},
		{concept: conceptInstruction, kind: ruleWalkDir, dir: filepath.Join(".claude", "rules")},
		{concept: conceptMCPServer, kind: ruleCandidateFiles, files: []string{".mcp.json"}},
		{concept: conceptSkill, kind: ruleWalkDir, dir: filepath.Join(".claude", "skills"), marker: "SKILL.md"},
		{concept: conceptHook, kind: ruleCandidateFiles, files: []string{filepath.Join(".claude", "settings.json"), filepath.Join(".claude", "settings.local.json")}},
		{concept: conceptPolicy, kind: ruleCandidateFiles, files: []string{filepath.Join(".claude", "settings.json"), filepath.Join(".claude", "settings.local.json")}},
		{concept: conceptPlugin, kind: ruleCandidateFiles, files: []string{filepath.Join(".claude", "settings.json"), filepath.Join(".claude", "settings.local.json")}},
	}
}

// claudeDirectoryChainRules is what this package applies at every
// intermediate directory in the root-to-cwd chain (directory.go): only
// Instructions, per docs/ontology/README.md §6.1's "project/ancestor
// CLAUDE.md or .claude/CLAUDE.md" wording — unlike Codex, Claude Code's MCP
// (.mcp.json) and Skills (.claude/skills) sources are documented only at the
// project root, never as resolving through an ancestor chain, so this is
// deliberately a narrower subset of claudeWorkspaceRules, not an identical
// copy the way codexDirectoryChainRules is.
func claudeDirectoryChainRules() []sourceRule {
	return []sourceRule{
		{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md")}},
		{concept: conceptInstruction, kind: ruleWalkDir, dir: filepath.Join(".claude", "rules")},
	}
}

// claudeLocalRules returns the source rule for Claude Code's `local` scope
// (docs/ontology/README.md §2: "One user in one workspace: Gitignored
// project-local config"): CLAUDE.local.md at the worktree root. PR-08
// deliberately excluded this (its own doc comment named `local` as a third
// scope outside PR-08's "user-global and repository sources" mandate);
// PR-16/issue #20's acceptance criteria explicitly add `local` to this
// package's required scope coverage. Codex has no ontology-documented
// `local`-scope source (docs/ontology/README.md §6.2 names no Codex source
// with scope `local`), so there is no codexLocalRules counterpart — a
// deliberate, documented asymmetry, not an oversight.
func claudeLocalRules() []sourceRule {
	return []sourceRule{
		{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"CLAUDE.local.md"}},
	}
}
