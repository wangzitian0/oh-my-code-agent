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

// VirtualHomeDirName is the directory name this package uses, inside a
// generation's per-host tree (alongside NativeHomeDirName), for the
// directory a launch shim points the exec'd process's own HOME environment
// variable at (docs/architecture/runtime.md §7.1: "HOME=<generation>/
// virtual-home"). Unlike NativeHomeDirName, this name does not vary by
// host: HOME is not a host-specific config-location variable that only
// codex or only claude-code reads, it is the process's own home directory,
// and every first-party host resolves at least one native-home location
// relative to it regardless of a host-specific override --
// internal/context/host.go's codexNativeHomes and claudeNativeHomes both
// append a "HOME/.agents/skills" entry independent of whatever CODEX_HOME/
// CLAUDE_CONFIG_DIR resolve to. A launch shim that only relocates
// CODEX_HOME/CLAUDE_CONFIG_DIR and never HOME itself therefore still lets
// the real, unmanaged $HOME/.agents/skills load into the host process --
// this directory (created empty by Bootstrap/Compile, exactly like
// NativeHomeDir's directory) is what a real HOME override
// (internal/shim/exec.go's Plan.Exec, cmd/omca/run.go's runIsolated) points
// at instead, closing that gap for both hosts identically.
const VirtualHomeDirName = "virtual-home"

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

// sandboxPermissionValues names, per host, the native values this compiler
// knows how to render for the one policy.permissions key
// docs/product/requirements.md §4.1's own worked example documents
// ("sandbox"), ordered starting with this compiler's existing hardcoded
// conservative default (index 0 -- "read-only" for codex, "plan" for
// claude-code, exactly the literals this file hardcoded before permission
// compilation existed). A value not in this list, or a permission key other
// than "sandbox", is a capability this compiler does not yet know how to
// translate -- see resolveSandboxPermission, which reports that rather than
// guessing.
var sandboxPermissionValues = map[string][]string{
	"codex":       {"read-only", "workspace-write", "danger-full-access"},
	"claude-code": {"plan", "default", "acceptEdits", "bypassPermissions"},
}

// knownSandboxValue reports whether value is one this compiler recognizes as
// a real native rendering for host's sandbox permission.
func knownSandboxValue(host, value string) bool {
	for _, v := range sandboxPermissionValues[host] {
		if v == value {
			return true
		}
	}
	return false
}

