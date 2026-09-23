package qualify

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// This file is issue #127's executable Knowledge Pack fixture evidence for
// promoting capabilities.instruction.resolve from UNKNOWN to EXACT on
// codex, claude-code, and pi (knowledge/hosts/{codex,claude-code,pi}/*/
// manifest.json). Each discover*InstructionChain function below is a
// reference simulation of one host's own documented instruction-file
// discovery algorithm -- written to prove the specific, falsifiable claims
// issue #127's fact table makes (sourced there from codex-rs/core/src/
// agents_md.rs, code.claude.com/docs/en/memory, and infra2-harness's
// handover.context-and-gates.md 2026-09-21 measurement) against a real,
// isolated temporary directory tree. None of this touches the real HOME or
// any host binary -- every path below is rooted at a t.TempDir().
//
// These are deliberately NOT internal/observe's production adapter code
// (internal/observe/rules.go, directory.go): that package's own scope is
// OMCA's inventory adapter and carries its own, separately tracked gaps
// (e.g. its directoryChain is bounded at one shared WorktreeRoot for every
// host alike, so it does not yet encode Claude Code's or pi's documented
// unbounded ancestor walk). This issue is scoped to the Knowledge Pack
// claim -- what the vendor CLI itself does -- not to reconciling OMCA's own
// adapter against it; that reconciliation, if needed, is separate future
// work.

// firstExistingFile returns the path of the first name in names that
// exists as a regular file directly under dir, and false if none do.
func firstExistingFile(dir string, names ...string) (string, bool) {
	for _, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path, true
		}
	}
	return "", false
}

// fileExists reports whether path exists as a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// sameFile reports whether a and b resolve to the same underlying file
// (e.g. one is a symlink to the other, or both are symlinks to a common
// target) using os.Stat (which follows symlinks) plus os.SameFile, rather
// than a path-string comparison.
func sameFile(a, b string) (bool, error) {
	infoA, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(infoA, infoB), nil
}

