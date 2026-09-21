// Package schema exposes canonical entity schema definitions, merge operators,
// and registry lookups, bridging internal/ontology to standard schema terminology.
package schema

import "github.com/wangzitian0/oh-my-code-agent/internal/ontology"

// ConceptSchema is one loaded agent configuration schema: an entity ID,
// canonical fields, identity rule, and merge operators.
type ConceptSchema = ontology.ConceptSchema

// LogicalIdentity defines entity equivalence across physical configurations.
type LogicalIdentity = ontology.LogicalIdentity

// MergeOperator defines how configuration items combine.
type MergeOperator = ontology.MergeOperator

// Registry holds the loaded entity schemas.
type Registry = ontology.Registry

const (
	OpReplace          = ontology.OpReplace
	OpDeepMerge        = ontology.OpDeepMerge
	OpConcatOrdered    = ontology.OpConcatOrdered
	OpUnionByID        = ontology.OpUnionByID
	OpFirstMatch       = ontology.OpFirstMatch
	OpNamespace        = ontology.OpNamespace
	OpDenyWins         = ontology.OpDenyWins
	OpManagedGuardrail = ontology.OpManagedGuardrail
	OpUnspecified      = ontology.OpUnspecified
)

// Concept looks up a canonical entity schema by ID (e.g. "skill", "mcp_server").
func Concept(id string) (ConceptSchema, bool) {
	return ontology.Concept(id)
}

// LoadRegistry loads concept schemas from an explicit directory.
func LoadRegistry(dir string) (*Registry, error) {
	return ontology.LoadRegistry(dir)
}

// ValidateMergeOperator checks if a merge operator is valid.
func ValidateMergeOperator(o MergeOperator) error {
	return ontology.ValidateMergeOperator(o)
}
