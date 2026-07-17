package report

import (
	"fmt"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/drift"
)

// cardIDPrefix mirrors docs/architecture/reporting.md §7's worked example ID
// shape ("DR-017"), read as "Drift Report" — a short, greppable label
// distinct from the sha256 digest that actually gives it stability.
const cardIDPrefix = "DR-"

// cardIDDigestLength is how many hex characters of the content digest
// become the visible ID suffix: long enough that two genuinely different
// cards essentially never collide in one report (8 hex chars is 4 billion
// buckets), short enough to stay a usable CLI argument a human can read off
// `omca drift`'s list output and paste into `omca drift show <id>`.
const cardIDDigestLength = 8

// computeCardID assigns card a stable, content-addressed ID: sha256 over
// exactly the fields drift.GroupWithSampleLimit's own groupKey groups by
// (root cause, remediation, category, adapter version — see
// internal/drift/group.go's groupKey), truncated to cardIDDigestLength hex
// characters. Grouping key content, not Matrix/Samples/Impact, is
// deliberately all that feeds the digest: two `omca report` runs against an
// unchanged root cause produce the same ID even if impact counts shift (one
// more host observed, one fewer artifact affected), which is exactly what
// makes `omca drift show <id>` stable enough to reference across two
// invocations rather than an index into a slice that reorders as the
// underlying signal set changes.
func computeCardID(card drift.ActionCard) (string, error) {
	key := struct {
		RootCause      string               `json:"rootCause"`
		Remediation    string               `json:"remediation"`
		Category       domain.DriftCategory `json:"category"`
		AdapterVersion string               `json:"adapterVersion"`
	}{
		RootCause:      card.RootCause,
		Remediation:    card.Remediation,
		Category:       card.Category,
		AdapterVersion: card.AdapterVersion,
	}
	digest, err := domain.CanonicalDigest(key)
	if err != nil {
		return "", fmt.Errorf("report: computeCardID: %w", err)
	}
	// digest is "sha256:<64 hex chars>" (domain.CanonicalDigest's own
	// documented shape); strip the algorithm prefix before truncating.
	hex := digest[len("sha256:"):]
	if len(hex) < cardIDDigestLength {
		return "", fmt.Errorf("report: computeCardID: digest %q shorter than expected", digest)
	}
	return cardIDPrefix + hex[:cardIDDigestLength], nil
}

// buildDriftCards assigns a stable ID to every ActionCard and wraps it into
// a DriftCard, in cards' own order (drift.Group/GroupWithSampleLimit already
// sort deterministically by groupKey, so this function does not re-sort).
func buildDriftCards(cards []drift.ActionCard) ([]DriftCard, error) {
	out := make([]DriftCard, 0, len(cards))
	for _, c := range cards {
		id, err := computeCardID(c)
		if err != nil {
			return nil, err
		}
		out = append(out, DriftCard{ID: id, ActionCard: c})
	}
	return out, nil
}
