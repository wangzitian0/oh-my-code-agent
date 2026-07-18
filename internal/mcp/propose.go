package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
)

// CapabilityFunc resolves the fresh domain.CapabilityOps a host's Knowledge
// Pack proves for one ontology concept ("mcp_server", "skill",
// "instruction" -- docs/ontology/README.md's snake_case vocabulary, the
// same one report.HostDebug's Observation-derived data uses). ok is false
// when the host has no qualified Knowledge Pack at all, or the pack
// declares no capability entry for concept -- either case the capability
// gate (below) treats as "not proven," never as an implicit pass, matching
// internal/effective/merge.go's capabilityQualified's own fail-closed
// stance. Called fresh for every omca_propose/omca_stage request, mirroring
// ArtifactFunc's "never answer from a value computed once at startup"
// discipline (query.go) -- cmd/omca/mcp.go wires this against the same
// internal/knowledge.Repository.Resolve(...).CapabilityFor(...) lookup
// internal/report/build.go's own Resolve+findPack path already uses.
type CapabilityFunc func(host, concept string) (domain.CapabilityOps, bool)

// ProposeContext bundles the two already-fresh, caller-supplied inputs
// ComputePropose needs beyond the proposal itself: a report.Artifact (for
// the fingerprint check and the per-host resolve.ResolvedState the policy
// gate reads via HostDebug.Desired) and a CapabilityFunc (for the
// capability gate). Both are resolved fresh by the caller immediately
// before validating -- the exact same "never trust a stale snapshot"
// discipline every other MCP tool in this package already follows.
type ProposeContext struct {
	Artifact      report.Artifact
	CapabilityFor CapabilityFunc
}

// ProposeRejectedError reports that omca_propose refused a RepairProposal
// at exactly one of the six gates docs/product/requirements.md §8 names
// ("OMCA validates every proposal against schemas, capability gates,
// policy, ownership, source digests, and risk confirmation before writing
// anything"): Gate is one of "schema", "fingerprint", "ownership",
// "capability", "policy", "risk". This mirrors this package's sibling
// distinguished-error precedent (runtime.CASMismatchError, runtime.
// ConfirmationRequiredError, profiles.AmbiguousIdentityError): a caller (a
// test, or a real MCP client rendering the tool-level error text) can act
// on exactly which gate stopped a given proposal, not just that validation
// failed somewhere.
type ProposeRejectedError struct {
	Gate   string
	Reason string
}

func (e *ProposeRejectedError) Error() string {
	return fmt.Sprintf("mcp: omca_propose: rejected at the %q gate: %s", e.Gate, e.Reason)
}

// ProposeArguments is omca_propose's "tools/call" arguments shape: the full
// RepairProposal document, not a bare ID reference -- there is deliberately
// no proposal-persistence layer yet (internal/artifact remains a reserved
// stub, see its own doc comment), so every omca_propose call is
// self-contained.
type ProposeArguments struct {
	Proposal domain.RepairProposal `json:"proposal"`
}

// ProposeResult is omca_propose's answer for an ACCEPTED proposal (a
// rejected one is reported as a tool-level error carrying a
// *ProposeRejectedError's text -- see handleToolsCall's own doc comment for
// why a validation failure is a tool-level, not protocol-level, error).
// Confirmation is OMCA's own authoritative classification
// (docs/product/requirements.md §7's table via classifyChange below) --
// never the proposal's own self-declared spec.confirmation, which
// ComputePropose reads only far enough to reject an outright PROHIBITED
// claim (domain.ValidateRepairProposal) and otherwise ignores, exactly
// because §8's LLM Boundary is "LLM output never changes ... risk
// confirmation ... level."
type ProposeResult struct {
	Accepted     bool                      `json:"accepted"`
	Confirmation domain.RepairConfirmation `json:"confirmation"`
	Explanation  string                    `json:"explanation"`
}

// specPatch decodes a RepairChange.Patch's "{\"spec\": {...}}" convention
// (see internal/domain/testdata/repairproposal-valid.json's own
// spec.changes[].patch shape, the M0/PR-04 golden fixture this PR builds
// on) into T -- the same partial-document-body shape as the real Profile/
// Binding/Activation document Patch targets, wrapped exactly like the real
// document's own top-level "spec" field.
type specPatch[T any] struct {
	Spec T `json:"spec"`
}

