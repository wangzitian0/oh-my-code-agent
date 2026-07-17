package domain

import "fmt"

// CurrentOntologyVersion is the docs/ontology/README.md vocabulary version
// this build compiles against ("Status: draft v0.3" in that document's own
// header). It is recorded on every compiled Generation (spec.ontologyVersion,
// docs/architecture/runtime.md §5.3's "Ontology version" pending-manifest
// field) so a generation is traceable to the vocabulary revision that shaped
// its Concept/Scope/Intent vocabulary, the same way BootstrapPolicyVersion
// (internal/runtime/policy.go) pins the M1 bootstrap policy's own revision.
// Bump this alongside docs/ontology/README.md's own "Status" line.
const CurrentOntologyVersion = "v0.3"

// GenerationMetadata identifies a Generation and its lineage.
type GenerationMetadata struct {
	ID       string `json:"id"`
	Worktree string `json:"worktree"`
	// Invocation is a short, free-text description of what triggered this
	// generation to be compiled (e.g. "omca run codex", "omca env"),
	// docs/architecture/runtime.md §5.3's "invocation context" half of the
	// "worktree and invocation context" pending-manifest field (the other
	// half is Worktree, above). Optional and caller-supplied: PR-09's
	// Bootstrap has no notion of invocation context and always leaves this
	// empty; a full-compilation caller (internal/runtime's Compile, PR-14)
	// may supply one. Never folded into any content-addressed generation ID
	// -- it describes *why* a generation was built, not *what* was built,
	// so two calls that would compile identical artifacts for different
	// reasons should still be recognized as the same generation.
	Invocation string  `json:"invocation,omitempty"`
	Parent     *string `json:"parent,omitempty"`
	CreatedAt  string  `json:"createdAt"`
}

// KnowledgePackRef pins a Generation to the immutable Knowledge Pack it was
// compiled against (docs/knowledge/README.md §11, Runtime Resolution).
type KnowledgePackRef struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// GenerationArtifact is one compiled file inside a Generation's per-host
// artifact tree.
type GenerationArtifact struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// GenerationHostEntry is one host's compiled artifact tree within a
// Generation (docs/architecture/README.md §5.4).
type GenerationHostEntry struct {
	Surface        string               `json:"surface,omitempty"`
	AdapterID      string               `json:"adapterId"`
	AdapterVersion string               `json:"adapterVersion,omitempty"`
	Ownership      Ownership            `json:"ownership"`
	Artifacts      []GenerationArtifact `json:"artifacts,omitempty"`
}

// GenerationSourceEntry records one included-or-excluded source decision a
// compiler made when building a Generation, and why
// (docs/architecture/runtime.md §12 invariant "native exclusions are
// explained rather than hidden"; issue #13/PR-09 AC "The manifest lists
// every included and excluded source with a reason"). It is named Sources,
// not Exclusions (the round-2 design note that first proposed this field
// sketched it as an exclusions-only list): AC #3's own text requires a
// reason for every *included* source too, not only excluded ones, so one
// homogeneous list carrying an explicit Included flag is what the
// acceptance criterion literally asks for, rather than two parallel lists
// or an inclusions list inferred only implicitly from
// GenerationHostEntry.Artifacts.
//
// A capability-gap entry (CapabilityGap: true) need not be tied to one
// discovered Source: it can describe an entire exclusion *class* a build
// cannot yet behaviorally prove is complete (e.g. "Claude Code user-global
// MCP/Skill exclusion under CLAUDE_CONFIG_DIR relocation was only
// statically inspected, never behavior-probed" -- see
// knowledge/hosts/claude-code/cli/2.1/manifest.json's knownUnknowns). Per
// this project's stated policy ("capability-gap shipping is allowed, hiding
// is not", issue #13 round-2 audit), CapabilityGap must never be true
// without a non-empty TrackingIssue -- ValidateGeneration enforces this
// below, the same "fail closed on an unexplained gap" stance
// domain.IsCanonicalDigest's doc comment describes for a malformed digest
// reference.
type GenerationSourceEntry struct {
	Concept       string `json:"concept"`
	Source        string `json:"source,omitempty"`
	Scope         string `json:"scope,omitempty"`
	Included      bool   `json:"included"`
	Reason        string `json:"reason"`
	CapabilityGap bool   `json:"capabilityGap,omitempty"`
	TrackingIssue string `json:"trackingIssue,omitempty"`
}

