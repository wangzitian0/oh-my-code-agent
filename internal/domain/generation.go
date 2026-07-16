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

// GenerationSpec is the body of a Generation document.
type GenerationSpec struct {
	DesiredGraphDigest string                         `json:"desiredGraphDigest"`
	KnowledgePacks     []KnowledgePackRef             `json:"knowledgePacks"`
	Hosts              map[string]GenerationHostEntry `json:"hosts"`
	Status             string                         `json:"status,omitempty"`
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
	return nil
}
