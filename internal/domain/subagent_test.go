package domain_test

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func TestSubagent_TiersValid(t *testing.T) {
	tests := []struct {
		tier  domain.SubagentTier
		valid bool
	}{
		{domain.TierLead, true},
		{domain.TierWorker, true},
		{domain.TierAudit, true},
		{domain.TierSwarm, true},
		{domain.SubagentTier("INVALID"), false},
		{domain.SubagentTier(""), false},
	}

	for _, tt := range tests {
		if got := tt.tier.Valid(); got != tt.valid {
			t.Errorf("SubagentTier(%q).Valid() = %v, want %v", tt.tier, got, tt.valid)
		}
	}
}

func TestSubagent_DefaultRoleSpecs(t *testing.T) {
	tiers := []domain.SubagentTier{
		domain.TierLead,
		domain.TierWorker,
		domain.TierAudit,
		domain.TierSwarm,
	}

	for _, tier := range tiers {
		spec, err := domain.DefaultRoleSpec(tier)
		if err != nil {
			t.Fatalf("DefaultRoleSpec(%s) returned unexpected error: %v", tier, err)
		}
		if err := spec.Validate(); err != nil {
			t.Errorf("DefaultRoleSpec(%s).Validate() failed: %v", tier, err)
		}
		if spec.Tier != tier {
			t.Errorf("DefaultRoleSpec(%s).Tier = %s, want %s", tier, spec.Tier, tier)
		}
	}

	// Verify specific invariants from the architecture
	workerSpec, _ := domain.DefaultRoleSpec(domain.TierWorker)
	if workerSpec.Quota.MaxConcurrency != 5 {
		t.Errorf("Worker max concurrency = %d, want 5", workerSpec.Quota.MaxConcurrency)
	}
	if !workerSpec.Tools.AllowsWrite() {
		t.Errorf("Worker tools must allow write")
	}

	auditSpec, _ := domain.DefaultRoleSpec(domain.TierAudit)
	if auditSpec.Tools.AllowsWrite() {
		t.Errorf("Audit tools must NOT allow write")
	}

	swarmSpec, _ := domain.DefaultRoleSpec(domain.TierSwarm)
	if swarmSpec.Quota.MaxConcurrency != 50 {
		t.Errorf("Swarm max concurrency = %d, want 50", swarmSpec.Quota.MaxConcurrency)
	}
	if swarmSpec.Tools.Mode != domain.ToolModeReadOnly {
		t.Errorf("Swarm tools mode = %s, want READ_ONLY", swarmSpec.Tools.Mode)
	}
	if swarmSpec.Tools.AllowsWrite() {
		t.Errorf("Swarm tools must NOT allow write")
	}
}

func TestSubagent_ToolAccessPolicy(t *testing.T) {
	policy := domain.ToolAccessPolicy{
		Mode:           domain.ToolModeReadOnly,
		ReadOnlyTools:  []string{"read_file", "grep_search"},
		ReadWriteTools: nil,
	}

	if policy.AllowsWrite() {
		t.Error("ReadOnly policy should not allow write")
	}
	if !policy.IsToolAllowed("read_file", false) {
		t.Error("read_file should be allowed for reading")
	}
	if policy.IsToolAllowed("unknown_read", false) {
		t.Error("unknown_read should be rejected when whitelist specified")
	}
	if policy.IsToolAllowed("write_to_file", true) {
		t.Error("write_to_file should be rejected under ReadOnly policy")
	}

	// Swarm policy (Mode None)
	swarmPolicy := domain.ToolAccessPolicy{
		Mode: domain.ToolModeNone,
	}
	if swarmPolicy.IsToolAllowed("any_tool", false) {
		t.Error("ToolModeNone must reject all tools")
	}

	// Empty whitelist in ReadOnly must deny by default
	emptyPolicy := domain.ToolAccessPolicy{
		Mode: domain.ToolModeReadOnly,
	}
	if emptyPolicy.IsToolAllowed("view_file", false) {
		t.Error("empty whitelist under ReadOnly must deny by default")
	}

	// Full policy (Lead)
	fullPolicy := domain.ToolAccessPolicy{
		Mode: domain.ToolModeFull,
	}
	if !fullPolicy.AllowsWrite() {
		t.Error("Full policy must allow write")
	}
	if !fullPolicy.IsToolAllowed("any_tool", true) {
		t.Error("Full policy must allow any tool")
	}

	// Default Audit spec has preconfigured whitelist
	auditSpec, _ := domain.DefaultRoleSpec(domain.TierAudit)
	if !auditSpec.Tools.IsToolAllowed("view_file", false) {
		t.Error("DefaultRoleSpec(TierAudit) must allow view_file")
	}
	if auditSpec.Tools.IsToolAllowed("write_to_file", true) {
		t.Error("DefaultRoleSpec(TierAudit) must reject write_to_file")
	}

	// Default Swarm spec has preconfigured whitelist (read-only)
	swarmSpec, _ := domain.DefaultRoleSpec(domain.TierSwarm)
	if !swarmSpec.Tools.IsToolAllowed("view_file", false) {
		t.Error("DefaultRoleSpec(TierSwarm) must allow view_file")
	}
	if swarmSpec.Tools.IsToolAllowed("write_to_file", true) {
		t.Error("DefaultRoleSpec(TierSwarm) must reject write_to_file")
	}
}

