package domain

import "fmt"

// KnowledgeCandidateMetadata carries a Knowledge Candidate's identity, the
// host/surface it concerns, and when it was collected. "Generated candidates
// identify automation and collection time" (docs/knowledge/README.md §12),
// so Automation is required alongside the usual id/host/surface facts.
type KnowledgeCandidateMetadata struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Surface     string `json:"surface"`
	CollectedAt string `json:"collectedAt"`
	// Automation names the tool or process that produced this candidate
	// (e.g. "omca knowledge poll"), never a human author -- a Knowledge
	// Candidate is always automation-generated input to a human-reviewed
	// pull request (ADR-0004 decision 4), never itself a reviewed
	// conclusion.
	Automation string `json:"automation"`
}

// ChangedSource is one upstream source a poll detected a digest mismatch
// for: docs/knowledge/README.md §9's "changed upstream sources and
// digests". OldDigest is empty when the currently published Pack recorded
// no baseline digest for this source yet (nothing to diff against, not
// itself evidence of a change).
type ChangedSource struct {
	SourceID  string `json:"sourceId"`
	Kind      string `json:"kind,omitempty"`
	URL       string `json:"url,omitempty"`
	OldDigest string `json:"oldDigest,omitempty"`
	NewDigest string `json:"newDigest"`
}

// VersionRangeChange is docs/knowledge/README.md §9's "old and new host
// version range". New is deliberately allowed to be empty: determining the
// correct new versionRange for a changed source is a maintainer
// qualification decision (ADR-0004 decision 4), not something a Poller can
// safely infer from a content digest mismatch alone -- an empty New is
// recorded honestly as a known unknown (see KnowledgeCandidateSpec.
// NewKnownUnknowns) rather than guessed.
type VersionRangeChange struct {
	Old string `json:"old,omitempty"`
	New string `json:"new,omitempty"`
}

// AffectedCapability names one concept+operation whose capability level the
// changed source(s) could affect -- docs/knowledge/README.md §9's "affected
// concepts and operations". Old is the level the currently published Pack
// declares today; New is left empty in this PR (determining a new level
// requires the qualification fixtures issue #33/PR-29 is responsible for
// running automatically, not a Poller's own job).
type AffectedCapability struct {
	Concept   string          `json:"concept"`
	Operation string          `json:"operation"`
	Old       CapabilityLevel `json:"old,omitempty"`
	New       CapabilityLevel `json:"new,omitempty"`
}

// FixtureResult is one qualification fixture's outcome against the
// candidate change -- docs/knowledge/README.md §9's "fixture results". This
// PR does not run qualification fixtures automatically (issue #33/PR-29's
// explicit job); StatusNotRun documents that honestly rather than a blank
// or fabricated PASS.
type FixtureResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Qualification fixture result statuses a KnowledgeCandidate's
// FixtureResults may report. StatusNotRun is this PR's own honest default:
// no automated qualification run backs a candidate yet.
const (
	FixtureResultPass   = "PASS"
	FixtureResultFail   = "FAIL"
	FixtureResultNotRun = "NOT_RUN"
)

// WriteCapabilityImpact names one concept whose write capability a
// candidate would block or expand -- docs/knowledge/README.md §9's "write
// capabilities that would be blocked or expanded". Change is one of
// WriteCapabilityBlocked/WriteCapabilityExpanded.
type WriteCapabilityImpact struct {
	Concept string `json:"concept"`
	Change  string `json:"change"`
	Reason  string `json:"reason,omitempty"`
}

// Write capability impact classifications a KnowledgeCandidate may report.
const (
	WriteCapabilityBlocked  = "BLOCKED"
	WriteCapabilityExpanded = "EXPANDED"
)