// resolveSandboxPermission decides the native "sandbox" value hostConfigFiles
// renders for host, and the GenerationSourceEntry (nil for "no decision to
// record") documenting why. permissions is a resolved
// domain.ProfilePolicy.Permissions map -- nil/empty for Bootstrap (no real
// Desired Graph, policy.go), non-empty for Compile's real resolved policy
// (compile_full.go). A nil/empty permissions map, or one with no "sandbox"
// key, returns exactly (conservative default, nil entry) -- this is what
// keeps Bootstrap's existing generated content and Sources count completely
// unchanged by this feature (every PR-09/10/11 test predates permission
// compilation and asserts on the old hardcoded defaults).
//
// issue #18 round-2 AC: "policy.permissions values compile into host
// artifacts where the capability level allows; DENY/lock intent is never
// weakened by compilation." REQUIRED and DENIED are this project's two
// "sticky" intents (internal/resolve/resolve.go's own vocabulary): a
// REQUIRED value is always honored when recognized ("lock intent" -- never
// silently dropped), a DENIED value is NEVER honored (honoring it would be
// exactly the weakening the AC forbids) -- compilation instead keeps the
// conservative default and records why, regardless of whether the denied
// value happens to be one this compiler recognizes. DEFAULT/AVAILABLE honor
// the requested value only "where the capability level allows": a
// recognized value compiles in, an unrecognized one falls back to the
// conservative default with an explained exclusion, exactly like any other
// unrecognized/unproven source this package reports rather than silently
// accepting or silently dropping.
func resolveSandboxPermission(host string, permissions map[string]domain.PermissionRef) (value string, entry *domain.GenerationSourceEntry) {
	defaults := sandboxPermissionValues[host]
	conservative := ""
	if len(defaults) > 0 {
		conservative = defaults[0]
	}
	ref, ok := permissions["sandbox"]
	if !ok {
		return conservative, nil
	}

	known := knownSandboxValue(host, ref.Value)
	switch ref.Intent {
	case domain.IntentDenied:
		return conservative, &domain.GenerationSourceEntry{
			Concept:  "permission",
			Source:   "sandbox",
			Included: false,
			Reason:   fmt.Sprintf("excluded: DENIED by profile policy -- value %q is denied; compiled artifact keeps the conservative default %q rather than weakening the deny (issue #18 round-2 AC)", ref.Value, conservative),
		}
	case domain.IntentRequired:
		if !known {
			return conservative, &domain.GenerationSourceEntry{
				Concept:  "permission",
				Source:   "sandbox",
				Included: false,
				Reason:   fmt.Sprintf("excluded: REQUIRED permission value %q is not a value this compiler recognizes for %s's sandbox permission; compiled artifact keeps the conservative default %q rather than guessing at an unverified native value", ref.Value, host, conservative),
			}
		}
		return ref.Value, &domain.GenerationSourceEntry{
			Concept:  "permission",
			Source:   "sandbox",
			Included: true,
			Reason:   fmt.Sprintf("included: REQUIRED by profile policy, compiled value %q", ref.Value),
		}
	case domain.IntentDefault, domain.IntentAvailable:
		if !known {
			return conservative, &domain.GenerationSourceEntry{
				Concept:  "permission",
				Source:   "sandbox",
				Included: false,
				Reason:   fmt.Sprintf("excluded: %s permission value %q is not a value this compiler recognizes for %s's sandbox permission; compiled artifact keeps the conservative default %q", ref.Intent, ref.Value, host, conservative),
			}
		}
		return ref.Value, &domain.GenerationSourceEntry{
			Concept:  "permission",
			Source:   "sandbox",
			Included: true,
			Reason:   fmt.Sprintf("included: %s by profile policy, compiled value %q", ref.Intent, ref.Value),
		}
	default:
		return conservative, &domain.GenerationSourceEntry{
			Concept:  "permission",
			Source:   "sandbox",
			Included: false,
			Reason:   fmt.Sprintf("excluded: unrecognized intent %q for permission \"sandbox\"; compiled artifact keeps the conservative default %q", ref.Intent, conservative),
		}
	}
}

