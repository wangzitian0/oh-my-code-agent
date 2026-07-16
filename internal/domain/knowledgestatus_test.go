package domain

import "testing"

func TestKnowledgeStatusValid(t *testing.T) {
	valid := []KnowledgeStatus{
		KnowledgeFresh, KnowledgeDue, KnowledgeStale,
		KnowledgeConflicted, KnowledgeSuperseded, KnowledgeRetired,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("KnowledgeStatus(%q).Valid() = false, want true", s)
		}
		if err := ValidateKnowledgeStatus(s); err != nil {
			t.Errorf("ValidateKnowledgeStatus(%q) = %v, want nil", s, err)
		}
	}

	invalid := KnowledgeStatus("ARCHIVED")
	if invalid.Valid() {
		t.Error("KnowledgeStatus(ARCHIVED).Valid() = true, want false")
	}
	if err := ValidateKnowledgeStatus(invalid); err == nil {
		t.Error("ValidateKnowledgeStatus(ARCHIVED) = nil, want error")
	}
}
