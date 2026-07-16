package ontology

import "fmt"

// MergeOperator is a composition rule two or more sources of the same
// concept can resolve through (docs/ontology/README.md §3.1, Merge
// operators). It is a closed enum: an operator name that is not on this
// list is a typo or an invented synonym, not a new rule — new merge
// semantics require a documented addition to §3.1 first.
type MergeOperator string

const (
	// OpReplace: a higher source replaces a scalar or whole entity.
	OpReplace MergeOperator = "REPLACE"
	// OpDeepMerge: objects merge recursively; leaf conflicts require an
	// explicit winner.
	OpDeepMerge MergeOperator = "DEEP_MERGE"
	// OpConcatOrdered: instruction texts are appended in a documented
	// order.
	OpConcatOrdered MergeOperator = "CONCAT_ORDERED"
	// OpUnionByID: entities merge by canonical logical ID while retaining
	// provenance.
	OpUnionByID MergeOperator = "UNION_BY_ID"
	// OpFirstMatch: the first existing or matching source wins and later
	// candidates are ignored.
	OpFirstMatch MergeOperator = "FIRST_MATCH"
	// OpNamespace: duplicate names remain distinct through a package or
	// directory prefix.
	OpNamespace MergeOperator = "NAMESPACE"
	// OpDenyWins: any applicable deny blocks the action.
	OpDenyWins MergeOperator = "DENY_WINS"
	// OpManagedGuardrail: admin policy constrains the result rather than
	// merely supplying a higher value.
	OpManagedGuardrail MergeOperator = "MANAGED_GUARDRAIL"
	// OpUnspecified: vendor does not define conflict resolution; surface a
	// conflict.
	OpUnspecified MergeOperator = "UNSPECIFIED"
)

var validMergeOperators = map[MergeOperator]bool{
	OpReplace:          true,
	OpDeepMerge:        true,
	OpConcatOrdered:    true,
	OpUnionByID:        true,
	OpFirstMatch:       true,
	OpNamespace:        true,
	OpDenyWins:         true,
	OpManagedGuardrail: true,
	OpUnspecified:      true,
}

// Valid reports whether o is one of the nine defined merge operators.
func (o MergeOperator) Valid() bool {
	return validMergeOperators[o]
}

// ValidateMergeOperator rejects any value outside the closed §3.1 operator
// enum.
func ValidateMergeOperator(o MergeOperator) error {
	if !o.Valid() {
		return fmt.Errorf("invalid merge operator %q", o)
	}
	return nil
}
