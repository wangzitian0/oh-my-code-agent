package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// generatedFile is one file this package places inside a generation's
// per-host artifact tree, path relative to the generation directory root
// (e.g. "hosts/codex/cli/codex-home/config.toml").
type generatedFile struct {
	RelPath string
	Content []byte
}

// NativeHomeDirName is the directory name this package uses, inside a
// generation's per-host tree, for the directory a launch shim points the
// host's own native-home environment variable at
// (docs/architecture/runtime.md §7.1's "CODEX_HOME=<generation>/codex-home",
// §7.2's "a relocated configuration directory per generation"). This is
// literally what issue #13 AC #1 means by "the generated CODEX_HOME":
// <generation>/hosts/codex/<surface>/codex-home. Named identically to
// internal/qualify.Sandbox's own directory-naming convention ("codex-home",
// "claude-config") for consistency across the codebase; not reused directly
// (Sandbox is a fixture-harness type this production compiler should not
// depend on).
//
// Exported (PR-09 had this unexported) because PR-10's non-recursive PATH
// shim (internal/shim) and `omca run --mode isolated` both need to compute
// the exact same path a compiled generation's manifest.json already implies
// -- <generationDir>/hosts/<host>/<surface>/<NativeHomeDirName> -- and
// re-declaring this two-entry switch a second time elsewhere would be
// exactly the kind of driftable duplication this package's own AdapterID
// doc comment warns against.
func NativeHomeDirName(host string) (string, error) {
	switch host {
	case "codex":
		return "codex-home", nil
	case "claude-code":
		return "claude-config", nil
	default:
		return "", fmt.Errorf("runtime: NativeHomeDirName: unsupported host %q (only codex, claude-code)", host)
	}
}

// NativeHomeEnvVar returns the environment variable name a launch shim must
// set to point host's native config/state resolution at a generation's
// NativeHomeDir: "CODEX_HOME" for codex, "CLAUDE_CONFIG_DIR" for
// claude-code. These are the exact same variable names
// internal/context/host.go's codexNativeHomes/claudeNativeHomes read to
// compute HostDetection.NativeHomes in the first place
// (docs/architecture/runtime.md §7.1/§7.2) -- this function is the
// generation-compiler package's own record of that correspondence, kept
// here (next to NativeHomeDirName, the other half of "where a shim points
// this variable") rather than in internal/context, since internal/context
// only ever reads these variables to observe the real installation and has
// no reason to know what a shim would set them to.
func NativeHomeEnvVar(host string) (string, error) {
	switch host {
	case "codex":
		return "CODEX_HOME", nil
	case "claude-code":
		return "CLAUDE_CONFIG_DIR", nil
	default:
		return "", fmt.Errorf("runtime: NativeHomeEnvVar: unsupported host %q (only codex, claude-code)", host)
	}
}

// classify applies the fixed M1 bootstrap policy (docs/project/roadmap.md
// M1: "no implicit user-global Skill, MCP, Hook, Plugin, or Instruction") to
// one Observation, returning whether it belongs in the generated tree and
// why. This is the literal decision issue #13 AC #1/#2 tests: every
// scope.kind=="user" observation is excluded, unconditionally, regardless
// of concept -- Instructions included.
func classify(o domain.Observation) (included bool, reason string) {
	switch o.Spec.Scope.Kind {
	case "user":
		return false, "excluded: native user-global source; the M1 bootstrap policy never inherits user-global config (docs/architecture/runtime.md §3, docs/project/roadmap.md M1)"
	case "workspace":
		if o.Spec.Concept == "instruction" {
			return true, "included: repository-scope Instructions chain, project-loadable per the M1 bootstrap policy (docs/project/roadmap.md M1)"
		}
		return false, "excluded: repository-scope Skill/MCP source, not yet activated by any desired state (no Profile exists before PR-12)"
	default:
		return false, fmt.Sprintf("excluded: scope %q is outside the M1 bootstrap policy's understood user/workspace scopes", o.Spec.Scope.Kind)
	}
}

