package normalize

// This file exists in PR-04 only to prove the import/data-flow shape: the
// real projector — matching physical Observations to ontology concepts,
// merging them per each concept's operator, and producing an Observed
// Graph (docs/architecture/README.md §6, §9 Normalizer interface) — is
// PR-17's job. normalize must look up concept facts (canonical fields,
// logical-identity rule, allowed merge operators) through
// internal/ontology's loader, never by re-deriving or duplicating them in
// a local map; see normalize_test.go for the check that this import
// boundary actually holds.

import "github.com/wangzitian0/oh-my-code-agent/internal/ontology"

// MergeOperatorsFor returns the merge operators the ontology allows for
// conceptID, sourced entirely from internal/ontology's concept registry.
// This function intentionally holds no concept knowledge of its own: it is
// a thin pass-through so the real normalizer (PR-17) has one obvious call
// to make instead of a local enum to keep in sync with ontology/concepts/.
func MergeOperatorsFor(conceptID string) ([]ontology.MergeOperator, bool) {
	c, ok := ontology.Concept(conceptID)
	if !ok {
		return nil, false
	}
	return c.MergeOperators, true
}

// LogicalIdentityFor returns the logical-identity rule the ontology
// declares for conceptID, again sourced entirely through
// internal/ontology's loader.
func LogicalIdentityFor(conceptID string) (ontology.LogicalIdentity, bool) {
	c, ok := ontology.Concept(conceptID)
	if !ok {
		return ontology.LogicalIdentity{}, false
	}
	return c.LogicalIdentity, true
}
