package domain

import "fmt"

// ReportMetadata carries a Report's identity and generation time.
type ReportMetadata struct {
	ID          string `json:"id"`
	Worktree    string `json:"worktree"`
	GeneratedAt string `json:"generatedAt"`
}

// ReportPlanes are summary counts for the reported state planes
// (docs/architecture/reporting.md §2). Full plane content is queried on
// demand (omca_query); a Report embeds counts only.
type ReportPlanes struct {
	Native        int `json:"native,omitempty"`
	Observed      int `json:"observed,omitempty"`
	Desired       int `json:"desired,omitempty"`
	Current       int `json:"current,omitempty"`
	Pending       int `json:"pending,omitempty"`
	HostEffective int `json:"hostEffective,omitempty"`
}

// DriftAssertion is one machine-level Drift record
// (docs/architecture/reporting.md §6, Drift Model).
type DriftAssertion struct {
	EntityID      string         `json:"entityId"`
	Field         string         `json:"field"`
	Category      DriftCategory  `json:"category"`
	Expected      any            `json:"expected,omitempty"`
	Observed      any            `json:"observed,omitempty"`
	RootCause     string         `json:"rootCause"`
	Remediation   string         `json:"remediation,omitempty"`
	ContextCell   string         `json:"contextCell,omitempty"`
	EvidenceLevel EvidenceLevel  `json:"evidenceLevel,omitempty"`
	Guarantee     GuaranteeLevel `json:"guarantee,omitempty"`
}

// ReportSpec is the body of a Report document.
type ReportSpec struct {
	Fingerprint     string                     `json:"fingerprint"`
	Planes          ReportPlanes               `json:"planes,omitempty"`
	Drift           []DriftAssertion           `json:"drift"`
	KnowledgeStatus map[string]KnowledgeStatus `json:"knowledgeStatus,omitempty"`
}

// Report is the immutable artifact human, JSON, TUI, and MCP projections all
// derive from (docs/architecture/reporting.md). Deeper behavior — root-cause
// grouping, representative sampling, Explain traces — belongs to the Drift
// Engine and Report projector landing in M3, not to this protocol-document
// type; this carries basic required-field and closed-enum validation only.
type Report struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   ReportMetadata `json:"metadata"`
	Spec       ReportSpec     `json:"spec"`
}

// ValidateReport validates a Report document's apiVersion, kind, required
// metadata, fingerprint shape, and every drift assertion's closed enums.
func ValidateReport(r Report) error {
	if err := ValidateAPIVersion("Report", r.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("Report", r.Kind); err != nil {
		return err
	}
	if r.Metadata.ID == "" {
		return fmt.Errorf("Report: metadata.id is required")
	}
	if r.Metadata.Worktree == "" {
		return fmt.Errorf("Report: metadata.worktree is required")
	}
	if r.Metadata.GeneratedAt == "" {
		return fmt.Errorf("Report: metadata.generatedAt is required")
	}
	if r.Spec.Fingerprint == "" {
		return fmt.Errorf("Report: spec.fingerprint is required")
	}
	if !IsCanonicalDigest(r.Spec.Fingerprint) {
		return fmt.Errorf("Report: spec.fingerprint %q is not a sha256 canonical digest", r.Spec.Fingerprint)
	}
	for i, d := range r.Spec.Drift {
		if d.EntityID == "" {
			return fmt.Errorf("Report: spec.drift[%d]: entityId is required", i)
		}
		if d.Field == "" {
			return fmt.Errorf("Report: spec.drift[%d]: field is required", i)
		}
		if d.RootCause == "" {
			return fmt.Errorf("Report: spec.drift[%d]: rootCause is required", i)
		}
		if err := ValidateDriftCategory(d.Category); err != nil {
			return fmt.Errorf("Report: spec.drift[%d]: %w", i, err)
		}
		if d.EvidenceLevel != "" {
			if err := ValidateEvidenceLevel(d.EvidenceLevel); err != nil {
				return fmt.Errorf("Report: spec.drift[%d]: %w", i, err)
			}
		}
		if d.Guarantee != "" {
			if err := ValidateGuaranteeLevel(d.Guarantee); err != nil {
				return fmt.Errorf("Report: spec.drift[%d]: %w", i, err)
			}
		}
	}
	for host, status := range r.Spec.KnowledgeStatus {
		if err := ValidateHostID(host); err != nil {
			return fmt.Errorf("Report: spec.knowledgeStatus: %w", err)
		}
		if err := ValidateKnowledgeStatus(status); err != nil {
			return fmt.Errorf("Report: spec.knowledgeStatus[%s]: %w", host, err)
		}
	}
	return nil
}