// ProfileRef names one Profile that fed a Generation's Desired Graph, pinned
// by the digest of the exact Profile document content used, not just its
// logical ID (a Profile document can change content across Generations while
// keeping the same metadata.id).
type ProfileRef struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// DesiredStateRef names the exact Profile/Activation/Exception documents a
// Generation's spec.desiredGraphDigest was computed from --
// docs/architecture/runtime.md §5.3's "selected Profiles and Activation"
// pending-manifest field. It is nil on a bootstrap generation (PR-09's
// Bootstrap has no real Desired Graph to name -- see doc.go's "why
// desiredGraphDigest is a bootstrap-policy digest" section) and populated by
// a full-compilation generation (PR-14's Compile), which does resolve a real
// one (internal/resolve.Resolve's inputs).
type DesiredStateRef struct {
	Profiles []ProfileRef `json:"profiles,omitempty"`
	// Activation is the digest of the Activation document applied, or empty
	// if none was supplied (a Desired Graph with Profiles but no local
	// activation overrides is valid).
	Activation string `json:"activation,omitempty"`
	// Exceptions are the digests of every Exception considered, sorted for
	// determinism.
	Exceptions []string `json:"exceptions,omitempty"`
}

// GenerationDiffSummary is reserved for docs/architecture/runtime.md §5.3's
// "native/current/pending diff" pending-manifest field. Neither PR-09's
// Bootstrap nor PR-14's Compile populates it: a real native/current/pending
// diff needs to re-observe native state and compare against the worktree's
// current-generation pointer at the moment a diff is requested, which is
// exactly the Activation transaction's job (PR-15, "Activation transaction:
// CAS, atomic switch, rollback" -- docs/architecture/runtime.md §5.4). The
// field exists now, always nil/omitted, so PR-15 extends an already-settled
// schema shape instead of making another additive schema change to add it.
type GenerationDiffSummary struct {
	NativeChanged  []string `json:"nativeChanged,omitempty"`
	CurrentChanged []string `json:"currentChanged,omitempty"`
}

// ExpectedEvidenceEntry names, for one host, the EvidenceLevel and
// GuaranteeLevel Activation's post-launch verification step
// (docs/architecture/runtime.md §5.4's "-> verify") is expected to confirm
// once this generation activates -- §5.3's "expected evidence and guarantee"
// pending-manifest field. Reserved and always left empty by both Bootstrap
// and Compile in this PR: a real expectation requires the Verify step and
// its evidence taxonomy, which is PR-22 scope (Evidence E0-E3). Evidence/
// Guarantee are validated when non-empty so a future populated value can't
// silently be a typo'd level.
type ExpectedEvidenceEntry struct {
	Host      string         `json:"host"`
	Evidence  EvidenceLevel  `json:"evidence,omitempty"`
	Guarantee GuaranteeLevel `json:"guarantee,omitempty"`
}

