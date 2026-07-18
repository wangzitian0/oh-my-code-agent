package mcp

import (
	"fmt"
	"os"

	"github.com/wangzitian0/oh-my-code-agent/internal/contextcost"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// ContextCostEstimate, ConfidenceEstimateNotMeasured, CountUserExclusions,
// and EstimateContextCost used to be defined directly in this package
// (issue #15's original omca_status stub). PR-20 (issue #24) moved the
// implementation to internal/contextcost — see that package's doc comment
// for why (breaking an import cycle between this package and
// internal/report, both of which need this logic) — and these are now thin
// re-exports, preserved so every existing external caller (cmd/omca/
// contextcost.go, internal/perf's real-environment measurement, and this
// package's own hostStatus below) keeps compiling against the identical
// names and behavior.
type ContextCostEstimate = contextcost.ContextCostEstimate

const ConfidenceEstimateNotMeasured = contextcost.ConfidenceEstimateNotMeasured

var (
	CountUserExclusions = contextcost.CountUserExclusions
	EstimateContextCost = contextcost.EstimateContextCost
)

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
	// Enforce ComputeStatusRequest.SessionGenerationID's own documented
	// contract ("required whenever SessionHost is set") instead of silently
	// skipping restart detection when it's violated (Copilot review finding
	// on this PR): a caller that determined SessionHost but failed to also
	// thread through SessionGenerationID is a real caller-composition bug
	// (most likely a wiring gap in whatever set OMCA_RUN_ID/SessionHost
	// upstream, cmd/omca/mcp.go's runMCP), the same class of mismatch
	// BootstrapRequest.validate()'s Observation-host check and
	// AppendLedgerEntry's entry.Host check already fail closed on elsewhere
	// in this codebase -- silently degrading restartRequired reporting
	// would mask exactly the kind of bug this check exists to surface.
	if req.SessionHost != "" && req.SessionGenerationID == "" {
		return StatusResult{}, fmt.Errorf("mcp: ComputeStatus: SessionGenerationID is required when SessionHost (%q) is set", req.SessionHost)
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
