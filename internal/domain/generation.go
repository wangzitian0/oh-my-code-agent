package domain

import "fmt"

// GenerationMetadata identifies a Generation and its lineage.
type GenerationMetadata struct {
	ID        string  `json:"id"`
	Worktree  string  `json:"worktree"`
	Parent    *string `json:"parent,omitempty"`
	CreatedAt string  `json:"createdAt"`
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
	return nil
}