// GenerationSpec is the body of a Generation document.
type GenerationSpec struct {
	DesiredGraphDigest string                         `json:"desiredGraphDigest"`
	KnowledgePacks     []KnowledgePackRef             `json:"knowledgePacks"`
	Hosts              map[string]GenerationHostEntry `json:"hosts"`
	// Sources is additive, optional M0-era protocol evolution (PR-09,
	// issue #13): schemas/protocol/*.schema.json are v1alpha1, and this
	// field was added without touching any existing required field, so
	// every already-merged caller of domain.Generation keeps working
	// unchanged. See internal/runtime/doc.go for the full "why extend
	// Generation instead of a separate manifest type" reasoning.
	Sources []GenerationSourceEntry `json:"sources,omitempty"`
	Status  string                  `json:"status,omitempty"`

	// The fields below are PR-14 (issue #18) additions, matching PR-09's own
	// additive-evolution precedent: every one is optional, so no
	// already-merged caller of domain.Generation is affected. Together they
	// close the gap between the original field list (desiredGraphDigest,
	// knowledgePacks, hosts, sources, status) and every remaining line of
	// docs/architecture/runtime.md §5.3's pending-manifest field list --
	// "generation ID" and "parent generation ID" were already on
	// GenerationMetadata; "worktree and invocation context" is
	// GenerationMetadata.Worktree plus the new GenerationMetadata.Invocation
	// above; "Knowledge Pack IDs and digests" is the existing KnowledgePacks,
	// left empty by Bootstrap but populated by Compile; "host artifacts and
	// ownership" is the existing Hosts. What follows are the genuinely new
	// fields: OntologyVersion, SourceDigest, and DesiredState round out
	// "source and desired-state digests" and "selected Profiles and
	// Activation"; Diff, RiskConfirmations, and ExpectedEvidence are honest,
	// always-empty-in-this-PR placeholders for the three pending-manifest
	// lines that depend on machinery this PR does not build (Activation's
	// diff/verify transaction is PR-15; a real risk-confirmation flow has no
	// source yet either) -- see each field's own doc comment.

	// OntologyVersion pins the docs/ontology/README.md vocabulary revision
	// this generation's Concept/Scope/Intent values were compiled under
	// (CurrentOntologyVersion, above).
	OntologyVersion string `json:"ontologyVersion,omitempty"`

	// SourceDigest is a single canonical digest over every entry in Sources
	// (sorted, so map/slice construction order never matters) -- the
	// "source... digest" half of §5.3's "source and desired-state digests"
	// line (DesiredGraphDigest is the other half). Activation's "ensure
	// source digests still match" step (§5.4) is meant to recompute this
	// same digest from a fresh observation pass and compare; that
	// recomputation belongs to the Activation transaction (PR-15), not to
	// this compiler, which only ever records the digest of what it itself
	// observed at compile time.
	SourceDigest string `json:"sourceDigest,omitempty"`

	// DesiredState names the exact Profile/Activation/Exception documents
	// DesiredGraphDigest was computed from. See DesiredStateRef.
	DesiredState *DesiredStateRef `json:"desiredState,omitempty"`

	// Diff is reserved for §5.3's "native/current/pending diff" line. See
	// GenerationDiffSummary.
	Diff *GenerationDiffSummary `json:"diff,omitempty"`

	// RiskConfirmations is reserved for §5.3's "risk confirmations" line: a
	// human- or policy-level acknowledgement that a specific risky change
	// (e.g. a permission loosening) was reviewed before activation. Neither
	// Bootstrap nor Compile populates this in this PR -- no risk-
	// confirmation workflow exists yet to source real values from; inventing
	// placeholder content here would misrepresent an unreviewed generation
	// as reviewed. Always empty/omitted until that workflow exists.
	RiskConfirmations []string `json:"riskConfirmations,omitempty"`

	// ExpectedEvidence is reserved for §5.3's "expected evidence and
	// guarantee" line. See ExpectedEvidenceEntry.
	ExpectedEvidence []ExpectedEvidenceEntry `json:"expectedEvidence,omitempty"`
}

// Generation maps the Desired Graph to native artifacts in one immutable
// runtime (docs/architecture/README.md §5.4, §8). Compilation, activation,
// and rollback behavior belong to the Runtime Compiler and Reconciler
// landing in M2; this type carries basic required-field validation only.
type Generation struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   GenerationMetadata `json:"metadata"`
	Spec       GenerationSpec     `json:"spec"`
}