// decodePatch strictly decodes patch (a RepairChange.Patch's raw
// map[string]any, as JSON-unmarshaled by encoding/json's default
// map[string]any handling) into T via specPatch[T], rejecting any key
// outside {"spec": {...T's own fields...}} -- json.Decoder.
// DisallowUnknownFields, so a patch naming a field T's shape does not
// recognize (a typo, or an attempt to smuggle a shape this gate does not
// expect) is a schema-gate rejection rather than a silently-dropped no-op.
// This is the "whatever additional structural checks the specific
// RepairChange.TargetKind/Patch shape needs" half of the schema gate issue
// #25's own AC text names, beyond domain.ValidateRepairProposal's own
// shape-agnostic checks.
func decodePatch[T any](patch map[string]any) (T, error) {
	var zero T
	raw, err := json.Marshal(patch)
	if err != nil {
		return zero, fmt.Errorf("marshaling patch for decoding: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var wrapper specPatch[T]
	if err := dec.Decode(&wrapper); err != nil {
		return zero, fmt.Errorf("patch does not match the expected {\"spec\": {...}} shape: %w", err)
	}
	return wrapper.Spec, nil
}

// validateChangeShape decodes c.Patch against the domain schema its own
// TargetKind implies (domain.ActivationSpec/ProfileSpec/BindingSpec --
// reusing the frozen document schemas directly rather than inventing a
// parallel patch schema), returning a descriptive error for a structurally
// invalid patch. c.TargetKind is assumed already validated to be one of
// Profile/Binding/Activation by domain.ValidateRepairProposal, which runs
// before this in ComputePropose's gate order.
func validateChangeShape(c domain.RepairChange) error {
	switch c.TargetKind {
	case "Profile":
		_, err := decodePatch[domain.ProfileSpec](c.Patch)
		return err
	case "Binding":
		_, err := decodePatch[domain.BindingSpec](c.Patch)
		return err
	case "Activation":
		spec, err := decodePatch[domain.ActivationSpec](c.Patch)
		if err != nil {
			return err
		}
		return validateActivationPatchSpec(spec)
	default:
		// Unreachable once domain.ValidateRepairProposal has already run
		// (it rejects any other targetKind), kept as a fail-closed default
		// rather than a panic.
		return fmt.Errorf("unrecognized targetKind %q", c.TargetKind)
	}
}

// validateActivationPatchSpec additionally requires that an Activation
// patch names at least one enable/disable selection AND scopes itself to
// exactly one host via spec.hosts (never a bare host-neutral spec.enable/
// spec.disable). This is a deliberate, documented narrowing beyond what
// domain.ActivationSpec's own schema requires: internal/resolve.Resolve
// happily accepts a host-neutral Activation entry, but omca_stage compiles
// ONE pending generation per host (docs/architecture/runtime.md §5.5,
// "activation is per host"), so a proposal has to say which host(s) its
// change actually targets for staging to have anywhere concrete to write.
// A proposal that legitimately wants a host-neutral effect submits one
// RepairChange per host it should apply to, each naming that host under
// spec.hosts -- more verbose, but never ambiguous about where compiling it
// would write.
func validateActivationPatchSpec(spec domain.ActivationSpec) error {
	if len(spec.Hosts) != 1 {
		return fmt.Errorf("RepairChange targetKind Activation: patch.spec.hosts must name exactly one host (got %d) -- omca_stage compiles one pending generation per host and a host-neutral spec.enable/spec.disable does not say which one", len(spec.Hosts))
	}
	enable, disable := activationPatchSelections(spec)
	if len(enable.Skills) == 0 && len(enable.MCPServers) == 0 && len(disable.Skills) == 0 && len(disable.MCPServers) == 0 {
		return fmt.Errorf("RepairChange targetKind Activation: patch names no enable/disable selection at all")
	}
	for h := range spec.Hosts {
		if err := domain.ValidateHostID(h); err != nil {
			return fmt.Errorf("RepairChange targetKind Activation: patch.spec.hosts: %w", err)
		}
	}
	return nil
}

// activationPatchHost returns the single host an already-validated
// Activation patch (validateActivationPatchSpec has already required
// exactly one spec.hosts entry) targets.
func activationPatchHost(spec domain.ActivationSpec) string {
	for h := range spec.Hosts {
		return h
	}
	return ""
}

// activationPatchSelections returns the effective enable/disable selections
// an already-validated Activation patch describes: its host-neutral
// spec.enable/spec.disable (rare but schema-legal alongside a host-scoped
// entry) combined with its one spec.hosts[host] entry's own enable/disable.
func activationPatchSelections(spec domain.ActivationSpec) (enable, disable domain.ActivationSelection) {
	host := activationPatchHost(spec)
	hostAct := spec.Hosts[host]
	enable.Skills = append(append([]string(nil), spec.Enable.Skills...), hostAct.Enable.Skills...)
	enable.MCPServers = append(append([]string(nil), spec.Enable.MCPServers...), hostAct.Enable.MCPServers...)
	disable.Skills = append(append([]string(nil), spec.Disable.Skills...), hostAct.Disable.Skills...)
	disable.MCPServers = append(append([]string(nil), spec.Disable.MCPServers...), hostAct.Disable.MCPServers...)
	return enable, disable
}

// ontologyConceptFor maps resolve.AssetKind's camelCase wire vocabulary to
// the ontology's snake_case Concept vocabulary a Knowledge Pack's
// Capabilities map is keyed by (docs/ontology/README.md; see query.go's own
// doc comment on QueryKindGeneration/resolvedAssetSources for why these are
// two distinct, independently documented vocabularies in this codebase).
func ontologyConceptFor(kind resolve.AssetKind) string {
	switch kind {
	case resolve.KindMCPServer:
		return "mcp_server"
	case resolve.KindSkill:
		return "skill"
	case resolve.KindInstruction:
		return "instruction"
	default:
		return string(kind)
	}
}

// capabilityQualifiedForCompile reports whether ops proves a qualified
// COMPILE capability -- the same EXACT/COMPATIBLE-only shape internal/
// effective/merge.go's capabilityQualified already checks for a merge
// decision's RESOLVE capability, reused here (not reinvented) for a
// different operation: omca_propose is asking "can this Knowledge Pack's
// evidence actually be compiled into a generation artifact for this
// concept," not "can conflicting sources be merged for it."
func capabilityQualifiedForCompile(ops domain.CapabilityOps) bool {
	return ops.Compile == domain.CapabilityExact || ops.Compile == domain.CapabilityCompatible
}

// capabilityAndPolicyGates runs the capability gate (gate 4) and policy
// gate (gate 5) together for one already-shape-validated Activation
// RepairChange: both only ever apply to an ENABLE selection (disabling an
// asset never needs new capability or contradicts a DENIED policy -- it can
// only narrow, never expand, what is active), scoped to the patch's one
// named host.
func capabilityAndPolicyGates(pc ProposeContext, c domain.RepairChange) error {
	spec, err := decodePatch[domain.ActivationSpec](c.Patch)
	if err != nil {
		return err // already proven to decode cleanly by the schema gate; defensive only
	}
	host := activationPatchHost(spec)
	enable, _ := activationPatchSelections(spec)

	checks := []struct {
		kind resolve.AssetKind
		ids  []string
	}{
		{resolve.KindMCPServer, enable.MCPServers},
		{resolve.KindSkill, enable.Skills},
	}
	for _, chk := range checks {
		for _, id := range chk.ids {
			concept := ontologyConceptFor(chk.kind)
			if pc.CapabilityFor == nil {
				return &ProposeRejectedError{Gate: "capability", Reason: fmt.Sprintf("no CapabilityFunc is wired in this server -- cannot prove host %q's Knowledge Pack supports concept %q, failing closed", host, concept)}
			}
			ops, ok := pc.CapabilityFor(host, concept)
			if !ok || !capabilityQualifiedForCompile(ops) {
				return &ProposeRejectedError{Gate: "capability", Reason: fmt.Sprintf("host %q's Knowledge Pack does not prove a qualified compile capability for concept %q (enabling %s %q); resolve=%q compile=%q", host, concept, chk.kind, id, ops.Resolve, ops.Compile)}
			}
			if hd, ok := pc.Artifact.Debug[host]; ok {
				if asset, found := hd.Desired.Find(chk.kind, id); found && asset.Intent == domain.IntentDenied {
					return &ProposeRejectedError{Gate: "policy", Reason: fmt.Sprintf("host %q's already-resolved desired state DENIES %s %q (%s); a proposal cannot silently override an established DENIED policy outcome (docs/product/requirements.md, resolve.Resolve semantics)", host, chk.kind, id, asset.Reason)}
				}
			}
		}
	}
	return nil
}

// classifyChange implements docs/product/requirements.md §7's risk-based
// confirmation table for one RepairChange, closing it into
// domain.RepairConfirmation -- this package's own analogue of
// internal/runtime.ClassifyChange (which classifies an activation-flow
// runtime.ProposedChange into a runtime.ConfirmationClass over the exact
// same table), reused in spirit rather than by direct call: a RepairChange
// is shaped around a document-Patch, not a resolved GenerationSourceEntry
// diff, so there is no single ProposedChange to hand runtime.ClassifyChange
// without first performing exactly the patch decoding this function does
// anyway.
func classifyChange(c domain.RepairChange) (domain.RepairConfirmation, string, error) {
	switch c.TargetKind {
	case "Profile":
		spec, err := decodePatch[domain.ProfileSpec](c.Patch)
		if err != nil {
			return "", "", err
		}
		if len(spec.Policy.Permissions) > 0 {
			return domain.RepairConfirmRequired, fmt.Sprintf("Profile %q patch expands policy.permissions -- expanding filesystem, network, approval, or sandbox access always requires confirmation (docs/product/requirements.md §7)", c.TargetID), nil
		}
		return domain.RepairReviewableDiff, fmt.Sprintf("Profile %q is a shared project/company Profile -- modifying it produces a reviewable repository diff (docs/product/requirements.md §7)", c.TargetID), nil
	case "Binding":
		return domain.RepairReviewableDiff, fmt.Sprintf("Binding %q selects which shared Profiles govern a repository/path context -- treated with the same reviewable-diff floor as a direct shared-Profile edit, never auto-staged", c.TargetID), nil
	case "Activation":
		spec, err := decodePatch[domain.ActivationSpec](c.Patch)
		if err != nil {
			return "", "", err
		}
		enable, _ := activationPatchSelections(spec)
		if len(enable.MCPServers) > 0 {
			return domain.RepairConfirmRequired, fmt.Sprintf("enables MCP server(s) %v -- confirm command, network destinations, and secret references before staging (docs/product/requirements.md §7)", enable.MCPServers), nil
		}
		return domain.RepairAutoStage, "selects an already-reviewed Skill, or only disables assets -- stage the pending generation automatically (docs/product/requirements.md §7)", nil
	default:
		return domain.RepairConfirmRequired, fmt.Sprintf("unrecognized targetKind %q -- failing closed to CONFIRM_REQUIRED rather than guessing a lower-risk class", c.TargetKind), nil
	}
}

// mostRestrictive returns whichever of a/b requires more human review, in
// docs/product/requirements.md §7's own severity order: PROHIBITED >
// CONFIRM_REQUIRED > REVIEWABLE_DIFF > AUTO_STAGE. A proposal with several
// Changes is only ever as safe as its riskiest single Change.
func mostRestrictive(a, b domain.RepairConfirmation) domain.RepairConfirmation {
	rank := map[domain.RepairConfirmation]int{
		domain.RepairAutoStage:       0,
		domain.RepairReviewableDiff:  1,
		domain.RepairConfirmRequired: 2,
		domain.RepairProhibited:      3,
	}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// ComputePropose validates rp against pc's fresh state through six gates,
// in order -- risk (the one PROHIBITED short-circuit), schema, fingerprint,
// ownership, capability, policy -- matching docs/product/requirements.md
// §8's "OMCA validates every proposal against schemas, capability gates,
// policy, ownership, source digests, and risk confirmation before writing
// anything." Every gate is pass/fail (a failure returns a
// *ProposeRejectedError naming exactly which gate) EXCEPT the risk gate's
// own second half, which runs last: classifying every Change into
// domain.RepairConfirmation is not itself a pass/fail decision -- an
// AUTO_STAGE-eligible proposal and a CONFIRM_REQUIRED one are both
// "accepted" by ComputePropose (both are valid, honestly-classified
// proposals), and it is omca_stage's own AUTO_STAGE-only rule (stage.go)
// that turns "not AUTO_STAGE" into a rejection. This split matters: a
// caller asking only "is this proposal well-formed and within policy" (a
// TUI listing outstanding proposals for a human to review later, roadmap
// M7) needs ComputePropose to answer that for a REVIEWABLE_DIFF/
// CONFIRM_REQUIRED proposal too, not just for the auto-stageable ones.
//
// The risk gate's FIRST half -- rejecting an outright PROHIBITED
// self-declaration (rp.Spec.Confirmation) -- runs before schema validation
// and is reported as its own "risk" gate, even though domain.
// ValidateRepairProposal (the schema gate) would also catch it: issue #25's
// own AC wants a rejection test dedicated to the risk gate specifically,
// distinct from a structural schema violation, and "prohibited by default"
// (docs/product/requirements.md §7's own words for this exact row) is a
// risk classification, not a shape problem. A PROHIBITED classification can
// otherwise never arise from a Change's own patch content: classifyChange
// only ever derives AUTO_STAGE/CONFIRM_REQUIRED/REVIEWABLE_DIFF from a
// Profile/Binding/Activation patch, because RepairChange.TargetKind's
// closed vocabulary has no shape that could express docs/product/
// requirements.md §7's actual PROHIBITED row ("import a native credential
// file") -- the same "classified only, not reachable through any real...
// code path" honesty internal/runtime.ClassifyChange's own doc comment
// already establishes for its sibling unreachable rows.
//
// ComputePropose is pure and side-effect-free (matching ComputeQuery/
// ComputeStatus's own precedent in this package): it never writes anything,
// which is exactly what makes it safe to call twice -- once from
// omca_propose directly, and again, unconditionally, from the start of
// omca_stage's own ComputeStage (the CAS-style full re-validation the
// round-4 audit requires, mirroring runtime.Activate's freshSourceDigest
// re-check pattern).
func ComputePropose(pc ProposeContext, rp domain.RepairProposal) (ProposeResult, error) {
	// Gate 1a: risk (PROHIBITED short-circuit).
	if rp.Spec.Confirmation == domain.RepairProhibited {
		return ProposeResult{}, &ProposeRejectedError{Gate: "risk", Reason: "spec.confirmation is PROHIBITED -- e.g. importing a native credential file is prohibited by default (docs/product/requirements.md §7) and can never be submitted, staged, or otherwise acted on"}
	}

	// Gate 1b: schema.
	if err := domain.ValidateRepairProposal(rp); err != nil {
		return ProposeResult{}, &ProposeRejectedError{Gate: "schema", Reason: err.Error()}
	}
	for i, c := range rp.Spec.Changes {
		if err := validateChangeShape(c); err != nil {
			return ProposeResult{}, &ProposeRejectedError{Gate: "schema", Reason: fmt.Sprintf("spec.changes[%d]: %v", i, err)}
		}
	}

	// Gate 2: fingerprint -- the proposal's reportFingerprint must match a
	// FRESH computation of pc.Artifact's own report fingerprint exactly (the
	// state moved since the report this proposal was based on was
	// generated).
	if err := domain.ValidateRepairProposalAgainstReport(rp, pc.Artifact.Report.Spec.Fingerprint); err != nil {
		return ProposeResult{}, &ProposeRejectedError{Gate: "fingerprint", Reason: err.Error()}
	}

	// Gate 3: ownership -- docs/adr/0002-ownership.md's v1 write path is
	// `managed` inside the proposer's own isolated generation; observed/
	// external/passthrough have no v1 write path at all, and patched is out
	// of v1 scope entirely. Every one of those four fails closed here,
	// regardless of what the proposal's Changes otherwise look like.
	if rp.Spec.Ownership != domain.OwnershipManaged {
		return ProposeResult{}, &ProposeRejectedError{Gate: "ownership", Reason: fmt.Sprintf("spec.ownership %q has no v1 OMCA write path (docs/adr/0002-ownership.md): only %q targets inside the proposer's own isolated generation are AUTO_STAGE-reachable in v1; observed/external/passthrough are never writable and patched is out of v1 scope", rp.Spec.Ownership, domain.OwnershipManaged)}
	}

	// Gates 4 (capability) and 5 (policy), per Activation-targeted Change.
	for i, c := range rp.Spec.Changes {
		if c.TargetKind != "Activation" {
			continue // capability/policy only meaningfully apply to an activation-shaped enable selection
		}
		if err := capabilityAndPolicyGates(pc, c); err != nil {
			if rejErr, ok := err.(*ProposeRejectedError); ok {
				rejErr.Reason = fmt.Sprintf("spec.changes[%d]: %s", i, rejErr.Reason)
				return ProposeResult{}, rejErr
			}
			return ProposeResult{}, &ProposeRejectedError{Gate: "schema", Reason: fmt.Sprintf("spec.changes[%d]: %v", i, err)}
		}
	}

	// Gate 6: risk (classification half) -- never rejects (see doc comment
	// above); it closes every Change into domain.RepairConfirmation and
	// reports the most restrictive one across the whole proposal.
	overall := domain.RepairAutoStage
	var explanations []string
	for i, c := range rp.Spec.Changes {
		confirmation, explanation, err := classifyChange(c)
		if err != nil {
			return ProposeResult{}, &ProposeRejectedError{Gate: "schema", Reason: fmt.Sprintf("spec.changes[%d]: %v", i, err)}
		}
		overall = mostRestrictive(overall, confirmation)
		explanations = append(explanations, explanation)
	}

	return ProposeResult{
		Accepted:     true,
		Confirmation: overall,
		Explanation:  joinExplanations(explanations),
	}, nil
}

func joinExplanations(explanations []string) string {
	sort.Strings(explanations) // deterministic across identical Change sets regardless of any map-iteration-derived ordering upstream
	out := ""
	for i, e := range explanations {
		if i > 0 {
			out += " | "
		}
		out += e
	}
	return out
}

// proposeToolDescription is what tools/list reports for omca_propose --
// deliberately small, matching this package's existing "tool schemas and
// default responses remain deliberately small" standard (statusToolDescription/
// queryToolDescription's own doc comments).
const proposeToolDescription = "Validate a RepairProposal document against the bound worktree's current report fingerprint, schema, capability gates, ownership, and policy, and classify its risk into AUTO_STAGE/CONFIRM_REQUIRED/REVIEWABLE_DIFF/PROHIBITED (docs/product/requirements.md §7). Never writes anything -- validation and classification only. A rejected proposal is returned as a tool-level error naming exactly which gate failed."

// proposeInputSchema is omca_propose's tools/list inputSchema: one required
// "proposal" object carrying the full RepairProposal document (never a bare
// ID reference -- see ProposeArguments' own doc comment). The nested
// spec/changes/patch shape is intentionally left loose (Patch is
// necessarily open-ended, matching domain.RepairChange.Patch's own
// map[string]any -- see decodePatch for where the REAL structural
// enforcement happens, against the frozen domain schemas, not this
// discovery-time schema) -- MCP inputSchema is a discovery aid for a
// client, not this tool's actual validation, which is ComputePropose's job.
func proposeInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"proposal": map[string]any{
				"type":        "object",
				"description": "A full RepairProposal document (apiVersion, kind, metadata.id, spec{reportFingerprint, author, rationale, ownership, changes, confirmation}) -- see docs/architecture/reporting.md §11.3 and internal/domain/repairproposal.go.",
			},
		},
		"required":             []string{"proposal"},
		"additionalProperties": false,
	}
}

