package assurance

import (
	"fmt"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
)

// evidenceKind is the fixed kind literal every domain.Evidence this package
// builds declares, matching domain.ValidateEvidence's own
// ValidateKind("Evidence", ...) check.
const evidenceKind = "Evidence"

// knowledgeRefFor derives a domain.KnowledgeRef from hk, honestly empty
// (ID="", Digest="") for the zero-valued HostKnowledge a caller passes when
// the host has no qualified Knowledge Pack at all (internal/report/build.go
// only populates hk when resolution.Qualified) -- an Evidence record must
// never claim a Knowledge Pack backing it that does not exist.
func knowledgeRefFor(hk domain.HostKnowledge) domain.KnowledgeRef {
	if hk.Metadata.ID == "" {
		return domain.KnowledgeRef{}
	}
	digest, err := domain.CanonicalDigest(hk)
	if err != nil {
		// A HostKnowledge value that fails to canonically digest is not
		// this function's problem to solve or hide: the KnowledgeRef still
		// names the Pack ID (the one fact this function is sure of), just
		// without a Digest, rather than silently dropping the whole
		// reference or fabricating one that cannot be reproduced.
		return domain.KnowledgeRef{ID: hk.Metadata.ID}
	}
	return domain.KnowledgeRef{ID: hk.Metadata.ID, Digest: digest}
}

// evidenceID deterministically names one Evidence record's Metadata.ID, the
// same "host:concept:logicalId" shape domain/testdata/evidence-valid.json's
// own example ID follows.
func evidenceID(host, concept, logicalID string) string {
	return fmt.Sprintf("evidence:%s:%s:%s", host, concept, logicalID)
}

// BuildEvidence turns a [VerifyGraph]-verified EffectiveGraph into one
// domain.Evidence record per EffectiveEntry and Conflict for host, making
// "attach honest evidence to every conclusion" (issue #26's own Goal line)
// a literal, separately queryable document rather than only an implicit
// field on each entry.
//
// Call this AFTER [VerifyGraph]/[VerifyGraphWithCeilings]: BuildEvidence
// only transcribes graph's already-computed EvidenceLevel/Guarantee/Reason
// into domain.Evidence documents; it never itself clamps or upgrades
// anything, so handing it an unverified graph produces unverified Evidence.
// Every returned record validates against domain.ValidateEvidence.
func BuildEvidence(host string, graph effective.EffectiveGraph, hk domain.HostKnowledge, observedAt time.Time) []domain.Evidence {
	ref := knowledgeRefFor(hk)
	observed := observedAt.UTC().Format(time.RFC3339)
	out := make([]domain.Evidence, 0, len(graph.Entries)+len(graph.Conflicts))

	for _, e := range graph.Entries {
		out = append(out, domain.Evidence{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       evidenceKind,
			Metadata:   domain.Metadata{ID: evidenceID(host, e.Concept, e.LogicalID)},
			Spec: domain.EvidenceSpec{
				Subject:      domain.EvidenceSubject{Concept: e.Concept, LogicalID: e.LogicalID},
				Level:        e.EvidenceLevel,
				Guarantee:    e.Guarantee,
				Method:       e.Reason,
				ObservedAt:   observed,
				KnowledgeRef: ref,
			},
		})
	}

	for _, c := range graph.Conflicts {
		out = append(out, domain.Evidence{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       evidenceKind,
			Metadata:   domain.Metadata{ID: evidenceID(host, c.Concept, c.LogicalID)},
			Spec: domain.EvidenceSpec{
				// A Conflict was deliberately never resolved to one
				// winner, so it carries no Guarantee -- Guarantee answers
				// "what prevents [a specific outcome] from changing," and
				// a Conflict has no outcome yet (reporting.md §5).
				Subject:      domain.EvidenceSubject{Concept: c.Concept, LogicalID: c.LogicalID},
				Level:        c.EvidenceLevel,
				Method:       c.Reason,
				ObservedAt:   observed,
				KnowledgeRef: ref,
			},
		})
	}

	return out
}

// HostVersionEvidence builds the one E3 (HOST_REPORTED) domain.Evidence
// record this package can honestly produce today: the claim "this host
// binary is installed at this exact version," backed by det's own
// --version probe (internal/context/host.go's probeVersion -- the safe,
// non-interactive, no-network, no-model-call invocation
// docs/architecture/evidence-ceiling.md's two "host" rows document as this
// repository's only proven E3 introspection surface). It returns
// (Evidence{}, false) when det carries no confirmed version (Installed is
// false, or probeVersion recorded a non-fatal Error) -- an honest "no claim
// to make" rather than a fabricated version at a level nothing backs.
func HostVersionEvidence(det hostcontext.HostDetection, observedAt time.Time) (domain.Evidence, bool) {
	if !det.Installed || det.Version == "" || det.Error != "" {
		return domain.Evidence{}, false
	}
	return domain.Evidence{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       evidenceKind,
		Metadata:   domain.Metadata{ID: evidenceID(det.Host, HostConceptClaim, det.Host)},
		Spec: domain.EvidenceSpec{
			Subject:    domain.EvidenceSubject{Concept: HostConceptClaim, LogicalID: det.Host, Field: "version"},
			Level:      domain.EvidenceLevelHostReported,
			Method:     fmt.Sprintf("%s --version (internal/context/host.go's probeVersion; see docs/architecture/evidence-ceiling.md)", det.BinaryPath),
			ObservedAt: observedAt.UTC().Format(time.RFC3339),
		},
	}, true
}
