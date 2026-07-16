package domain

import "fmt"

// ObservationHost identifies the detected host binary and version an
// Observation was collected from.
type ObservationHost struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
}

// ObservationSource is the physical origin of one observed record
// (docs/ontology/README.md §1.1, "source" entity).
type ObservationSource struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Digest string `json:"digest,omitempty"`
}

// ObservationScope is the subjects and locations an observed source affects
// (docs/ontology/README.md §2, Scope Model).
type ObservationScope struct {
	Kind          string `json:"kind"`
	Root          string `json:"root,omitempty"`
	Selector      string `json:"selector,omitempty"`
	Shared        bool   `json:"shared,omitempty"`
	TrustRequired bool   `json:"trustRequired,omitempty"`
}

// ObservationSpec is the body of an Observation document, shaped after the
// Adapter Record Contract (docs/ontology/README.md §7).
type ObservationSpec struct {
	Host               ObservationHost   `json:"host"`
	Surface            string            `json:"surface,omitempty"`
	Concept            string            `json:"concept"`
	Source             ObservationSource `json:"source"`
	Scope              ObservationScope  `json:"scope"`
	Disposition        SourceDisposition `json:"disposition"`
	EvidenceLevel      EvidenceLevel     `json:"evidenceLevel"`
	RawDigest          string            `json:"rawDigest,omitempty"`
	ParsedDigest       string            `json:"parsedDigest,omitempty"`
	OpaqueVendorFields map[string]any    `json:"opaqueVendorFields,omitempty"`
}

// Observation is one normalized source record inventoried by a host adapter
// (docs/architecture/README.md §5.1, Observed Graph; §9, HostAdapter.Observe
// returns an ObservationSet of these). This type carries basic
// required-field validation only: the richer Observed Graph behavior
// (identity matching, precedence, duplicate detection) belongs to the
// Normalizer and Identity Matcher components landing in later PRs
// (roadmap M3), not to this protocol-document type.
type Observation struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   Metadata        `json:"metadata"`
	Spec       ObservationSpec `json:"spec"`
}

// ValidateObservation validates an Observation document's apiVersion, kind,
// required metadata, and the closed enums its spec references.
func ValidateObservation(o Observation) error {
	if err := ValidateAPIVersion("Observation", o.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("Observation", o.Kind); err != nil {
		return err
	}
	if o.Metadata.ID == "" {
		return fmt.Errorf("Observation: metadata.id is required")
	}
	if o.Spec.Host.ID == "" {
		return fmt.Errorf("Observation: spec.host.id is required")
	}
	if err := ValidateHostID(o.Spec.Host.ID); err != nil {
		return fmt.Errorf("Observation: spec.host.id: %w", err)
	}
	if o.Spec.Concept == "" {
		return fmt.Errorf("Observation: spec.concept is required")
	}
	if o.Spec.Source.Kind == "" {
		return fmt.Errorf("Observation: spec.source.kind is required")
	}
	if o.Spec.Scope.Kind == "" {
		return fmt.Errorf("Observation: spec.scope.kind is required")
	}
	if err := ValidateScopeKind(o.Spec.Scope.Kind); err != nil {
		return fmt.Errorf("Observation: spec.scope.kind: %w", err)
	}
	if err := ValidateSourceDisposition(o.Spec.Disposition); err != nil {
		return fmt.Errorf("Observation: spec.disposition: %w", err)
	}
	if err := ValidateEvidenceLevel(o.Spec.EvidenceLevel); err != nil {
		return fmt.Errorf("Observation: spec.evidenceLevel: %w", err)
	}
	return nil
}
