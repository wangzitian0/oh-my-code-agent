package mcp

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// CompileFunc performs the one impure step ComputeStage needs beyond
// re-validation: compiling hostActivations -- ComputeStage's own
// already-decoded, already-gate-validated per-host Activation
// enable/disable selections (built from rp.Spec.Changes by
// buildHostActivations, below, so a CompileFunc implementation never needs
// to re-parse a RepairChange.Patch itself) -- into a FRESH pending
// generation across every host named, and reading back each of those
// hosts' EXISTING (untouched) current generation for ComputeStage's own
// diff/restart_required projection.
//
// All real I/O -- fresh detection/observation, internal/profiles.Compose,
// and the actual internal/resolve.Resolve -> internal/runtime.Compile call
// -- lives in the caller (cmd/omca/mcp.go), mirroring ArtifactFunc/
// CapabilityFunc's own "pure core in this package, impure part supplied by
// the caller" split (query.go, propose.go). A CompileFunc implementation
// MUST:
//
//   - compile against a FRESH re-composition of the worktree's real desired
//     state, exactly like runtime.Activate's freshSourceDigest CAS check
//     re-derives rather than trusts a stale snapshot, with hostActivations
//     merged on top;
//   - call runtime.SetPendingGeneration for every host it compiles, never
//     runtime.SetCurrentGeneration or anything else that would move
//     "current" -- staging must never activate;
//   - return currentByHost read-only (runtime.CurrentGenerationDir/
//     ReadGenerationManifest), never write through it.
//
// pending is the ONE domain.Generation produced (internal/runtime.Compile's
// own "every host combined into one Generation" design, compile_full.go) --
// currentByHost carries one entry per host named in pending.Spec.Hosts,
// omitted for a host with no current generation yet (that host's first-ever
// activation).
type CompileFunc func(hostActivations map[string]domain.HostActivation) (pending domain.Generation, currentByHost map[string]domain.Generation, err error)

// StageRejectedError reports that omca_stage refused to stage rp: either
// ComputePropose itself rejected it (Underlying is a *ProposeRejectedError,
// Class is empty), or ComputePropose accepted it but at a confirmation
// class other than AUTO_STAGE (Class names exactly which one -- round-4
// audit: "omca_stage refuses outright and names which class applied
// (CONFIRM_REQUIRED / REVIEWABLE_DIFF / PROHIBITED) so a human acting
// through the CLI directly ... has something concrete to act on later").
// There is no in-MCP human-confirmation flow in M4 (PR-31/M7 TUI scope) --
// this is always a hard rejection, never a partial-accept-then-wait state.
type StageRejectedError struct {
	Class       domain.RepairConfirmation
	Explanation string
	Underlying  error
}

func (e *StageRejectedError) Error() string {
	if e.Underlying != nil {
		return fmt.Sprintf("mcp: omca_stage: %v", e.Underlying)
	}
	return fmt.Sprintf("mcp: omca_stage: refused: proposal classifies as %s, not AUTO_STAGE -- omca_stage only ever stages an AUTO_STAGE proposal; every other class is a hard rejection (%s)", e.Class, e.Explanation)
}

func (e *StageRejectedError) Unwrap() error { return e.Underlying }

// StageArguments is omca_stage's "tools/call" arguments shape: the full
// RepairProposal document again, not a bare ID/proposal reference -- see
// CompileFunc's own doc comment and this package's doc.go for why (no
// proposal-persistence layer exists, and every call fully re-validates from
// scratch, CAS-style).
type StageArguments struct {
	Proposal domain.RepairProposal `json:"proposal"`
}

// StageHostResult is one host's compiled-into-pending outcome.
type StageHostResult struct {
	Host            string                   `json:"host"`
	Diff            []runtime.ProposedChange `json:"diff"`
	RestartRequired bool                     `json:"restartRequired"`
}

// StageResult is omca_stage's answer for a successfully staged proposal.
type StageResult struct {
	PendingGenerationID string            `json:"pendingGenerationId"`
	Hosts               []StageHostResult `json:"hosts"`
	Explanation         string            `json:"explanation"`
}

// buildHostActivations decodes every Activation-targeted RepairChange in
// changes into its one target host's domain.HostActivation, unioning
// Enable/Disable selections across any two changes naming the same host.
// Called only after ComputePropose has already accepted rp at AUTO_STAGE --
// every Change here has therefore already passed validateChangeShape (one
// spec.hosts entry, at least one enable/disable selection), so decodePatch
// is not expected to fail; an error here would mean ComputePropose's own
// gates and this function disagree about what a valid patch looks like,
// which is a bug in this package, not a proposal-content problem.
func buildHostActivations(changes []domain.RepairChange) (map[string]domain.HostActivation, error) {
	out := map[string]domain.HostActivation{}
	for i, c := range changes {
		if c.TargetKind != "Activation" {
			continue // AUTO_STAGE is only ever reachable via Activation changes (classifyChange) -- Profile/Binding changes never reach here
		}
		spec, err := decodePatch[domain.ActivationSpec](c.Patch)
		if err != nil {
			return nil, fmt.Errorf("spec.changes[%d]: %w", i, err)
		}
		host := activationPatchHost(spec)
		enable, disable := activationPatchSelections(spec)
		existing := out[host]
		existing.Enable.Skills = append(existing.Enable.Skills, enable.Skills...)
		existing.Enable.MCPServers = append(existing.Enable.MCPServers, enable.MCPServers...)
		existing.Disable.Skills = append(existing.Disable.Skills, disable.Skills...)
		existing.Disable.MCPServers = append(existing.Disable.MCPServers, disable.MCPServers...)
		out[host] = existing
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("an AUTO_STAGE proposal named no Activation-targeted change with a host to compile (should be unreachable: classifyChange only ever returns AUTO_STAGE for an Activation change)")
	}
	return out, nil
}