// hostConfigFiles returns every OMCA-authored config file this package
// always emits inside nativeHomeDir: permission defaults (docs/project/
// roadmap.md M1's conservative values, or a resolved policy.permissions
// value where resolveSandboxPermission's capability table allows it), plus
// -- when omcaBinaryPath is non-empty -- the OMCA MCP server registration
// (docs/architecture/runtime.md §3: "the bootstrap generation contains...
// the OMCA MCP server"; FR-7). This is the scope cut doc.go's "What is
// deliberately NOT in the generated tree yet" section flagged as open
// ("internal/mcp... is still an empty doc.go stub -- there is no real
// command or protocol handler this compiler could point a generated config
// entry at yet"): issue #15 built internal/mcp and `omca mcp serve`, so this
// function now closes that gap whenever a caller supplies a real
// omcaBinaryPath.
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
// version this replaces. The returned GenerationSourceEntry slice is
// permission-compilation sources only (empty/nil when permissions is
// empty/nil, exactly PR-09's behavior); compileHostTree appends it to the
// Observation-derived sources list.
func hostConfigFiles(host, nativeHomeDir, omcaBinaryPath string, permissions map[string]domain.PermissionRef) ([]generatedFile, []domain.GenerationSourceEntry, error) {
	sandboxValue, permEntry := resolveSandboxPermission(host, permissions)
	var permSources []domain.GenerationSourceEntry
	if permEntry != nil {
		permSources = append(permSources, *permEntry)
	}

	switch host {
	case "codex":
		content := "" +
			"# OMCA generation: permission defaults (docs/project/roadmap.md M1 conservative\n" +
			"# baseline, or a resolved policy.permissions value where recognized -- issue #18).\n" +
			"approval_policy = \"untrusted\"\n" +
			fmt.Sprintf("sandbox_mode = %s\n", tomlString(sandboxValue))
		if omcaBinaryPath != "" {
			content += codexMCPRegistrationTOML(omcaBinaryPath)
		}
		return []generatedFile{{RelPath: filepath.Join(nativeHomeDir, "config.toml"), Content: []byte(content)}}, permSources, nil
	case "claude-code":
		settingsDoc := struct {
			Permissions struct {
				DefaultMode string `json:"defaultMode"`
			} `json:"permissions"`
		}{}
		settingsDoc.Permissions.DefaultMode = sandboxValue
		// json.Marshal (not MarshalIndent) to keep the original compact
		// single-line form this file has always emitted -- tests
		// substring-match against `"defaultMode"` on one line.
		compact, err := json.Marshal(settingsDoc)
		if err != nil {
			return nil, nil, fmt.Errorf("runtime: hostConfigFiles: %w", err)
		}
		content := append(compact, '\n')
		files := []generatedFile{{RelPath: filepath.Join(nativeHomeDir, "settings.json"), Content: content}}
		if omcaBinaryPath != "" {
			claudeJSON, err := claudeMCPRegistrationJSON(omcaBinaryPath)
			if err != nil {
				return nil, nil, err
			}
			files = append(files, generatedFile{RelPath: filepath.Join(nativeHomeDir, ".claude.json"), Content: claudeJSON})
		}
		return files, permSources, nil
	default:
		return nil, nil, fmt.Errorf("runtime: hostConfigFiles: unsupported host %q", host)
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
//
// This "structural, compiler-controlled" claim was, for a time, only half
// true: CODEX_HOME relocation always was compiler-controlled the way this
// comment describes, but HOME itself was NOT actually redirected at launch
// (internal/shim/exec.go's Plan.Exec and cmd/omca/run.go's runIsolated only
// ever injected CODEX_HOME/CLAUDE_CONFIG_DIR), so the real, unmanaged
// $HOME/.agents/skills a host binary resolves independent of either
// variable (internal/context/host.go's codexNativeHomes and
// claudeNativeHomes both append it unconditionally) still loaded on every
// real launch -- a genuine native-config leak this bootstrap generation's
// own manifest never reported, for either host, because Codex's Sources
// list carried no capability-gap entry for it at all. That gap is now
// closed: VirtualHomeDirName (above) gives every generation an empty,
// compiler-controlled directory, and the launch paths now redirect HOME to
// it (docs/architecture/runtime.md §7.1's full documented env set), so the
// "this compiler itself fully controls by construction" claim now actually
// covers HOME resolution too, not only CODEX_HOME -- see
// TestPlanExec_SetsHomeAndRealHome (internal/shim/exec_test.go) and
// TestRunIsolated_EndToEnd_VirtualizesHome (cmd/omca/run_exec_test.go) for
// the regression coverage proving it.
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

// classifyFunc decides whether one Observation belongs in a generation's
// artifact tree, and why -- the one genuinely policy-specific (as opposed to
// general/shared) decision compileHostTree needs from its caller (issue #18
// round-2 MECE requirement: "the bootstrap path and full compilation share
// one compiler core; bootstrap is the minimal-input case, not a second
// implementation"). Bootstrap (PR-09) supplies classify, above -- the fixed,
// hardcoded M1 policy. Compile (PR-14, compile_full.go) supplies the exact
// same classify function today (see compile_full.go's doc comment for why:
// there is no Identity Matcher yet to do anything more desired-state-aware
// with a physically-discovered Observation), but the seam exists so that
// changes independently.
type classifyFunc func(domain.Observation) (included bool, reason string)

// hostTreeInput is compileHostTree's one input shape: every value the
// shared compiler core needs to walk one host's Observations and render its
// native config artifacts, decoupled from BootstrapRequest so a second
// caller (Compile, compile_full.go) does not need to construct a
// BootstrapRequest just to reach this logic. This is the concrete seam the
// round-2 MECE requirement asks for: everything compileHostTree does with a
// hostTreeInput -- walking Observations, deciding file placement via
// Classify, computing the config files hostConfigFiles renders, assembling
// GenerationSourceEntry records -- is the single shared implementation
// neither Bootstrap nor Compile forks; only Classify and Permissions differ
// between the two callers, and both are passed in as plain data, not
// branched on internally.
type hostTreeInput struct {
	Host           string
	Surface        string
	WorktreeRoot   string
	Observations   []domain.Observation
	OMCABinaryPath string
	// Permissions is the resolved domain.ProfilePolicy.Permissions map this
	// host's generation should compile permission defaults from. Bootstrap
	// always passes nil (no real Desired Graph, policy.go) and gets exactly
	// the old hardcoded conservative defaults with zero extra Sources
	// entries; Compile passes the real resolved policy.
	Permissions map[string]domain.PermissionRef
	Classify    classifyFunc
}

// compileHostTree is the shared generation-compiler core (issue #18 round-2
// MECE requirement): given one host's already-computed Observation
// inventory and a caller-supplied policy (in.Classify, in.Permissions), it
// returns every file to place in the generation's per-host artifact tree
// (path relative to the generation directory root) and the full sources
// list manifest.json.spec.sources records (issue #13 AC "The manifest lists
// every included and excluded source with a reason"). Both Bootstrap
// (bootstrap.go) and Compile (compile_full.go) call this exact function
// through compileHostTreeFn -- see that variable's doc comment for how a
// test proves neither one forked its own copy of this logic.
func compileHostTree(in hostTreeInput) ([]generatedFile, []domain.GenerationSourceEntry, error) {
	nativeHomeDir, err := NativeHomeDirName(in.Host)
	if err != nil {
		return nil, nil, err
	}
	hostPrefix := filepath.Join("hosts", in.Host, in.Surface)

	var files []generatedFile
	var sources []domain.GenerationSourceEntry

	for _, o := range in.Observations {
		included, reason := in.Classify(o)
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
				rel, relErr := filepath.Rel(in.WorktreeRoot, o.Spec.Source.Path)
				if relErr != nil {
					return nil, nil, fmt.Errorf("runtime: compileHostTree: %w", relErr)
				}
				if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return nil, nil, fmt.Errorf("runtime: compileHostTree: repository Instructions source %s resolves outside the worktree root %s", o.Spec.Source.Path, in.WorktreeRoot)
				}
				files = append(files, generatedFile{
					RelPath: filepath.Join(hostPrefix, "instructions", rel),
					Content: content,
				})
			}
		}
		sources = append(sources, entry)
	}

	configFiles, permSources, err := hostConfigFiles(in.Host, nativeHomeDir, in.OMCABinaryPath, in.Permissions)
	if err != nil {
		return nil, nil, err
	}
	for i := range configFiles {
		configFiles[i].RelPath = filepath.Join(hostPrefix, configFiles[i].RelPath)
	}
	files = append(files, configFiles...)
	sources = append(sources, permSources...)

	if in.Host == "claude-code" {
		sources = append(sources, claudeConfigDirExclusionGapSources()...)
	}

	// Stamp every entry with the host this call compiled for (Copilot
	// review finding, issue #19/PR-15): compileHostTree always runs in a
	// single-host context, so every entry it produces -- Observation-
	// derived, permission-compilation, or the Claude Code capability-gap
	// pair -- belongs to in.Host. A single blanket backfill here is simpler
	// and less error-prone than threading Host through each of the three
	// entry-producing paths individually, and is exactly what lets a
	// shared multi-host Generation's flat Spec.Sources list still answer
	// "which host does this entry belong to" once compileHostTree's three
	// per-host calls (one per host in req.Hosts, compile_full.go) are all
	// merged together.
	for i := range sources {
		sources[i].Host = in.Host
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

// compileHostTreeFn holds compileHostTree's function value in a package
// variable that Bootstrap (bootstrap.go) and Compile (compile_full.go) both
// call instead of calling compileHostTree directly. Production code never
// reassigns it; it exists purely as a seam runtimesharedcore_test.go's
// TestSharedCompilerCore_BootstrapAndCompileUseSameCore uses to prove, by
// swapping in a counting wrapper for the duration of one test, that both
// entry points really do route through the identical implementation --
// issue #18's round-2 MECE requirement ("one compiler, two entry points")
// made mechanically checkable rather than merely asserted in a comment: a
// future change that forked this logic into two copies (e.g. a
// Compile-specific reimplementation of Observation-walking) would make that
// test's call count stop matching, whether or not anyone remembered to
// update this doc comment.
var compileHostTreeFn = compileHostTree