// proposeToolHandler adapts a ProposeContext-returning pair of callbacks
// into a toolHandler: decode arguments, resolve FRESH context (never
// cached -- ArtifactFunc/CapabilityFunc's own doc comments), then answer
// via the pure ComputePropose. A *ProposeRejectedError becomes a tool-level
// error (IsError:true via handleToolsCall's shared error path) exactly like
// every other handler-returned error in this package -- a client can
// recover from a rejected proposal without treating the whole JSON-RPC
// exchange as broken.
func proposeToolHandler(artifactFn ArtifactFunc, capabilityFn CapabilityFunc) toolHandler {
	return func(arguments json.RawMessage) (any, error) {
		var args ProposeArguments
		if len(arguments) > 0 {
			if err := json.Unmarshal(arguments, &args); err != nil {
				return nil, fmt.Errorf("mcp: omca_propose: invalid arguments: %w", err)
			}
		}
		artifact, err := artifactFn()
		if err != nil {
			return nil, fmt.Errorf("mcp: omca_propose: computing report: %w", err)
		}
		return ComputePropose(ProposeContext{Artifact: artifact, CapabilityFor: capabilityFn}, args.Proposal)
	}
}

// ProposeToolEntry builds the registered omca_propose ToolEntry -- artifactFn
// supplies a fresh report.Artifact and capabilityFn a fresh per-(host,
// concept) capability lookup, both called fresh on every "tools/call"
// (never once at server startup).
func ProposeToolEntry(artifactFn ArtifactFunc, capabilityFn CapabilityFunc) ToolEntry {
	return ToolEntry{
		definition: toolDefinition{
			Name:        toolNamePropose,
			Description: proposeToolDescription,
			InputSchema: proposeInputSchema(),
		},
		handler: proposeToolHandler(artifactFn, capabilityFn),
	}
}