// KnowledgeCandidateSpec is the body of a Knowledge Candidate document: the
// full field list docs/knowledge/README.md §9 requires, verbatim --
// "changed upstream sources and digests; old and new host version range;
// changed discovery roots; changed precedence or merge behavior; affected
// concepts and operations; fixture results; generations that would become
// stale; write capabilities that would be blocked or expanded; new known
// unknowns; required adapter code changes."
type KnowledgeCandidateSpec struct {
	ChangedSources         []ChangedSource         `json:"changedSources"`
	VersionRange           VersionRangeChange      `json:"versionRange"`
	ChangedDiscoveryRoots  []string                `json:"changedDiscoveryRoots,omitempty"`
	ChangedPrecedence      []string                `json:"changedPrecedence,omitempty"`
	AffectedCapabilities   []AffectedCapability    `json:"affectedCapabilities,omitempty"`
	FixtureResults         []FixtureResult         `json:"fixtureResults,omitempty"`
	StaleGenerations       []string                `json:"staleGenerations,omitempty"`
	WriteCapabilityImpacts []WriteCapabilityImpact `json:"writeCapabilityImpacts,omitempty"`
	NewKnownUnknowns       []string                `json:"newKnownUnknowns,omitempty"`
	RequiredAdapterChanges []string                `json:"requiredAdapterChanges,omitempty"`
}

// KnowledgeCandidate is the automation-produced report ADR-0004 decision 4's
// update workflow starts from: "poll allowlisted official sources, detect a
// change, create a Knowledge Candidate, diff facts and affected
// capabilities, run qualification fixtures, open a PR, require maintainer
// review, then publish the immutable Pack." A KnowledgeCandidate is never
// itself a Pack, never merged automatically, and never promotes a
// capability level on its own -- it is the human-reviewable evidence a
// maintainer uses to decide whether to do so.
type KnowledgeCandidate struct {
	APIVersion string                     `json:"apiVersion"`
	Kind       string                     `json:"kind"`
	Metadata   KnowledgeCandidateMetadata `json:"metadata"`
	Spec       KnowledgeCandidateSpec     `json:"spec"`
}

// ValidateKnowledgeCandidate validates a KnowledgeCandidate document's
// apiVersion, kind, required metadata, and closed-enum fields. It does not
// evaluate whether the reported diff is itself correct -- only that the
// document is structurally sound.
func ValidateKnowledgeCandidate(kc KnowledgeCandidate) error {
	if err := ValidateAPIVersion("KnowledgeCandidate", kc.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("KnowledgeCandidate", kc.Kind); err != nil {
		return err
	}
	if kc.Metadata.ID == "" {
		return fmt.Errorf("KnowledgeCandidate: metadata.id is required")
	}
	if kc.Metadata.Host == "" {
		return fmt.Errorf("KnowledgeCandidate: metadata.host is required")
	}
	if err := ValidateHostID(kc.Metadata.Host); err != nil {
		return fmt.Errorf("KnowledgeCandidate: metadata.host: %w", err)
	}
	if kc.Metadata.Surface == "" {
		return fmt.Errorf("KnowledgeCandidate: metadata.surface is required")
	}
	if kc.Metadata.CollectedAt == "" {
		return fmt.Errorf("KnowledgeCandidate: metadata.collectedAt is required")
	}
	if kc.Metadata.Automation == "" {
		return fmt.Errorf("KnowledgeCandidate: metadata.automation is required")
	}
	if len(kc.Spec.ChangedSources) == 0 {
		return fmt.Errorf("KnowledgeCandidate: spec.changedSources must not be empty -- a candidate with no changed source is not a candidate")
	}
	for i, cs := range kc.Spec.ChangedSources {
		if cs.SourceID == "" {
			return fmt.Errorf("KnowledgeCandidate: spec.changedSources[%d]: sourceId is required", i)
		}
		if cs.NewDigest == "" {
			return fmt.Errorf("KnowledgeCandidate: spec.changedSources[%d]: newDigest is required", i)
		}
	}
	for i, fr := range kc.Spec.FixtureResults {
		switch fr.Status {
		case FixtureResultPass, FixtureResultFail, FixtureResultNotRun:
		default:
			return fmt.Errorf("KnowledgeCandidate: spec.fixtureResults[%d]: invalid status %q", i, fr.Status)
		}
	}
	for i, wc := range kc.Spec.WriteCapabilityImpacts {
		switch wc.Change {
		case WriteCapabilityBlocked, WriteCapabilityExpanded:
		default:
			return fmt.Errorf("KnowledgeCandidate: spec.writeCapabilityImpacts[%d]: invalid change %q", i, wc.Change)
		}
	}
	return nil
}
