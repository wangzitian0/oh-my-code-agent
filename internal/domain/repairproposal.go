package domain

import "fmt"

// RepairConfirmation classifies the human-confirmation weight a
// RepairProposal requires. Unlike EvidenceLevel/GuaranteeLevel/etc., this
// is not a verbatim design-doc vocabulary: it closes the risk-based
// confirmation table in docs/product/requirements.md §7 ("Stage pending
// generation automatically" / "Always confirm" / "Produce a reviewable
// repository diff" / "Prohibited by default") into a checkable enum.
type RepairConfirmation string

const (
	// RepairAutoStage: matches an already-reviewed selection; stage
	// automatically.
	RepairAutoStage RepairConfirmation = "AUTO_STAGE"
	// RepairConfirmRequired: expands access or enables executable
	// behavior; always confirm.
	RepairConfirmRequired RepairConfirmation = "CONFIRM_REQUIRED"
	// RepairReviewableDiff: modifies a shared project/company Profile;
	// produce a reviewable repository diff.
	RepairReviewableDiff RepairConfirmation = "REVIEWABLE_DIFF"
	// RepairProhibited: e.g. importing a native credential file;
	// prohibited by default.
	RepairProhibited RepairConfirmation = "PROHIBITED"
)

// Valid reports whether c is one of the four defined confirmation classes.
func (c RepairConfirmation) Valid() bool {
	switch c {
	case RepairAutoStage, RepairConfirmRequired, RepairReviewableDiff, RepairProhibited:
		return true
	default:
		return false
	}
}

// ValidateRepairConfirmation rejects any value outside the closed
// confirmation enum.
func ValidateRepairConfirmation(c RepairConfirmation) error {
	if !c.Valid() {
		return fmt.Errorf("invalid repair confirmation %q", c)
	}
	return nil
}

// RepairAuthor identifies who or what produced a RepairProposal
// (docs/architecture/reporting.md §12, LLM Annotations).
type RepairAuthor struct {
	Kind  string `json:"kind"`
	Model string `json:"model,omitempty"`
}

// RepairChange is one targeted desired-state patch. It is never a raw
// native file edit (docs/architecture/reporting.md §11.3).
type RepairChange struct {
	TargetKind string         `json:"targetKind"`
	TargetID   string         `json:"targetId"`
	Patch      map[string]any `json:"patch"`
}

var repairChangeTargetKinds = map[string]bool{
	"Profile":    true,
	"Binding":    true,
	"Activation": true,
}

// RepairProposalSpec is the body of a RepairProposal document.
type RepairProposalSpec struct {
	ReportFingerprint     string             `json:"reportFingerprint"`
	Author                RepairAuthor       `json:"author"`
	Rationale             string             `json:"rationale,omitempty"`
	Ownership             Ownership          `json:"ownership"`
	Changes               []RepairChange     `json:"changes"`
	Confirmation          RepairConfirmation `json:"confirmation"`
	RequiredConfirmations []string           `json:"requiredConfirmations,omitempty"`
	RestartRequired       bool               `json:"restartRequired,omitempty"`
}

// RepairProposal is a schema-constrained desired-state change proposal tied
// to a Report fingerprint (docs/architecture/reporting.md §11.3; init.md
// decision 12; docs/product/requirements.md §8, FR-10; docs/project/
// roadmap.md M4).
type RepairProposal struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   Metadata           `json:"metadata"`
	Spec       RepairProposalSpec `json:"spec"`
}

// ValidateRepairProposal validates a RepairProposal document's apiVersion,
// kind, required metadata, report-fingerprint shape, author, ownership,
// changes, and confirmation class. It rejects a PROHIBITED proposal
// outright (docs/product/requirements.md §7: "Import a native credential
// file | Prohibited by default" — the answer is refusal, not merely a
// flag for a human to notice later).
func ValidateRepairProposal(rp RepairProposal) error {
	if err := ValidateAPIVersion("RepairProposal", rp.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("RepairProposal", rp.Kind); err != nil {
		return err
	}
	if rp.Metadata.ID == "" {
		return fmt.Errorf("RepairProposal: metadata.id is required")
	}
	if rp.Spec.ReportFingerprint == "" {
		return fmt.Errorf("RepairProposal: spec.reportFingerprint is required")
	}
	if !IsCanonicalDigest(rp.Spec.ReportFingerprint) {
		return fmt.Errorf("RepairProposal: spec.reportFingerprint %q is not a sha256 canonical digest", rp.Spec.ReportFingerprint)
	}
	switch rp.Spec.Author.Kind {
	case "human":
	case "llm":
		if rp.Spec.Author.Model == "" {
			return fmt.Errorf("RepairProposal: spec.author.model is required when spec.author.kind is llm")
		}
	default:
		return fmt.Errorf("RepairProposal: spec.author.kind must be \"human\" or \"llm\", got %q", rp.Spec.Author.Kind)
	}
	if err := ValidateOwnership(rp.Spec.Ownership); err != nil {
		return fmt.Errorf("RepairProposal: spec.ownership: %w", err)
	}
	if len(rp.Spec.Changes) == 0 {
		return fmt.Errorf("RepairProposal: spec.changes must not be empty")
	}
	for i, c := range rp.Spec.Changes {
		if !repairChangeTargetKinds[c.TargetKind] {
			return fmt.Errorf("RepairProposal: spec.changes[%d].targetKind %q must be Profile, Binding, or Activation", i, c.TargetKind)
		}
		if c.TargetID == "" {
			return fmt.Errorf("RepairProposal: spec.changes[%d].targetId is required", i)
		}
	}
	if err := ValidateRepairConfirmation(rp.Spec.Confirmation); err != nil {
		return fmt.Errorf("RepairProposal: spec.confirmation: %w", err)
	}
	if rp.Spec.Confirmation == RepairProhibited {
		return fmt.Errorf("RepairProposal: confirmation class PROHIBITED cannot be submitted (docs/product/requirements.md §7)")
	}
	return nil
}

// ValidateRepairProposalAgainstReport rejects a RepairProposal whose
// reportFingerprint does not match the Report it claims to repair. A stale
// or mismatched fingerprint is rejected rather than reinterpreted against
// the current report (docs/architecture/reporting.md §11.3).
func ValidateRepairProposalAgainstReport(rp RepairProposal, reportFingerprint string) error {
	if rp.Spec.ReportFingerprint != reportFingerprint {
		return fmt.Errorf("RepairProposal: spec.reportFingerprint %q does not match report fingerprint %q", rp.Spec.ReportFingerprint, reportFingerprint)
	}
	return nil
}