// ComputeStage stages rp: it fully re-validates rp against pc's fresh state
// via ComputePropose (the round-4 audit's CAS-style re-check -- report
// fingerprint included -- mirroring internal/runtime/activate.go's Activate
// re-deriving and comparing a fresh source digest rather than trusting
// whatever ComputePropose might have decided a moment earlier over a
// possibly-now-stale snapshot), requires the result classify as AUTO_STAGE
// exactly (every other class -- CONFIRM_REQUIRED, REVIEWABLE_DIFF,
// PROHIBITED -- is a hard rejection naming the class, never a partial
// accept), and only then calls compile to actually produce a fresh pending
// generation.
//
// ComputeStage itself never touches disk: compile is where all I/O
// happens, and ComputeStage's own contribution beyond re-validation is
// projecting compile's (pending, currentByHost) into a diff (runtime.
// DiffProposedChanges, the exact same diff machinery `omca compare`/`omca
// diff`/the real activation flow already use, per issue #25's own
// guidance) and a restartRequired verdict per host -- true when this host
// already had a current generation AND the diff is non-empty (activating
// this pending generation would move "current" out from under whatever is
// already running against the old one; a host with no current generation
// yet has nothing running to disrupt, so restarting is moot even though the
// diff itself is non-empty). ComputeStage never calls anything that could
// move "current" itself -- see stage_test.go's TestComputeStage_
// NeverMutatesCurrent for the property this closes.
func ComputeStage(pc ProposeContext, compile CompileFunc, rp domain.RepairProposal) (StageResult, error) {
	result, err := ComputePropose(pc, rp)
	if err != nil {
		return StageResult{}, &StageRejectedError{Underlying: err}
	}
	if result.Confirmation != domain.RepairAutoStage {
		return StageResult{}, &StageRejectedError{Class: result.Confirmation, Explanation: result.Explanation}
	}
	if compile == nil {
		return StageResult{}, fmt.Errorf("mcp: omca_stage: no CompileFunc is wired in this server")
	}

	hostActivations, err := buildHostActivations(rp.Spec.Changes)
	if err != nil {
		return StageResult{}, fmt.Errorf("mcp: omca_stage: %w", err)
	}

	pending, currentByHost, err := compile(hostActivations)
	if err != nil {
		return StageResult{}, fmt.Errorf("mcp: omca_stage: compiling pending generation: %w", err)
	}

	hosts := make([]string, 0, len(pending.Spec.Hosts))
	for h := range pending.Spec.Hosts {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	hostResults := make([]StageHostResult, 0, len(hosts))
	for _, h := range hosts {
		current := currentByHost[h] // zero value domain.Generation{} for a host's first-ever generation
		diff := runtime.DiffProposedChanges(current, pending, h)
		restartRequired := current.Metadata.ID != "" && len(diff) > 0
		hostResults = append(hostResults, StageHostResult{
			Host:            h,
			Diff:            diff,
			RestartRequired: restartRequired,
		})
	}

	return StageResult{
		PendingGenerationID: pending.Metadata.ID,
		Hosts:               hostResults,
		Explanation:         result.Explanation,
	}, nil
}

// stageToolDescription is what tools/list reports for omca_stage --
// deliberately small, matching this package's existing standard.
const stageToolDescription = "Fully re-validate a RepairProposal document (report fingerprint included, CAS-style) and, only if it classifies as AUTO_STAGE, compile it into a fresh pending generation for every host it names. Returns the diff and restart_required per host. Never mutates \"current\". Any other confirmation class (CONFIRM_REQUIRED/REVIEWABLE_DIFF/PROHIBITED) is a hard rejection naming the class -- there is no in-MCP confirmation flow."

// stageInputSchema is omca_stage's tools/list inputSchema -- identical
// shape to omca_propose's (the full RepairProposal document again).
func stageInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"proposal": map[string]any{
				"type":        "object",
				"description": "A full RepairProposal document, re-submitted in full (there is no proposal-persistence layer to reference by ID yet) -- see docs/architecture/reporting.md §11.3 and internal/domain/repairproposal.go.",
			},
		},
		"required":             []string{"proposal"},
		"additionalProperties": false,
	}
}

// stageToolHandler adapts artifactFn/capabilityFn/compileFn into a
// toolHandler, mirroring proposeToolHandler's own shape.
func stageToolHandler(artifactFn ArtifactFunc, capabilityFn CapabilityFunc, compileFn CompileFunc) toolHandler {
	return func(arguments json.RawMessage) (any, error) {
		var args StageArguments
		if len(arguments) > 0 {
			if err := json.Unmarshal(arguments, &args); err != nil {
				return nil, fmt.Errorf("mcp: omca_stage: invalid arguments: %w", err)
			}
		}
		artifact, err := artifactFn()
		if err != nil {
			return nil, fmt.Errorf("mcp: omca_stage: computing report: %w", err)
		}
		return ComputeStage(ProposeContext{Artifact: artifact, CapabilityFor: capabilityFn}, compileFn, args.Proposal)
	}
}

// StageToolEntry builds the registered omca_stage ToolEntry.
func StageToolEntry(artifactFn ArtifactFunc, capabilityFn CapabilityFunc, compileFn CompileFunc) ToolEntry {
	return ToolEntry{
		definition: toolDefinition{
			Name:        toolNameStage,
			Description: stageToolDescription,
			InputSchema: stageInputSchema(),
		},
		handler: stageToolHandler(artifactFn, capabilityFn, compileFn),
	}
}