// instructionContent returns the raw text this package copies into the
// generated tree for a repository-scope Instructions observation, and
// whether content was actually available. internal/observe always retains
// Instructions content as raw text (walk.go's parseContent: only
// ".json"-suffixed sources are structurally parsed), so an E1
// (EvidenceLevelParsed) instruction observation's OpaqueVendorFields always
// holds a string under "content" -- but an E0 (EvidenceLevelDiscovered,
// unreadable source) observation has no "content" key at all
// (discoveredOnlyPlaceholder only carries "discovered"/"readable"), and
// this function correctly reports ok=false for that case rather than
// panicking on a missing or mistyped key.
func instructionContent(o domain.Observation) (content []byte, ok bool) {
	raw, present := o.Spec.OpaqueVendorFields["content"]
	if !present {
		return nil, false
	}
	s, isString := raw.(string)
	if !isString {
		return nil, false
	}
	return []byte(s), true
}

// mcpServerID is the entry name this package registers the OMCA MCP server
// under inside a generation's own config -- "omca", matching the binary a
// human would recognize, distinct from any project- or user-registered
// server ID a real config might otherwise carry (there is none in a
// bootstrap generation's own generated tree, but the name still matters for
// a human reading the generated file).
const mcpServerID = "omca"

// mcpServerArgs is the fixed argv `omca mcp serve` (cmd/omca/mcp.go) this
// package always registers when it emits an MCP registration at all --
// there is exactly one subcommand this PR wires up (issue #15's own "ONLY
// omca_status; omca_query/omca_propose/omca_stage are explicitly out of
// scope").
var mcpServerArgs = []string{"mcp", "serve"}

// tomlString quote-escapes s for a TOML basic string literal: backslash and
// double-quote are TOML's own two escape-worthy characters inside a basic
// (double-quoted) string. This package's other hardcoded TOML values never
// contain either, so this is the one place a caller-supplied value (a real
// filesystem path, which on a sane filesystem should never contain a
// double-quote but could in principle) needs actual escaping rather than
// bare interpolation.
func tomlString(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(s) + `"`
}

// codexMCPRegistrationTOML returns the `[mcp_servers.omca]` TOML table this
// package appends to a codex generation's config.toml when omcaBinaryPath
// is non-empty (docs/architecture/runtime.md §3: "the bootstrap generation
// contains... the OMCA MCP server"). command is Bootstrap's caller-supplied,
// absolute OMCA binary path (BootstrapRequest.OMCABinaryPath); args are the
// fixed mcpServerArgs.
func codexMCPRegistrationTOML(omcaBinaryPath string) string {
	return fmt.Sprintf("\n[mcp_servers.%s]\ncommand = %s\nargs = [\"mcp\", \"serve\"]\n", mcpServerID, tomlString(omcaBinaryPath))
}

// claudeMCPServerEntry is one entry of a Claude-Code-shaped `mcpServers`
// JSON object (fixtures/claude-code/2.1.211/mcp-merge/input's own
// `{"command":..., "args":[...]}` shape).
type claudeMCPServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// claudeMCPRegistrationJSON returns the generated `.claude.json` content
// this package writes into a claude-code generation's nativeHomeDir when
// omcaBinaryPath is non-empty: a single `mcpServers.omca` entry, matching
// the real user-scope file internal/observe/rules.go's claudeUserRules
// already looks for at $CLAUDE_CONFIG_DIR/.claude.json (fixtures/README.md's
// static-inspection finding: this file sits directly under CLAUDE_CONFIG_DIR,
// not nested under a further .claude/ subdirectory). Unlike a real user's
// ~/.claude.json, this generated file carries nothing but the one MCP
// registration -- no trust state, no OAuth/account data, nothing copied
// from any real native source (docs/architecture/runtime.md §8:
// "Authentication state is not normal configuration and is never imported
// from an untrusted native home as part of runtime compilation").
func claudeMCPRegistrationJSON(omcaBinaryPath string) ([]byte, error) {
	doc := map[string]map[string]claudeMCPServerEntry{
		"mcpServers": {
			mcpServerID: {Command: omcaBinaryPath, Args: append([]string{}, mcpServerArgs...)},
		},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("runtime: claudeMCPRegistrationJSON: %w", err)
	}
	return append(data, '\n'), nil
}

