package domain

import "testing"

func TestHostTier_Validation(t *testing.T) {
	tests := []struct {
		tier  HostTier
		valid bool
	}{
		{TierManaged, true},
		{TierBridge, true},
		{TierObserved, true},
		{"INVALID", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := tt.tier.Valid(); got != tt.valid {
			t.Errorf("HostTier(%q).Valid() = %v, want %v", tt.tier, got, tt.valid)
		}
		err := ValidateHostTier(tt.tier)
		if (err == nil) != tt.valid {
			t.Errorf("ValidateHostTier(%q) error = %v, wantValid %v", tt.tier, err, tt.valid)
		}
	}
}

func TestDefaultHostCapability(t *testing.T) {
	codexCap := DefaultHostCapability("codex")
	if codexCap.Tier != TierManaged || !codexCap.CanVirtualizeHome {
		t.Errorf("expected codex to be TierManaged with CanVirtualizeHome=true, got %+v", codexCap)
	}

	claudeCap := DefaultHostCapability("claude-code")
	if claudeCap.Tier != TierBridge || claudeCap.CanVirtualizeHome || !claudeCap.SupportsHooks {
		t.Errorf("expected claude-code to be TierBridge with CanVirtualizeHome=false and SupportsHooks=true, got %+v", claudeCap)
	}

	unknownCap := DefaultHostCapability("unknown-host")
	if unknownCap.Tier != TierObserved {
		t.Errorf("expected unknown host to be TierObserved, got %+v", unknownCap)
	}
}
