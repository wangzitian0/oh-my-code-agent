package drift

import (
	"fmt"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// DefaultSampleLimit bounds how many representative rows Group prints under
// each ActionCard's Samples, matching reporting.md §7's worked example
// (DR-017 shows 2 illustrative rows for an card whose Impact spans 40
// artifacts) — Samples is always illustrative, never the mechanism for
// exposing the full matrix (Matrix and Query are).
const DefaultSampleLimit = 5

// groupKey is the root-cause aggregation key: every Assertion sharing the
// same root cause, remediation, outcome class (Category — EXCEPTION and the
// six base categories are each their own outcome class), and adapter
// version collapses into one ActionCard (docs/architecture/reporting.md §7:
// "Human output groups by: root cause + remediation + outcome class +
// adapter version").
type groupKey struct {
	rootCause      string
	remediation    string
	category       domain.DriftCategory
	adapterVersion string
}

// Group aggregates assertions into ActionCards using DefaultSampleLimit.
func Group(assertions []Assertion) []ActionCard {
	return GroupWithSampleLimit(assertions, DefaultSampleLimit)
}

// GroupWithSampleLimit aggregates assertions into ActionCards exactly as
// Group does, but bounds each card's Samples to at most sampleLimit entries
// (sampleLimit <= 0 means unlimited — Samples equals the full, deterministic
// Matrix order). Card order, Matrix order, and Samples selection never
// depend on the input slice's order: assertions is sorted internally before
// grouping, so feeding the same logical set of assertions in any order
// produces byte-identical output (see determinism_test.go).
func GroupWithSampleLimit(assertions []Assertion, sampleLimit int) []ActionCard {
	buckets := map[groupKey][]Assertion{}
	var keys []groupKey
	seen := map[groupKey]bool{}
	for _, a := range assertions {
		k := groupKey{
			rootCause:      a.RootCause,
			remediation:    a.Remediation,
			category:       a.Category,
			adapterVersion: a.AdapterVersion,
		}
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
		buckets[k] = append(buckets[k], a)
	}

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].rootCause != keys[j].rootCause {
			return keys[i].rootCause < keys[j].rootCause
		}
		if keys[i].remediation != keys[j].remediation {
			return keys[i].remediation < keys[j].remediation
		}
		if keys[i].category != keys[j].category {
			return keys[i].category < keys[j].category
		}
		return keys[i].adapterVersion < keys[j].adapterVersion
	})

	cards := make([]ActionCard, 0, len(keys))
	for _, k := range keys {
		matrix := append([]Assertion(nil), buckets[k]...)
		sort.Slice(matrix, func(i, j int) bool { return matrixLess(matrix[i], matrix[j]) })

		card := ActionCard{
			RootCause:      k.rootCause,
			Remediation:    k.remediation,
			Category:       k.category,
			AdapterVersion: k.adapterVersion,
			Impact:         computeImpact(matrix),
			EvidenceCounts: computeEvidenceCounts(matrix),
			Guarantee:      representativeGuarantee(matrix),
			Matrix:         matrix,
		}
		card.Samples = selectSamples(matrix, sampleLimit)
		cards = append(cards, card)
	}
	return cards
}

// matrixLess orders a card's Matrix by (Project, Host, EntityID, Field) so
// that Matrix order — and therefore Samples selection, which walks Matrix in
// order — never depends on the order assertions were produced or grouped
// in.
func matrixLess(a, b Assertion) bool {
	if a.Project != b.Project {
		return a.Project < b.Project
	}
	if a.Host != b.Host {
		return a.Host < b.Host
	}
	if a.EntityID != b.EntityID {
		return a.EntityID < b.EntityID
	}
	return a.Field < b.Field
}

// computeImpact counts the distinct projects, distinct hosts, and total
// assertions ("artifacts") a card's matrix spans
// (docs/architecture/reporting.md §7: "Impact 8 projects · 5 hosts · 40
// artifacts").
func computeImpact(matrix []Assertion) Impact {
	projects := map[string]bool{}
	hosts := map[string]bool{}
	for _, a := range matrix {
		if a.Project != "" {
			projects[a.Project] = true
		}
		if a.Host != "" {
			hosts[a.Host] = true
		}
	}
	return Impact{Projects: len(projects), Hosts: len(hosts), Artifacts: len(matrix)}
}

// computeEvidenceCounts tallies matrix entries by EvidenceLevel
// (docs/architecture/reporting.md §7: "Evidence 38 × E3, 2 × E2"). Entries
// with no EvidenceLevel set are not counted.
func computeEvidenceCounts(matrix []Assertion) map[domain.EvidenceLevel]int {
	counts := map[domain.EvidenceLevel]int{}
	for _, a := range matrix {
		if a.EvidenceLevel == "" {
			continue
		}
		counts[a.EvidenceLevel]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

// representativeGuarantee returns the card's single GuaranteeLevel only when
// every matrix entry that sets one agrees; it returns "" both when no entry
// sets one and when entries disagree, rather than silently picking one and
// hiding a real disagreement between assertions grouped into the same card.
func representativeGuarantee(matrix []Assertion) domain.GuaranteeLevel {
	var g domain.GuaranteeLevel
	for _, a := range matrix {
		if a.Guarantee == "" {
			continue
		}
		if g == "" {
			g = a.Guarantee
			continue
		}
		if g != a.Guarantee {
			return ""
		}
	}
	return g
}

// selectSamples deterministically picks representative rows from matrix
// (already sorted by matrixLess): one entry for each distinct (outcome,
// exceptional) bucket, in matrix order, before any redundant entry
// (docs/architecture/reporting.md §7: "Sample selection is deterministic
// and covers each distinct outcome ... and exceptional bucket before adding
// redundant examples"). If more buckets exist than the limit allows, the
// buckets kept are exactly the first `limit` encountered in matrix's
// deterministic order — never an arbitrary or random subset. Once every
// bucket has one representative, remaining slots (if any) are filled with
// the next entries in matrix order, including repeats of already-covered
// buckets, up to limit. limit <= 0 or limit >= len(matrix) returns the full
// matrix order (every entry is a "sample").
func selectSamples(matrix []Assertion, limit int) []Assertion {
	if limit <= 0 || limit > len(matrix) {
		limit = len(matrix)
	}

	used := make([]bool, len(matrix))
	seenBucket := map[string]bool{}
	out := make([]Assertion, 0, limit)
	for i, a := range matrix {
		k := outcomeBucketKey(a)
		if seenBucket[k] {
			continue
		}
		seenBucket[k] = true
		used[i] = true
		out = append(out, a)
	}

	if len(out) >= limit {
		return out[:limit]
	}
	for i, a := range matrix {
		if len(out) >= limit {
			break
		}
		if used[i] {
			continue
		}
		out = append(out, a)
	}
	return out
}

// outcomeBucketKey is the "distinct outcome ... and exceptional bucket"
// selectSamples covers before adding redundancy: the expected->observed
// transition, plus whether the assertion ended up classified EXCEPTION (an
// excepted difference is a materially different outcome for a human reader
// than the same transition reported as live drift, even though the
// underlying values match).
func outcomeBucketKey(a Assertion) string {
	return fmt.Sprintf("%v->%v|exception=%v", a.Expected, a.Observed, a.Category == domain.DriftException)
}