// hostConfigFiles returns every OMCA-authored config file this package
// always emits inside nativeHomeDir: conservative default permissions
// (docs/project/roadmap.md M1), plus -- when omcaBinaryPath is non-empty --
// the OMCA MCP server registration (docs/architecture/runtime.md §3: "the
// bootstrap generation contains... the OMCA MCP server"; FR-7). This is the
// scope cut doc.go's "What is deliberately NOT in the generated tree yet"
// section flagged as open ("internal/mcp... is still an empty doc.go stub
// -- there is no real command or protocol handler this compiler could point
// a generated config entry at yet"): issue #15 built internal/mcp and
// `omca mcp serve`, so this function now closes that gap whenever a caller
// supplies a real omcaBinaryPath.
//
// Permission defaults and the MCP registration are emitted together, not as
// two separately-tracked files, because for both hosts today they land in
// the exact same physical file (codex's single config.toml; claude-code's
// case is the one exception -- see below) and a generatedFile slice cannot
// contain two entries with the same RelPath without one silently
// overwriting the other on disk.
//
// Claude Code's MCP registration is the one case that does NOT share a file
// with the permission defaults: internal/observe/rules.go's claudeUserRules
// already establishes that Claude Code's MCP registry lives in
// $CLAUDE_CONFIG_DIR/.claude.json, a different file from settings.json
// (where defaultMode lives) -- writing them as two separate generated files
// mirrors that real, already-established physical split rather than
// inventing a combined file format Claude Code itself does not use.
//
// Returned RelPaths are relative to nativeHomeDir itself; compileHostTree
// joins them under the full per-host prefix, exactly like the single-file
// version this replaces.
func hostConfigFiles(host, nativeHomeDir, omcaBinaryPath string) ([]generatedFile, error) {
	switch host {
	case "codex":
		content := "" +
			"# OMCA bootstrap generation: conservative permission defaults (docs/project/roadmap.md M1).\n" +
			"# This is a hardcoded M1 policy value, not a host-verified capability\n" +
			"# resolution -- see knowledge/hosts/codex/cli/0.144/manifest.json.\n" +
			"approval_policy = \"untrusted\"\n" +
			"sandbox_mode = \"read-only\"\n"
		if omcaBinaryPath != "" {
			content += codexMCPRegistrationTOML(omcaBinaryPath)
		}
		return []generatedFile{{RelPath: filepath.Join(nativeHomeDir, "config.toml"), Content: []byte(content)}}, nil
	case "claude-code":
		content := `{"permissions":{"defaultMode":"plan"}}` + "\n"
		files := []generatedFile{{RelPath: filepath.Join(nativeHomeDir, "settings.json"), Content: []byte(content)}}
		if omcaBinaryPath != "" {
			claudeJSON, err := claudeMCPRegistrationJSON(omcaBinaryPath)
			if err != nil {
				return nil, err
			}
			files = append(files, generatedFile{RelPath: filepath.Join(nativeHomeDir, ".claude.json"), Content: claudeJSON})
		}
		return files, nil
	default:
		return nil, fmt.Errorf("runtime: hostConfigFiles: unsupported host %q", host)
	}
}

