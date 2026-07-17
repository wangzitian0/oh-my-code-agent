package observe

import "path/filepath"

// Concept IDs this package tags every Observation with, matching the three
// concept declarations this PR's scope covers (ontology/concepts/{instruction,
// skill,mcp_server}.json's conceptId). Observe does not validate these
// against internal/ontology's registry: it only ever emits these three
// literal, hardcoded values, so there is nothing to validate against a
// loaded schema at runtime — matching internal/qualify/observe.go's
// ObserveSandbox, whose ObservationRule.Concept is likewise a plain string
// supplied by the caller, not ontology-checked.
const (
	conceptInstruction = "instruction"
	conceptSkill       = "skill"
	conceptMCPServer   = "mcp_server"
)

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
		return []sourceRule{
			{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"AGENTS.override.md", "AGENTS.md"}},
			{concept: conceptMCPServer, kind: ruleCandidateFiles, files: []string{"config.toml"}},
			{concept: conceptSkill, kind: ruleWalkDir, dir: "skills", marker: "SKILL.md"},
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
// discovered from cwd/ancestors/root"). This package intentionally checks
// only the worktree root itself, not the full root-to-cwd chain each
// intermediate directory would add — that per-directory "directory" scope
// walk is explicitly out of scope for this PR (see doc.go and this
// package's final report for the documented reasoning).
func codexWorkspaceRules() []sourceRule {
	return []sourceRule{
		{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"AGENTS.override.md", "AGENTS.md"}},
		{concept: conceptMCPServer, kind: ruleCandidateFiles, files: []string{filepath.Join(".codex", "config.toml")}},
		{concept: conceptSkill, kind: ruleWalkDir, dir: filepath.Join(".agents", "skills"), marker: "SKILL.md"},
	}
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
		return []sourceRule{
			{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"CLAUDE.md"}},
			{concept: conceptInstruction, kind: ruleWalkDir, dir: "rules"},
			{concept: conceptMCPServer, kind: ruleCandidateFiles, files: []string{".claude.json"}},
			{concept: conceptSkill, kind: ruleWalkDir, dir: "skills", marker: "SKILL.md"},
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
// MCP "project .mcp.json"; Skills ".claude/skills/<id>/SKILL.md"). As with
// codexWorkspaceRules, only the worktree root itself is checked — not the
// ancestor/nested chain — per this PR's documented scope cut.
//
// CLAUDE.local.md (docs/ontology/README.md §6.1) is deliberately excluded:
// its scope is `local` ("One user in one workspace: Gitignored project-local
// config", docs/ontology/README.md §2), a third scope this PR's "user-global
// and repository sources" mandate does not name — see doc.go.
func claudeWorkspaceRules() []sourceRule {
	return []sourceRule{
		{concept: conceptInstruction, kind: ruleCandidateFiles, files: []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md")}},
		{concept: conceptInstruction, kind: ruleWalkDir, dir: filepath.Join(".claude", "rules")},
		{concept: conceptMCPServer, kind: ruleCandidateFiles, files: []string{".mcp.json"}},
		{concept: conceptSkill, kind: ruleWalkDir, dir: filepath.Join(".claude", "skills"), marker: "SKILL.md"},
	}
}
