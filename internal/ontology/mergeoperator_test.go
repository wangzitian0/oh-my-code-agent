package ontology

import "testing"

func TestMergeOperatorValid(t *testing.T) {
	valid := []MergeOperator{
		OpReplace, OpDeepMerge, OpConcatOrdered, OpUnionByID, OpFirstMatch,
		OpNamespace, OpDenyWins, OpManagedGuardrail, OpUnspecified,
	}
	for _, op := range valid {
		if !op.Valid() {
			t.Errorf("MergeOperator(%q).Valid() = false, want true", op)
		}
		if err := ValidateMergeOperator(op); err != nil {
			t.Errorf("ValidateMergeOperator(%q) = %v, want nil", op, err)
		}
	}

	// docs/knowledge/README.md's own Knowledge Pack Contract example uses
	// "KEEP_BOTH", which is not in the §3.1 closed operator list; it must
	// not be silently accepted here as a synonym.
	invalid := MergeOperator("KEEP_BOTH")
	if invalid.Valid() {
		t.Error("MergeOperator(KEEP_BOTH).Valid() = true, want false")
	}
	if err := ValidateMergeOperator(invalid); err == nil {
		t.Error("ValidateMergeOperator(KEEP_BOTH) = nil, want error")
	}
}