// findProjectRoot walks upward from cwd, inclusive, looking for the nearest
// ancestor directory containing marker (a name checked with os.Stat, e.g.
// ".git" -- Codex's project_root_markers default). It returns found=false
// if the filesystem root is reached without a hit.
func findProjectRoot(cwd, marker string) (root string, found bool) {
	dir := cwd
	for {
		if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// chainRootToLeaf lists every directory from root down to leaf, inclusive
// of both ends, in root-to-leaf order. leaf must be root itself or a
// descendant of it.
func chainRootToLeaf(root, leaf string) []string {
	rel, err := filepath.Rel(root, leaf)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	if rel == "." {
		return []string{root}
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	chain := []string{root}
	cur := root
	for _, seg := range segments {
		cur = filepath.Join(cur, seg)
		chain = append(chain, cur)
	}
	return chain
}

// chainLeafToStop lists every directory from cwd upward through and
// including stopAbove, in root-to-leaf order (stopAbove first, cwd last).
// stopAbove must be cwd itself or an ancestor of it. Unlike
// findProjectRoot/chainRootToLeaf, this never looks for a ".git" (or any
// other project) marker -- it is the shape of an unbounded upward walk,
// stopped only by the caller-supplied boundary.
func chainLeafToStop(cwd, stopAbove string) []string {
	var reversed []string
	dir := cwd
	for {
		reversed = append(reversed, dir)
		if dir == stopAbove {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	chain := make([]string, len(reversed))
	for i, d := range reversed {
		chain[len(reversed)-1-i] = d
	}
	return chain
}

// discoverCodexInstructionChain models Codex CLI's documented instruction
// discovery (issue #127's fact table, sourced from codex-rs/core/src/
// agents_md.rs): the project root is the nearest ancestor of cwd containing
// a project_root_markers hit (".git" by default); AGENTS.override.md then
// AGENTS.md then fallback names are tried per directory, first hit wins,
// walking from that root DOWN to cwd inclusive -- the chain never extends
// above the project root, whether or not one was found. $CODEX_HOME/
// AGENTS.md, when present, is returned first (loads before every
// project-scoped entry).
func discoverCodexInstructionChain(codexHome, cwd string) []string {
	var out []string
	if codexHome != "" {
		if p, ok := firstExistingFile(codexHome, "AGENTS.md"); ok {
			out = append(out, p)
		}
	}

	root, found := findProjectRoot(cwd, ".git")
	if !found {
		root = cwd
	}
	for _, dir := range chainRootToLeaf(root, cwd) {
		if p, ok := firstExistingFile(dir, "AGENTS.override.md", "AGENTS.md"); ok {
			out = append(out, p)
		}
	}
	return out
}

// discoverClaudeInstructionChain models Claude Code's documented
// instruction discovery (issue #127's fact table, sourced from
// https://code.claude.com/docs/en/memory): CLAUDE.md and CLAUDE.local.md
// are collected from cwd and every parent directory, unbounded -- never
// stopped by a ".git" project-root marker the way Codex's chain is -- with
// CLAUDE.local.md appended immediately after CLAUDE.md for each directory
// that has one. stopAbove is a caller-supplied outer boundary standing in
// for wherever the real host's walk actually terminates (filesystem root or
// $HOME -- the cited source names neither exactly); this fixture proves the
// one claim the fact table actually makes -- this chain is not bounded by a
// project root -- without inventing an unfounded stop condition.
func discoverClaudeInstructionChain(cwd, stopAbove string) []string {
	var out []string
	for _, dir := range chainLeafToStop(cwd, stopAbove) {
		if p := filepath.Join(dir, "CLAUDE.md"); fileExists(p) {
			out = append(out, p)
		}
		if p := filepath.Join(dir, "CLAUDE.local.md"); fileExists(p) {
			out = append(out, p)
		}
	}
	return out
}

// htmlCommentPattern matches an HTML comment, including one spanning
// multiple lines.
var htmlCommentPattern = regexp.MustCompile(`(?s)<!--.*?-->`)

// stripHTMLComments models Claude Code's documented memory-file processing
// (issue #127's fact table, same source as discoverClaudeInstructionChain):
// HTML comments are stripped from instruction content before it reaches the
// model.
func stripHTMLComments(content string) string {
	return htmlCommentPattern.ReplaceAllString(content, "")
}

// discoverPiInstructionChain models pi's documented instruction discovery
// (issue #127's fact table, sourced from infra2-harness's
// handover.context-and-gates.md 2026-09-21 measurement): AGENTS.md and
// CLAUDE.md are collected from cwd and every parent directory, unbounded --
// the same unbounded shape as Claude Code, never stopped by a ".git"
// project-root marker. When both names exist in one directory and resolve
// to the same underlying file (a symlink pair -- one a symlink to the
// other, or both symlinks to a shared target), that directory contributes
// it once, not twice; two genuinely distinct files both count.
func discoverPiInstructionChain(cwd, stopAbove string) ([]string, error) {
	var out []string
	for _, dir := range chainLeafToStop(cwd, stopAbove) {
		agents := filepath.Join(dir, "AGENTS.md")
		claude := filepath.Join(dir, "CLAUDE.md")
		agentsExists := fileExists(agents)
		claudeExists := fileExists(claude)
		switch {
		case agentsExists && claudeExists:
			same, err := sameFile(agents, claude)
			if err != nil {
				return nil, err
			}
			out = append(out, agents)
			if !same {
				out = append(out, claude)
			}
		case agentsExists:
			out = append(out, agents)
		case claudeExists:
			out = append(out, claude)
		}
	}
	return out, nil
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := writeFile(path, content, 0o644); err != nil {
		t.Fatalf("writeFile(%s): %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}

// TestInstructionDiscovery_Codex_BoundedAtProjectRootHomeFirst proves the
// codex row of issue #127's fact table: $CODEX_HOME/AGENTS.md loads first;
// the chain then runs root-to-cwd from the nearest ".git" ancestor;
// AGENTS.override.md shadows AGENTS.md in the same directory; and a file
// placed ABOVE the project root is never discovered.
func TestInstructionDiscovery_Codex_BoundedAtProjectRootHomeFirst(t *testing.T) {
	tmp := t.TempDir()

	codexHome := filepath.Join(tmp, "codex-home")
	mustWrite(t, filepath.Join(codexHome, "AGENTS.md"), "home instructions, must load first")

	outsideAgents := filepath.Join(tmp, "outside", "AGENTS.md")
	mustWrite(t, outsideAgents, "must never be discovered: above the project root")

	projectRoot := filepath.Join(tmp, "outside", "project")
	mustMkdirAll(t, filepath.Join(projectRoot, ".git"))
	rootAgents := filepath.Join(projectRoot, "AGENTS.md")
	mustWrite(t, rootAgents, "root instructions")

	subDir := filepath.Join(projectRoot, "sub")
	subOverride := filepath.Join(subDir, "AGENTS.override.md")
	subAgents := filepath.Join(subDir, "AGENTS.md")
	mustWrite(t, subOverride, "override wins over AGENTS.md in the same directory")
	mustWrite(t, subAgents, "must be shadowed by AGENTS.override.md in this same directory")

	cwd := filepath.Join(subDir, "cwd")
	cwdAgents := filepath.Join(cwd, "AGENTS.md")
	mustWrite(t, cwdAgents, "cwd instructions")

	got := discoverCodexInstructionChain(codexHome, cwd)
	want := []string{
		filepath.Join(codexHome, "AGENTS.md"),
		rootAgents,
		subOverride,
		cwdAgents,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverCodexInstructionChain = %v, want %v", got, want)
	}
	for _, p := range got {
		if p == outsideAgents {
			t.Fatalf("chain crossed the project root and included %s -- codex must never walk past the project root", outsideAgents)
		}
		if p == subAgents {
			t.Fatalf("chain included %s -- AGENTS.md must be shadowed by AGENTS.override.md in the same directory", subAgents)
		}
	}
}

// TestInstructionDiscovery_ClaudeCode_UnboundedAncestorChain proves the
// claude-code row of issue #127's fact table: a CLAUDE.md placed ABOVE a
// ".git" boundary is still discovered (unbounded, unlike codex), and
// CLAUDE.local.md is appended immediately after CLAUDE.md for the
// directory that has one.
func TestInstructionDiscovery_ClaudeCode_UnboundedAncestorChain(t *testing.T) {
	tmp := t.TempDir()

	topDir := filepath.Join(tmp, "top") // stands in for $HOME or the filesystem root
	topClaude := filepath.Join(topDir, "CLAUDE.md")
	mustWrite(t, topClaude, "top-level instructions, ABOVE any project root")

	gitDir := filepath.Join(topDir, "repo")
	mustMkdirAll(t, filepath.Join(gitDir, ".git"))
	repoClaude := filepath.Join(gitDir, "CLAUDE.md")
	repoLocal := filepath.Join(gitDir, "CLAUDE.local.md")
	mustWrite(t, repoClaude, "repo instructions")
	mustWrite(t, repoLocal, "repo-local, gitignored instructions")

	innerDir := filepath.Join(gitDir, "inner")
	innerClaude := filepath.Join(innerDir, "CLAUDE.md")
	mustWrite(t, innerClaude, "inner instructions")

	got := discoverClaudeInstructionChain(innerDir, topDir)
	want := []string{topClaude, repoClaude, repoLocal, innerClaude}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverClaudeInstructionChain = %v, want %v (a .git boundary at %s must not stop the walk)", got, want, gitDir)
	}
}

// TestInstructionDiscovery_ClaudeCode_StripsHTMLComments proves the
// claude-code fact table's "HTML comments stripped" clause with a real
// positive+negative assertion: comment content is gone, surrounding content
// survives.
func TestInstructionDiscovery_ClaudeCode_StripsHTMLComments(t *testing.T) {
	raw := "Visible before.\n<!-- secret: do not surface this -->\nVisible after.\n"
	got := stripHTMLComments(raw)
	if strings.Contains(got, "secret") {
		t.Fatalf("stripHTMLComments left an HTML comment in the result: %q", got)
	}
	if !strings.Contains(got, "Visible before.") || !strings.Contains(got, "Visible after.") {
		t.Fatalf("stripHTMLComments removed non-comment content: %q", got)
	}
}

// TestInstructionDiscovery_Pi_UnboundedAncestorChainWithSymlinkDedup proves
// the pi row of issue #127's fact table: the walk is unbounded upward (like
// Claude Code, unlike codex), and a same-directory AGENTS.md/CLAUDE.md
// symlink pair is only counted once.
func TestInstructionDiscovery_Pi_UnboundedAncestorChainWithSymlinkDedup(t *testing.T) {
	tmp := t.TempDir()

	topDir := filepath.Join(tmp, "top")
	topAgents := filepath.Join(topDir, "AGENTS.md")
	mustWrite(t, topAgents, "top-level instructions, ABOVE any project root")

	gitDir := filepath.Join(topDir, "repo")
	mustMkdirAll(t, filepath.Join(gitDir, ".git"))
	midAgents := filepath.Join(gitDir, "AGENTS.md")
	midClaude := filepath.Join(gitDir, "CLAUDE.md")
	mustWrite(t, midAgents, "shared content reachable through a symlink pair")
	if err := os.Symlink(midAgents, midClaude); err != nil {
		t.Fatalf("os.Symlink(%s, %s): %v", midAgents, midClaude, err)
	}

	innerDir := filepath.Join(gitDir, "inner")
	innerClaude := filepath.Join(innerDir, "CLAUDE.md")
	mustWrite(t, innerClaude, "inner instructions, a distinct file, not a symlink pair")

	got, err := discoverPiInstructionChain(innerDir, topDir)
	if err != nil {
		t.Fatalf("discoverPiInstructionChain: %v", err)
	}
	want := []string{topAgents, midAgents, innerClaude}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverPiInstructionChain = %v, want %v (a .git boundary at %s must not stop the walk, and the symlink pair at %s must count once)", got, want, gitDir, gitDir)
	}
}

// TestInstructionDiscovery_Pi_DistinctAgentsAndClaudeBothCount is the
// negative twin of the symlink-dedup test above: it proves the dedup only
// collapses a genuine symlink pair, not every same-directory AGENTS.md +
// CLAUDE.md pair -- two independent, non-symlinked files must both be
// reported.
func TestInstructionDiscovery_Pi_DistinctAgentsAndClaudeBothCount(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "project")
	agents := filepath.Join(dir, "AGENTS.md")
	claude := filepath.Join(dir, "CLAUDE.md")
	mustWrite(t, agents, "agents content")
	mustWrite(t, claude, "distinct claude content, not a symlink")

	got, err := discoverPiInstructionChain(dir, dir)
	if err != nil {
		t.Fatalf("discoverPiInstructionChain: %v", err)
	}
	want := []string{agents, claude}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverPiInstructionChain = %v, want %v (two distinct files must both be counted)", got, want)
	}
}
