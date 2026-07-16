package plugin

import "testing"

func TestReconcileModeValid(t *testing.T) {
	valid := []ReconcileMode{
		ReconcileManaged, ReconcilePatched, ReconcileObserved,
		ReconcileOpaque, ReconcileBlocked,
	}
	for _, m := range valid {
		if !m.Valid() {
			t.Errorf("ReconcileMode(%q).Valid() = false, want true", m)
		}
		if err := ValidateReconcileMode(m); err != nil {
			t.Errorf("ValidateReconcileMode(%q) = %v, want nil", m, err)
		}
	}

	invalid := ReconcileMode("IGNORED")
	if invalid.Valid() {
		t.Error("ReconcileMode(IGNORED).Valid() = true, want false")
	}
	if err := ValidateReconcileMode(invalid); err == nil {
		t.Error("ValidateReconcileMode(IGNORED) = nil, want error")
	}
}