// claudeConfigDirExclusionGapSources returns the two capability-gap
// manifest entries this package always attaches to a claude-code
// generation's sources list (issue #13's round-2 anti-drift rule: "If any
// exclusion class ships as a capability gap ... the generation manifest
// links that issue"). This is Claude-Code-specific, not Codex-specific:
// Codex's isolation mechanism (CODEX_HOME/HOME redirection,
// docs/architecture/runtime.md §7.1) is a structural filesystem/env
// boundary this compiler itself fully controls by construction -- a freshly
// built generation directory cannot contain content this compiler never
// wrote into it, so there is nothing left to "prove" about Codex's own
// native-config leakage the way there is for Claude Code, whose
// CLAUDE_CONFIG_DIR relocation is the host *binary's own*, only
// statically-inspected (never behavior-probed) mechanism -- see
// knowledge/hosts/claude-code/cli/2.1/manifest.json's knownUnknowns and
// fixtures/README.md's evidentiary trail. See doc.go and policy.go's
// ClaudeConfigDirExclusionGapIssueURL doc comment for the tracking issue
// this links.
func claudeConfigDirExclusionGapSources() []domain.GenerationSourceEntry {
	const reasonTemplate = "capability gap: whether CLAUDE_CONFIG_DIR relocation completely excludes every native user-global %s was only established by static, read-only binary inspection (E1 evidence), never behaviorally confirmed by an actual launch (see knowledge/hosts/claude-code/cli/2.1/manifest.json's knownUnknowns and fixtures/README.md); reported explicitly rather than silently assumed clean, per issue #13's policy: capability-gap shipping is allowed, hiding is not"
	return []domain.GenerationSourceEntry{
		{
			Concept:       "mcp_server",
			Scope:         "user",
			Included:      false,
			Reason:        fmt.Sprintf(reasonTemplate, "MCP server registration"),
			CapabilityGap: true,
			TrackingIssue: ClaudeConfigDirExclusionGapIssueURL,
		},
		{
			Concept:       "skill",
			Scope:         "user",
			Included:      false,
			Reason:        fmt.Sprintf(reasonTemplate, "Skill"),
			CapabilityGap: true,
			TrackingIssue: ClaudeConfigDirExclusionGapIssueURL,
		},
	}
}

// compileHostTree applies the fixed M1 bootstrap policy to req, returning
// every file to place in the generation's per-host artifact tree (path
// relative to the generation directory root) and the full sources list
// manifest.json.spec.sources records (issue #13 AC "The manifest lists
// every included and excluded source with a reason"). req must already be
// req.validate()-clean; callers (Bootstrap, GenerationID) are responsible
// for that.
func compileHostTree(req BootstrapRequest) ([]generatedFile, []domain.GenerationSourceEntry, error) {
	host := req.Detection.Host
	surface := req.surface()
	nativeHomeDir, err := NativeHomeDirName(host)
	if err != nil {
		return nil, nil, err
	}
	hostPrefix := filepath.Join("hosts", host, surface)

	var files []generatedFile
	var sources []domain.GenerationSourceEntry

	for _, o := range req.Observations {
		included, reason := classify(o)
		entry := domain.GenerationSourceEntry{
			Concept:  o.Spec.Concept,
			Source:   o.Spec.Source.Path,
			Scope:    o.Spec.Scope.Kind,
			Included: included,
			Reason:   reason,
		}
		if included {
			content, ok := instructionContent(o)
			if !ok {
				// A repository Instructions source classify() decided to
				// include, but internal/observe could not actually read
				// (E0). There is no content to place in the artifact tree,
				// so this downgrades to an explained exclusion rather than
				// silently omitting the file with no trace in the manifest.
				entry.Included = false
				entry.Reason = "excluded: repository Instructions source was discovered but its content could not be read (E0); a generation artifact cannot be produced without content"
			} else {
				rel, relErr := filepath.Rel(req.Worktree.Root, o.Spec.Source.Path)
				if relErr != nil {
					return nil, nil, fmt.Errorf("runtime: compileHostTree: %w", relErr)
				}
				if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return nil, nil, fmt.Errorf("runtime: compileHostTree: repository Instructions source %s resolves outside the worktree root %s", o.Spec.Source.Path, req.Worktree.Root)
				}
				files = append(files, generatedFile{
					RelPath: filepath.Join(hostPrefix, "instructions", rel),
					Content: content,
				})
			}
		}
		sources = append(sources, entry)
	}

	configFiles, err := hostConfigFiles(host, nativeHomeDir, req.OMCABinaryPath)
	if err != nil {
		return nil, nil, err
	}
	for i := range configFiles {
		configFiles[i].RelPath = filepath.Join(hostPrefix, configFiles[i].RelPath)
	}
	files = append(files, configFiles...)

	if host == "claude-code" {
		sources = append(sources, claudeConfigDirExclusionGapSources()...)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Concept != sources[j].Concept {
			return sources[i].Concept < sources[j].Concept
		}
		if sources[i].Source != sources[j].Source {
			return sources[i].Source < sources[j].Source
		}
		return sources[i].Reason < sources[j].Reason
	})

	return files, sources, nil
}
