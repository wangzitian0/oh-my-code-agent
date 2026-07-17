package runtime

import "github.com/wangzitian0/oh-my-code-agent/internal/domain"

// BootstrapPolicyVersion names the current hardcoded M1 bootstrap policy
// (docs/project/roadmap.md M1; docs/architecture/runtime.md §3). It has no
// relationship to any Profile/Binding/Activation -- PR-12 is where a real
// Desired Graph starts existing. Bumping this version is how a future
// change to the fixed policy *itself* (not to any per-worktree input) forces
// every bootstrap generation's ID to change, since GenerationID folds
// BootstrapPolicyDigest into its input set (generationid.go).
const BootstrapPolicyVersion = "bootstrap-policy/v1"

// bootstrapPolicyValue is the fixed, explicitly-named "no desired state,
// hardcoded minimal policy" value substituted for
// domain.GenerationSpec.DesiredGraphDigest, which the schema marks required
// and digest-shaped even though a bootstrap generation is definitionally
// not derived from any real Desired Graph. See doc.go's "why
// desiredGraphDigest is a bootstrap-policy digest" section for the full
// reasoning. The Rules slice is documentation baked into the digested value
// itself (so the digest is traceable back to a human-readable policy
// statement, not an opaque version string alone) -- it must stay in sync
// with what compile.go's classify/compileHostTree actually do; a mismatch
// would be a bug in this package, not a schema violation, since nothing
// re-derives Rules from the real classification logic.
var bootstrapPolicyValue = struct {
	Version string   `json:"version"`
	Rules   []string `json:"rules"`
}{
	Version: BootstrapPolicyVersion,
	Rules: []string{
		"exclude every native user-global Instructions, Skill, MCP server, Hook, and Plugin source",
		"include the repository-scope Instructions chain",
		"exclude repository-scope Skill and MCP sources: not yet activated by any desired state",
		"apply conservative default permissions",
		"activate no MCP servers: no real Desired Graph exists before PR-12",
	},
}

// BootstrapPolicyDigest returns bootstrapPolicyValue's canonical digest --
// the value this package records as a bootstrap Generation's
// spec.desiredGraphDigest. Two calls always agree (bootstrapPolicyValue is a
// fixed package-level value); it changes only across a build that changed
// BootstrapPolicyVersion/Rules, i.e. only when the policy itself changed.
func BootstrapPolicyDigest() (string, error) {
	return domain.CanonicalDigest(bootstrapPolicyValue)
}

// AdapterID is the fixed adapterId this package records in a bootstrap
// Generation's spec.hosts[*].adapterId. It intentionally does not name an
// internal/plugin.AdapterID / HostAdapter plugin identity: this package is a
// direct compiler (internal/observe + internal/context + this fixed
// policy), not a HostAdapter-plugin-based compile pipeline -- see doc.go for
// why that frozen M0 contract is out of scope for this PR.
const AdapterID = "runtime.bootstrap-compiler"

// defaultSurface is used whenever a HostDetection does not report one,
// mirroring internal/observe.Observe's identical "cli" fallback
// (internal/observe/request.go's Observe).
const defaultSurface = "cli"

// ClaudeConfigDirExclusionGapIssueURL is the follow-up GitHub issue this PR
// filed for the capability gap claudeConfigDirExclusionGapSources
// (compile.go) records on every claude-code bootstrap generation: whether
// CLAUDE_CONFIG_DIR relocation actually, behaviorally, completely excludes
// native user-global MCP servers and Skills has never been confirmed by a
// real launch -- only by static binary inspection (E1 evidence). See
// knowledge/hosts/claude-code/cli/2.1/manifest.json's knownUnknowns and
// fixtures/README.md for the underlying evidentiary trail, and issue #13's
// round-2 anti-drift rule ("a shipped capability gap is tracked, never a
// resting state... this PR files a follow-up GitHub issue carrying its own
// exit gate, and the generation manifest links that issue") for why this
// constant exists at all rather than a bare, unlinked TODO comment.
const ClaudeConfigDirExclusionGapIssueURL = "https://github.com/wangzitian0/oh-my-code-agent/issues/47"
