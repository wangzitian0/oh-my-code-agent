package domain

import "fmt"

// EvidenceSubject names the claim one Evidence record backs: a field on a
// logical entity of some ontology concept.
type EvidenceSubject struct {
	Concept   string `json:"concept"`
	LogicalID string `json:"logicalId"`
	Field     string `json:"field,omitempty"`
}

// EvidenceSource is where the evidence came from.
type EvidenceSource struct {
	Kind   string `json:"kind,omitempty"`
	URL    string `json:"url,omitempty"`
	Path   string `json:"path,omitempty"`
	Digest string `json:"digest,omitempty"`
}

// KnowledgeRef pins evidence to the Knowledge Pack it was produced under.
type KnowledgeRef struct {
	ID     string `json:"id,omitempty"`
	Digest string `json:"digest,omitempty"`
}

// EvidenceSpec is the body of an Evidence document.
type EvidenceSpec struct {
	Subject      EvidenceSubject `json:"subject"`
	Level        EvidenceLevel   `json:"level"`
	Guarantee    GuaranteeLevel  `json:"guarantee,omitempty"`
	Method       string          `json:"method,omitempty"`
	ObservedAt   string          `json:"observedAt"`
	Source       EvidenceSource  `json:"source,omitempty"`
	KnowledgeRef KnowledgeRef    `json:"knowledgeRef,omitempty"`
}

// Evidence is a single evidence record backing one claim
// (docs/architecture/reporting.md §4, §5). Evidence is monotonic only when
// a higher level proves the exact same claim (same EvidenceSubject); it
// never generalizes across claims, hosts, or fields.
type Evidence struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Metadata   Metadata     `json:"metadata"`
	Spec       EvidenceSpec `json:"spec"`
}

// ValidateEvidence validates an Evidence document's apiVersion, kind,
// required metadata, subject, and closed evidence/guarantee levels.
func ValidateEvidence(e Evidence) error {
	if err := ValidateAPIVersion("Evidence", e.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("Evidence", e.Kind); err != nil {
		return err
	}
	if e.Metadata.ID == "" {
		return fmt.Errorf("Evidence: metadata.id is required")
	}
	if e.Spec.Subject.Concept == "" {
		return fmt.Errorf("Evidence: spec.subject.concept is required")
	}
	if e.Spec.Subject.LogicalID == "" {
		return fmt.Errorf("Evidence: spec.subject.logicalId is required")
	}
	if err := ValidateEvidenceLevel(e.Spec.Level); err != nil {
		return fmt.Errorf("Evidence: spec.level: %w", err)
	}
	if e.Spec.Guarantee != "" {
		if err := ValidateGuaranteeLevel(e.Spec.Guarantee); err != nil {
			return fmt.Errorf("Evidence: spec.guarantee: %w", err)
		}
	}
	if e.Spec.ObservedAt == "" {
		return fmt.Errorf("Evidence: spec.observedAt is required")
	}
	return nil
}

// sameSubject reports whether a and b name the same claim (same concept,
// logical ID, and field). Evidence levels are only ever compared within one
// subject: reporting.md §4 is explicit that a higher level for a different
// claim proves nothing about this one.
func sameSubject(a, b EvidenceSubject) bool {
	return a.Concept == b.Concept && a.LogicalID == b.LogicalID && a.Field == b.Field
}

// StrongestEvidence returns the highest-EvidenceLevel record among records
// whose Subject exactly matches subject, ignoring every record for a
// different claim. It never compares evidence across subjects, keeping the
// "evidence is monotonic only for the same claim" invariant structural
// rather than left to callers to remember.
func StrongestEvidence(subject EvidenceSubject, records []Evidence) (Evidence, bool) {
	var (
		best    Evidence
		found   bool
		bestIdx = -1
	)
	for _, r := range records {
		if !sameSubject(r.Spec.Subject, subject) {
			continue
		}
		if !r.Spec.Level.Valid() {
			continue
		}
		idx := r.Spec.Level.Rank()
		if !found || idx > bestIdx {
			best, found, bestIdx = r, true, idx
		}
	}
	return best, found
}
