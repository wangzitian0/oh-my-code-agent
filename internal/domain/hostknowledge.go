package domain

import "fmt"

// HostKnowledgeMetadata is a Knowledge Pack's identity and lifecycle facts
// (docs/knowledge/README.md §4, Knowledge Pack Contract).
type HostKnowledgeMetadata struct {
	ID           string          `json:"id"`
	Host         string          `json:"host"`
	Surface      string          `json:"surface"`
	VersionRange string          `json:"versionRange"`
	Platforms    []string        `json:"platforms,omitempty"`
	ObservedAt   string          `json:"observedAt,omitempty"`
	RecheckAfter string          `json:"recheckAfter,omitempty"`
	Status       KnowledgeStatus `json:"status"`
}

// KnowledgeEvidenceRef is one primary-evidence citation for a Knowledge Pack.
type KnowledgeEvidenceRef struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	URL    string `json:"url,omitempty"`
	Path   string `json:"path,omitempty"`
	Digest string `json:"digest,omitempty"`
}

// CapabilityOps is the set of operation-level capability relations for one
// concept, plus the independent reconcile mode (docs/knowledge/README.md
// §5, Capability Vocabulary).
type CapabilityOps struct {
	Discover            CapabilityLevel `json:"discover,omitempty"`
	Parse               CapabilityLevel `json:"parse,omitempty"`
	Normalize           CapabilityLevel `json:"normalize,omitempty"`
	Resolve             CapabilityLevel `json:"resolve,omitempty"`
	Compile             CapabilityLevel `json:"compile,omitempty"`
	Verify              CapabilityLevel `json:"verify,omitempty"`
	ReconcileMode       string          `json:"reconcileMode,omitempty"`
	VerificationMethods []string        `json:"verificationMethods,omitempty"`
}

// PrecedenceProgram names the merge program a Knowledge Pack proves for one
// concept (docs/ontology/README.md §7, Adapter Record Contract "precedence").
type PrecedenceProgram struct {
	ID       string `json:"id"`
	Identity string `json:"identity,omitempty"`
	Operator string `json:"operator"`
	Fixture  string `json:"fixture,omitempty"`
}

// HostKnowledge is an immutable Knowledge Pack: changing facts about one
// host surface and version range (docs/knowledge/README.md §1, §4).
type HostKnowledge struct {
	APIVersion         string                   `json:"apiVersion"`
	Kind               string                   `json:"kind"`
	Metadata           HostKnowledgeMetadata    `json:"metadata"`
	Evidence           []KnowledgeEvidenceRef   `json:"evidence"`
	Capabilities       map[string]CapabilityOps `json:"capabilities"`
	PrecedencePrograms []PrecedenceProgram      `json:"precedencePrograms,omitempty"`
	KnownUnknowns      []string                 `json:"knownUnknowns,omitempty"`
}

// ValidateHostKnowledge validates a HostKnowledge document's apiVersion,
// kind, required metadata, lifecycle status, and any capability levels it
// declares. It does not re-run qualification fixtures; it only rejects a
// structurally malformed or closed-enum-violating Pack.
func ValidateHostKnowledge(hk HostKnowledge) error {
	if err := ValidateAPIVersion("HostKnowledge", hk.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("HostKnowledge", hk.Kind); err != nil {
		return err
	}
	if hk.Metadata.ID == "" {
		return fmt.Errorf("HostKnowledge: metadata.id is required")
	}
	if hk.Metadata.Host == "" {
		return fmt.Errorf("HostKnowledge: metadata.host is required")
	}
	if err := ValidateHostID(hk.Metadata.Host); err != nil {
		return fmt.Errorf("HostKnowledge: metadata.host: %w", err)
	}
	if hk.Metadata.Surface == "" {
		return fmt.Errorf("HostKnowledge: metadata.surface is required")
	}
	if hk.Metadata.VersionRange == "" {
		return fmt.Errorf("HostKnowledge: metadata.versionRange is required")
	}
	if err := ValidateKnowledgeStatus(hk.Metadata.Status); err != nil {
		return fmt.Errorf("HostKnowledge: metadata.status: %w", err)
	}
	if len(hk.Evidence) == 0 {
		return fmt.Errorf("HostKnowledge: evidence must not be empty")
	}
	for _, ev := range hk.Evidence {
		if ev.ID == "" {
			return fmt.Errorf("HostKnowledge: evidence entry missing id")
		}
	}
	for concept, ops := range hk.Capabilities {
		levels := map[string]CapabilityLevel{
			"discover":  ops.Discover,
			"parse":     ops.Parse,
			"normalize": ops.Normalize,
			"resolve":   ops.Resolve,
			"compile":   ops.Compile,
			"verify":    ops.Verify,
		}
		for op, level := range levels {
			if level == "" {
				continue
			}
			if err := ValidateCapabilityLevel(level); err != nil {
				return fmt.Errorf("HostKnowledge: capabilities.%s.%s: %w", concept, op, err)
			}
		}
	}
	return nil
}
