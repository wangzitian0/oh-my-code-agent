package mcp

import (
	"fmt"
	"os"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// estimatedTokensPerExcludedMCPServer / estimatedTokensPerExcludedSkill are
// fixed, documented, rough per-item token-cost averages this package
// multiplies an exclusion count by to produce ContextCostEstimate
// (issue #15's own suggested method: "N excluded MCP server tool-schema
// definitions x an estimated average token cost per schema, M excluded
// Skill descriptions x an estimated average token cost"). These are NOT
// measured from any real schema or description text — see
// ConfidenceEstimateNotMeasured's doc comment for why this stays an
// explicit estimate rather than borrowing domain.EvidenceLevel's E0-E5
// vocabulary, and docs/evidence/perf-v0.1.0.md for how these two constants
// were chosen (a small manual sample of real MCP tool-schema JSON and
// Skill-description frontmatter, not a rigorous corpus study).
const (
	estimatedTokensPerExcludedMCPServer = 200
	estimatedTokensPerExcludedSkill     = 150
)

// ConfidenceEstimateNotMeasured is the fixed confidence label every
// ContextCostEstimate carries. domain.EvidenceLevel (E0-E5) is this
// project's vocabulary for how strongly a claim about NATIVE HOST BEHAVIOR
// was established (docs/architecture/reporting.md §4) — a category
// mismatch for "how many tokens does an average excluded tool schema cost,"
// which is a modeling assumption about token economics, not a claim this
// package could ever raise to E2 (RESOLVED) by observing the host harder.
// Issue #15's own AC text explicitly allows either vocabulary ("domain.
// EvidenceLevel E0-E5, or a simpler explicit 'estimate, not measured'
// caveat — your call, but it must be honest about being an estimate"); this
// package picks the plain-language caveat as the documented, defensible
// choice, reserved as a named constant (not a literal repeated at every call
// site) so a future PR that replaces the fixed per-item averages with a
// real measurement (actually tokenizing excluded schemas/descriptions) has
// exactly one place to change the label.
const ConfidenceEstimateNotMeasured = "estimate, not measured -- no real MCP tool-schema or Skill description text was tokenized; this multiplies exclusion counts by fixed, documented per-item token averages (see docs/evidence/perf-v0.1.0.md)"

// ContextCostEstimate is the omca_status tool's answer to issue #15's
// "estimated context-cost delta with method + confidence" acceptance
// criterion, attached to one host's HostStatus.
type ContextCostEstimate struct {
	// EstimatedTokensExcluded is excludedMCPServers*estimatedTokensPer
	// ExcludedMCPServer + excludedSkills*estimatedTokensPerExcludedSkill:
	// the rough token count a native, unmanaged launch would have spent on
	// tool-schema/description text this managed session never loads.
	EstimatedTokensExcluded int `json:"estimatedTokensExcluded"`
	// Method is a human-readable description of exactly how
	// EstimatedTokensExcluded was computed, so a report reader never has to
	// take the number on faith.
	Method string `json:"method"`
	// Confidence is always ConfidenceEstimateNotMeasured today.
	Confidence string `json:"confidence"`
}

// HostStatus is one managed host's slice of the omca_status response.
type HostStatus struct {
	// Host is the canonical host ID ("codex" or "claude-code").
	Host string `json:"host"`
	// Managed reports whether this worktree has a compiled "current"
	// generation for Host at all. false means every other field below is
	// zero-valued; Detail explains why (no generation yet vs. a corrupt
	// pointer vs. an unreadable manifest) — the same "distinguish expected
	// not-yet-managed from real corruption" stance cmd/omca/doctor.go's
	// checkGenerationFreshness already takes.
	Managed bool `json:"managed"`
	// GenerationID is the current generation's metadata.id, when Managed.
	GenerationID string `json:"generationId,omitempty"`
	// ExcludedMCPServers / ExcludedSkills are the literal "N MCP servers and
	// M Skills excluded versus native" issue #15 names: a count of
	// gen.Spec.Sources entries for this host's current generation where
	// Concept is "mcp_server"/"skill", Scope is "user", and Included is
	// false — i.e. real, discovered native user-global sources this
	// generation's compiler actually excluded, not a capability-gap
	// placeholder (see CountUserExclusions).
	//
	// Granularity note: internal/observe's mcp_server concept is file-level
	// (internal/observe/rules.go's codexUserRules/claudeUserRules each name
	// exactly one user-scope MCP registration file per host — $CODEX_HOME/
	// config.toml, $CLAUDE_CONFIG_DIR/.claude.json), so ExcludedMCPServers is
	// really "excluded native MCP configuration sources," which for both
	// first-party hosts today is 0 or 1, not a count of individual
	// registered server IDs inside that file. Generation.Spec.Sources also
	// never carries an excluded native source's raw content (copying a real
	// native config's contents into a generation's own committed-forever
	// manifest would defeat the point of excluding it), so there is no way
	// to count individual entries without re-reading real native state at
	// status-query time — which this package deliberately does not do (see
	// ComputeStatus's doc comment). ExcludedSkills has no such ceiling:
	// internal/observe emits one observation per discovered SKILL.md file,
	// so it is a true per-Skill count.
	ExcludedMCPServers int `json:"excludedMcpServers"`
	ExcludedSkills     int `json:"excludedSkills"`
	// ContextCost is nil only when Managed is false (there is no generation
	// to estimate a delta for yet).
	ContextCost *ContextCostEstimate `json:"contextCost,omitempty"`
	// RestartRequired reports whether THIS session (the one whose OMCA
	// MCP server subprocess is answering this omca_status call) is running
	// on a generation "current" has since superseded for Host -- issue #19's
	// "a session running on a superseded generation is detected and
	// reported; restart_required is per host" AC (docs/architecture/
	// runtime.md §5.5). Only ever set for the ONE host
	// ComputeStatusRequest.SessionHost names (the host whose native-home
	// environment variable this process actually inherited -- see
	// ComputeStatusRequest.SessionHost's own doc comment); every other
	// host's HostStatus in the same response leaves this false, not because
	// it is known to be false, but because this process has no session
	// generation ID to compare for a host it was not itself launched by.
	RestartRequired bool `json:"restartRequired"`
	// SessionGenerationID is the generation this session was launched with,
	// when known (ComputeStatusRequest.SessionGenerationID, for Host ==
	// SessionHost only).
	SessionGenerationID string `json:"sessionGenerationId,omitempty"`
	// Detail is a human-readable note: why Managed is false, or (when
	// Managed) a short confirmation string. Always non-empty.
	Detail string `json:"detail"`
}

// StatusResult is the omca_status tool's complete, fixed-schema response:
// "context, generation ID, exclusion counts" per issue #15's own
// parenthetical.
type StatusResult struct {
	// WorktreeID is the OMCA_WORKTREE_ID this managed session's worktree
	// resolves to (internal/context.Worktree.ID).
	WorktreeID string `json:"worktreeId"`
	// ContextID is OMCA_CONTEXT_ID (cmd/omca/env.go's computeContextID) when
	// known -- empty if this MCP server was started outside a shell that
	// ever ran `omca env` (e.g. directly via `omca mcp serve` for manual
	// testing).
	ContextID string `json:"contextId,omitempty"`
	// Hosts carries one entry per host ComputeStatusRequest.Hosts named, in
	// the same order.
	Hosts []HostStatus `json:"hosts"`
}

// ComputeStatusRequest is everything ComputeStatus needs, explicit and
// caller-supplied -- this package never reads an environment variable or
// the real filesystem's ambient state itself, matching internal/context.
// Environment, internal/observe.Request, and internal/runtime.
// BootstrapRequest's identical "explicit inputs, nothing implicit"
// discipline. cmd/omca/mcp.go is the one place that resolves these from
// OMCA_WORKTREE_ID/OMCA_CONTEXT_ID/OMCA_STATE_DIR.
type ComputeStatusRequest struct {
	// WorktreeID becomes StatusResult.WorktreeID verbatim.
	WorktreeID string
	// ContextID becomes StatusResult.ContextID verbatim.
	ContextID string
	// WorktreeStateDir is the worktree's OMCA state directory (OMCA_STATE_DIR)
	// -- the same value cmd/omca/env.go and cmd/omca/run.go pass to
	// runtime.SetCurrentGeneration/CurrentGenerationDir. Required.
	WorktreeStateDir string
	// Hosts is the list of canonical host IDs to report on, in order --
	// normally hostcontext.DetectedHostIDs. This package does not import
	// internal/context (which would pull in host-version-probing scope this
	// purely-local-state read has no need for, the same minimal-dependency
	// judgment internal/shim's doc.go documents for its own package), so the
	// caller supplies the list directly.
	Hosts []string
	// SessionHost is the canonical host ID this specific `omca mcp serve`
	// process was spawned by, when known -- issue #19's restart_required
	// wiring. This process is always launched as a subprocess of exactly one
	// host's session (internal/runtime/compile.go's hostConfigFiles
	// registers it inside that host's own generated config), and therefore
	// inherits that host's native-home environment variable (CODEX_HOME or
	// CLAUDE_CONFIG_DIR, runtime.NativeHomeEnvVar) pointing into the
	// generation directory it was launched with -- cmd/omca/mcp.go's runMCP
	// determines SessionHost from exactly that signal (a documented
	// judgment call: these variables are only ever set, in this project's
	// own managed launch paths, to a value scoped to one host's generation).
	// Empty when this server was started outside any managed session (e.g.
	// `omca mcp serve` invoked directly for manual testing) -- restart_required
	// is then left false for every host, honestly reflecting "unknown," not
	// "confirmed fresh."
	SessionHost string
	// SessionGenerationID is the generation SessionHost's session was
	// launched with (OMCA_RUN_ID), required whenever SessionHost is set.
	SessionGenerationID string
}

// ComputeStatus builds one StatusResult from req, reading only the
// "current" generation pointer (and its manifest) for each of req.Hosts
// under req.WorktreeStateDir via internal/runtime -- no host detection, no
// native filesystem walk, no subprocess. This is deliberately the fastest,
// most local thing omca_status could do: the generation's own manifest.json
// already recorded every exclusion decision at compile time (internal/
// runtime/compile.go's Sources list), so re-deriving it here would just be
// slower, duplicated work for an identical answer.
func ComputeStatus(req ComputeStatusRequest) (StatusResult, error) {
	if req.WorktreeStateDir == "" {
		return StatusResult{}, fmt.Errorf("mcp: ComputeStatus: WorktreeStateDir is required")
	}
	out := StatusResult{WorktreeID: req.WorktreeID, ContextID: req.ContextID}
	for _, host := range req.Hosts {
		hs := hostStatus(req.WorktreeStateDir, host)
		if host == req.SessionHost && req.SessionGenerationID != "" {
			hs = applyRestartStatus(req.WorktreeStateDir, hs, req.SessionGenerationID)
		}
		out.Hosts = append(out.Hosts, hs)
	}
	return out, nil
}

// hostStatus builds one HostStatus for host, degrading honestly (Managed:
// false, an explanatory Detail) rather than failing the whole
// ComputeStatus call when this one host has no generation yet or its
// on-disk state is unreadable -- the same "one check's failure to run never
// suppresses the others" stance cmd/omca/doctor.go's runDoctor documents.
func hostStatus(worktreeStateDir, host string) HostStatus {
	genDir, err := runtime.CurrentGenerationDir(worktreeStateDir, host)
	if err != nil {
		if os.IsNotExist(err) {
			return HostStatus{Host: host, Managed: false, Detail: fmt.Sprintf("no current generation recorded for %s in this worktree yet (run `omca env` or `omca run %s`)", host, host)}
		}
		return HostStatus{Host: host, Managed: false, Detail: fmt.Sprintf("%s's current-generation pointer is corrupt: %v", host, err)}
	}
	gen, err := runtime.ReadGenerationManifest(genDir)
	if err != nil {
		return HostStatus{Host: host, Managed: false, Detail: fmt.Sprintf("current generation manifest for %s at %s is unreadable: %v", host, genDir, err)}
	}

	excludedMCP, excludedSkills := CountUserExclusions(gen)
	cost := EstimateContextCost(excludedMCP, excludedSkills)
	return HostStatus{
		Host:               host,
		Managed:            true,
		GenerationID:       gen.Metadata.ID,
		ExcludedMCPServers: excludedMCP,
		ExcludedSkills:     excludedSkills,
		ContextCost:        &cost,
		Detail:             fmt.Sprintf("managed: current generation %s", gen.Metadata.ID),
	}
}

// applyRestartStatus fills in hs.RestartRequired/SessionGenerationID for the
// one host this session was actually launched by (ComputeStatusRequest.
// SessionHost), via runtime.DetectRestartRequired -- issue #19's
// restart_required wiring. A failure to determine restart status (e.g. hs
// itself is not Managed, so there is no "current" to compare against) is
// reported in Detail rather than failing the whole status call, matching
// hostStatus's own degrade-honestly stance.
func applyRestartStatus(worktreeStateDir string, hs HostStatus, sessionGenerationID string) HostStatus {
	hs.SessionGenerationID = sessionGenerationID
	status, err := runtime.DetectRestartRequired(worktreeStateDir, hs.Host, sessionGenerationID)
	if err != nil {
		hs.Detail = fmt.Sprintf("%s (restart status unknown: %v)", hs.Detail, err)
		return hs
	}
	hs.RestartRequired = status.RestartRequired
	if status.RestartRequired {
		hs.Detail = status.Detail
	}
	return hs
}

// CountUserExclusions counts gen.Spec.Sources entries the M1 bootstrap
// policy excluded at user scope, split by concept -- issue #15's literal
// "N MCP servers and M Skills excluded versus native," computed directly
// from internal/runtime/compile.go's own recorded decisions (never
// re-derived by walking the real native homes again: Sources already IS
// the authoritative record of what this generation's compiler saw and
// decided, exactly like cmd/omca/doctor.go's checkStaleGeneration re-derives
// only when it needs to detect drift, not for a plain status read). Exported
// so internal/perf's real-environment measurement (M1's "record native vs
// managed... context cost before/after" round-2 AC line) can reuse the
// identical count rather than a second, driftable copy.
//
// Only Scope == "user" entries count, and a CapabilityGap entry (internal/
// runtime/compile.go's claudeConfigDirExclusionGapSources) is explicitly
// excluded even though it also carries Scope == "user" (Copilot review
// finding on this PR: an earlier version of this doc comment claimed a
// capability-gap entry "carries no Scope at all," which was never true of
// the actual claudeConfigDirExclusionGapSources implementation -- the
// filter below is what actually enforces the distinction the comment only
// asserted). A capability-gap entry describes an unproven exclusion
// *class* ("we don't yet behaviorally know whether every native user-global
// MCP server was really excluded"), not one discovered physical source this
// generation counted and excluded; counting it the same as a real,
// observed-and-excluded source would silently inflate N/M by one per gap
// class and overstate confidence in a number that is, for that class,
// genuinely unknown rather than confirmed.
func CountUserExclusions(gen domain.Generation) (mcpServers, skills int) {
	for _, s := range gen.Spec.Sources {
		if s.Included || s.Scope != "user" || s.CapabilityGap {
			continue
		}
		switch s.Concept {
		case "mcp_server":
			mcpServers++
		case "skill":
			skills++
		}
	}
	return mcpServers, skills
}

// EstimateContextCost is CountUserExclusions's companion: the estimated
// context-cost delta issue #15's AC requires alongside the exclusion
// counts, exported so internal/perf's real-environment measurement (M1's
// "record native vs managed startup time and context cost before/after"
// round-2 AC line) can reuse the identical method rather than a second,
// driftable copy.
func EstimateContextCost(excludedMCPServers, excludedSkills int) ContextCostEstimate {
	tokens := excludedMCPServers*estimatedTokensPerExcludedMCPServer + excludedSkills*estimatedTokensPerExcludedSkill
	return ContextCostEstimate{
		EstimatedTokensExcluded: tokens,
		Method: fmt.Sprintf(
			"%d excluded native MCP configuration source(s) x ~%d tokens/source (each source may register multiple individual servers; see HostStatus.ExcludedMCPServers' doc comment) + %d excluded Skill(s) x ~%d tokens/description (fixed, documented per-item averages, not measured from this session's actual excluded schemas/descriptions)",
			excludedMCPServers, estimatedTokensPerExcludedMCPServer, excludedSkills, estimatedTokensPerExcludedSkill,
		),
		Confidence: ConfidenceEstimateNotMeasured,
	}
}