func TestSubagent_ValidationEnforcement(t *testing.T) {
	// Audit role cannot have write permissions
	invalidAudit := domain.AgentRoleSpec{
		Role:  "Audit",
		Tier:  domain.TierAudit,
		Model: "glm-5.3",
		Tools: domain.ToolAccessPolicy{
			Mode: domain.ToolModeReadWrite,
		},
		Quota: domain.QuotaBudget{
			MaxConcurrency: 5,
			MaxTokens:      16384,
		},
	}
	if err := invalidAudit.Validate(); err == nil {
		t.Error("expected error when audit role has write permissions, got nil")
	}

	// Swarm role cannot have write permissions
	invalidSwarm := domain.AgentRoleSpec{
		Role:  "Swarm",
		Tier:  domain.TierSwarm,
		Model: "glm-5.3-flash",
		Tools: domain.ToolAccessPolicy{
			Mode: domain.ToolModeReadWrite,
		},
		Quota: domain.QuotaBudget{
			MaxConcurrency: 50,
			MaxTokens:      8192,
		},
	}
	if err := invalidSwarm.Validate(); err == nil {
		t.Error("expected error when swarm role has write permissions, got nil")
	}

	// Swarm role with ToolModeReadOnly is valid
	validSwarmReadOnly := domain.AgentRoleSpec{
		Role:  "Swarm",
		Tier:  domain.TierSwarm,
		Model: "glm-5.3-flash",
		Tools: domain.ToolAccessPolicy{
			Mode:          domain.ToolModeReadOnly,
			ReadOnlyTools: domain.StandardReadOnlyTools,
		},
		Quota: domain.QuotaBudget{
			MaxConcurrency: 50,
			MaxTokens:      8192,
		},
	}
	if err := validSwarmReadOnly.Validate(); err != nil {
		t.Errorf("valid read-only swarm spec rejected: %v", err)
	}

	// Swarm role with ToolModeNone is also valid
	validSwarmNone := domain.AgentRoleSpec{
		Role:  "Swarm",
		Tier:  domain.TierSwarm,
		Model: "glm-5.3-flash",
		Tools: domain.ToolAccessPolicy{
			Mode: domain.ToolModeNone,
		},
		Quota: domain.QuotaBudget{
			MaxConcurrency: 50,
			MaxTokens:      8192,
		},
	}
	if err := validSwarmNone.Validate(); err != nil {
		t.Errorf("valid pure text swarm spec rejected: %v", err)
	}

	// Negative quota must be rejected
	negativeQuota := domain.AgentRoleSpec{
		Role:  "Worker",
		Tier:  domain.TierWorker,
		Model: "glm-5.3",
		Tools: domain.ToolAccessPolicy{
			Mode: domain.ToolModeReadWrite,
		},
		Quota: domain.QuotaBudget{
			MaxConcurrency: -1,
		},
	}
	if err := negativeQuota.Validate(); err == nil {
		t.Error("expected error on negative concurrency, got nil")
	}
}