// ValidateGeneration validates a Generation document's apiVersion, kind,
// required metadata, digest shapes, and per-host ownership.
func ValidateGeneration(g Generation) error {
	if err := ValidateAPIVersion("Generation", g.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("Generation", g.Kind); err != nil {
		return err
	}
	if g.Metadata.ID == "" {
		return fmt.Errorf("Generation: metadata.id is required")
	}
	if g.Metadata.Worktree == "" {
		return fmt.Errorf("Generation: metadata.worktree is required")
	}
	if g.Metadata.CreatedAt == "" {
		return fmt.Errorf("Generation: metadata.createdAt is required")
	}
	if g.Spec.DesiredGraphDigest == "" {
		return fmt.Errorf("Generation: spec.desiredGraphDigest is required")
	}
	if !IsCanonicalDigest(g.Spec.DesiredGraphDigest) {
		return fmt.Errorf("Generation: spec.desiredGraphDigest %q is not a sha256 canonical digest", g.Spec.DesiredGraphDigest)
	}
	for i, kp := range g.Spec.KnowledgePacks {
		if kp.ID == "" {
			return fmt.Errorf("Generation: spec.knowledgePacks[%d]: id is required", i)
		}
		if !IsCanonicalDigest(kp.Digest) {
			return fmt.Errorf("Generation: spec.knowledgePacks[%d]: digest %q is not a sha256 canonical digest", i, kp.Digest)
		}
	}
	for host, entry := range g.Spec.Hosts {
		if err := ValidateHostID(host); err != nil {
			return fmt.Errorf("Generation: spec.hosts: %w", err)
		}
		if entry.AdapterID == "" {
			return fmt.Errorf("Generation: spec.hosts[%s]: adapterId is required", host)
		}
		if err := ValidateOwnership(entry.Ownership); err != nil {
			return fmt.Errorf("Generation: spec.hosts[%s]: %w", host, err)
		}
	}
	for i, s := range g.Spec.Sources {
		if s.Concept == "" {
			return fmt.Errorf("Generation: spec.sources[%d]: concept is required", i)
		}
		if s.Reason == "" {
			return fmt.Errorf("Generation: spec.sources[%d]: reason is required", i)
		}
		if s.CapabilityGap && s.TrackingIssue == "" {
			return fmt.Errorf("Generation: spec.sources[%d]: capabilityGap is true but trackingIssue is empty -- capability-gap shipping is allowed, hiding is not (issue #13 round-2 audit)", i)
		}
	}
	if g.Spec.DesiredState != nil {
		for i, p := range g.Spec.DesiredState.Profiles {
			if p.ID == "" {
				return fmt.Errorf("Generation: spec.desiredState.profiles[%d]: id is required", i)
			}
			if !IsCanonicalDigest(p.Digest) {
				return fmt.Errorf("Generation: spec.desiredState.profiles[%d]: digest %q is not a sha256 canonical digest", i, p.Digest)
			}
		}
		if g.Spec.DesiredState.Activation != "" && !IsCanonicalDigest(g.Spec.DesiredState.Activation) {
			return fmt.Errorf("Generation: spec.desiredState.activation %q is not a sha256 canonical digest", g.Spec.DesiredState.Activation)
		}
		for i, d := range g.Spec.DesiredState.Exceptions {
			if !IsCanonicalDigest(d) {
				return fmt.Errorf("Generation: spec.desiredState.exceptions[%d]: %q is not a sha256 canonical digest", i, d)
			}
		}
	}
	if g.Spec.SourceDigest != "" && !IsCanonicalDigest(g.Spec.SourceDigest) {
		return fmt.Errorf("Generation: spec.sourceDigest %q is not a sha256 canonical digest", g.Spec.SourceDigest)
	}
	for i, e := range g.Spec.ExpectedEvidence {
		if e.Host == "" {
			return fmt.Errorf("Generation: spec.expectedEvidence[%d]: host is required", i)
		}
		if err := ValidateHostID(e.Host); err != nil {
			return fmt.Errorf("Generation: spec.expectedEvidence[%d]: %w", i, err)
		}
		if e.Evidence != "" {
			if err := ValidateEvidenceLevel(e.Evidence); err != nil {
				return fmt.Errorf("Generation: spec.expectedEvidence[%d]: %w", i, err)
			}
		}
		if e.Guarantee != "" {
			if err := ValidateGuaranteeLevel(e.Guarantee); err != nil {
				return fmt.Errorf("Generation: spec.expectedEvidence[%d]: %w", i, err)
			}
		}
	}
	return nil
}
