package domain

import (
	"errors"
	"fmt"
	"slices"
)

// SubagentTier classifies the role and capability hierarchy of agents:
// - TierLead (总裁 / 大哥): Full authority, primary session, task triage, architecture decisions, git/PR gate.
// - TierWorker (小弟): Read/write and execute tools, writes implementation, refactoring, bugfixes.
// - TierSwarm (马仔): Unified read-only fast fleet (combines audit and scanning), code auditing, defect detection, hypothesis validation, log scanning, high-concurrency breadth exploration.
// - TierAudit (审计): Backward-compatible alias / legacy tier for TierSwarm (read-only).
type SubagentTier string

const (
	TierLead   SubagentTier = "LEAD"
	TierWorker SubagentTier = "WORKER"
	TierSwarm  SubagentTier = "SWARM"
	TierAudit  SubagentTier = "AUDIT" // Deprecated: backward compatibility for TierSwarm
)

// Valid reports whether t is a recognized SubagentTier.
func (t SubagentTier) Valid() bool {
	switch t {
	case TierLead, TierWorker, TierSwarm, TierAudit:
		return true
	default:
		return false
	}
}

// ToolAccessMode specifies the permission level for tool access.
type ToolAccessMode string

const (
	ToolModeNone      ToolAccessMode = "NONE"       // 无工具纯文本 (马仔纯文本批处理 / 广度扫描)
	ToolModeReadOnly  ToolAccessMode = "READ_ONLY"  // 只读工具白名单 (马仔代码审计 / 找茬)
	ToolModeReadWrite ToolAccessMode = "READ_WRITE" // 读写执行工具白名单 (小弟)
	ToolModeFull      ToolAccessMode = "FULL"       // 全权工具访问 (总裁 / 大哥)
)

// ToolAccessPolicy defines tool execution boundaries for an agent tier.
type ToolAccessPolicy struct {
	Mode           ToolAccessMode `json:"mode"`
	ReadOnlyTools  []string       `json:"readOnlyTools,omitempty"`
	ReadWriteTools []string       `json:"readWriteTools,omitempty"`
}

// Standard tool presets for agent tiers adhering to the collaboration hierarchy.
var (
	StandardReadOnlyTools = []string{
		"view_file",
		"grep_search",
		"find_by_name",
		"list_dir",
		"read_url_content",
		"subagent_quick_lint",
	}

	StandardReadWriteTools = []string{
		"write_to_file",
		"replace_file_content",
		"run_command",
		"subagent_code_transform",
		"subagent_task",
	}
)

// AllowsWrite reports whether write or execute actions are permitted.
func (p ToolAccessPolicy) AllowsWrite() bool {
	return p.Mode == ToolModeReadWrite || p.Mode == ToolModeFull
}

// IsToolAllowed checks if the specified tool is permitted under this policy.
// Deny-by-default: empty whitelists in ReadOnly or ReadWrite reject tools.
func (p ToolAccessPolicy) IsToolAllowed(toolName string, isWriteOrExec bool) bool {
	switch p.Mode {
	case ToolModeFull:
		return true
	case ToolModeNone:
		return false
	case ToolModeReadOnly:
		if isWriteOrExec {
			return false
		}
		// Deny-by-default on empty whitelist
		return slices.Contains(p.ReadOnlyTools, toolName)
	case ToolModeReadWrite:
		if isWriteOrExec {
			return slices.Contains(p.ReadWriteTools, toolName)
		}
		return slices.Contains(p.ReadOnlyTools, toolName) || slices.Contains(p.ReadWriteTools, toolName)
	default:
		return false
	}
}

// QuotaBudget defines execution capacity constraints (concurrency and token limits).
type QuotaBudget struct {
	MaxConcurrency int `json:"maxConcurrency"`
	MaxTokens      int `json:"maxTokens"`
}

// Validate checks the consistency of the QuotaBudget.
func (q QuotaBudget) Validate() error {
	if q.MaxConcurrency < 0 {
		return errors.New("domain: maxConcurrency cannot be negative")
	}
	if q.MaxTokens < 0 {
		return errors.New("domain: maxTokens cannot be negative")
	}
	return nil
}

// AgentRoleSpec defines the contract and constraints for an agent role.
type AgentRoleSpec struct {
	Role        string           `json:"role"`
	Tier        SubagentTier     `json:"tier"`
	Model       string           `json:"model"`
	Description string           `json:"description,omitempty"`
	Tools       ToolAccessPolicy `json:"tools"`
	Quota       QuotaBudget      `json:"quota"`
}

// Validate enforces contract invariants across tiers:
// 1. Tier must be valid.
// 2. Role must not be empty.
// 3. Swarm / Audit agents must not allow write/execution (read-only or pure text).
// 4. Quotas must be non-negative.
func (s AgentRoleSpec) Validate() error {
	if !s.Tier.Valid() {
		return fmt.Errorf("domain: invalid subagent tier %q", s.Tier)
	}
	if s.Role == "" {
		return errors.New("domain: agent role cannot be empty")
	}
	if err := s.Quota.Validate(); err != nil {
		return err
	}
	switch s.Tier {
	case TierSwarm:
		if s.Tools.AllowsWrite() {
			return errors.New("domain: swarm agent must be read-only (write/exec prohibited)")
		}
		if s.Tools.Mode != ToolModeReadOnly && s.Tools.Mode != ToolModeNone {
			return errors.New("domain: swarm agent tool mode must be READ_ONLY or NONE")
		}
	case TierAudit:
		if s.Tools.AllowsWrite() {
			return errors.New("domain: audit agent must be read-only (write/exec prohibited)")
		}
		if s.Tools.Mode != ToolModeReadOnly && s.Tools.Mode != ToolModeNone {
			return errors.New("domain: audit agent tool mode must be READ_ONLY or NONE")
		}
	}
	return nil
}

// DefaultRoleSpec returns the canonical role spec adhering to the multi-agent hierarchy.
func DefaultRoleSpec(tier SubagentTier) (AgentRoleSpec, error) {
	switch tier {
	case TierLead:
		return AgentRoleSpec{
			Role:        "Lead",
			Tier:        TierLead,
			Model:       "fable-5.x",
			Description: "Primary session / task triage / architecture decisions / git gates",
			Tools: ToolAccessPolicy{
				Mode: ToolModeFull,
			},
			Quota: QuotaBudget{
				MaxConcurrency: 1,
				MaxTokens:      32768,
			},
		}, nil
	case TierWorker:
		return AgentRoleSpec{
			Role:        "Worker",
			Tier:        TierWorker,
			Model:       "glm-5.3",
			Description: "Implementation, refactoring, code fixing with read/write/exec permissions",
			Tools: ToolAccessPolicy{
				Mode:           ToolModeReadWrite,
				ReadOnlyTools:  StandardReadOnlyTools,
				ReadWriteTools: StandardReadWriteTools,
			},
			Quota: QuotaBudget{
				MaxConcurrency: 5,
				MaxTokens:      16384,
			},
		}, nil
	case TierAudit:
		return AgentRoleSpec{
			Role:        "Audit",
			Tier:        TierAudit,
			Model:       "glm-5.3-flash",
			Description: "Code review, defect finding, hypothesis checking (read-only, backward-compatible tier)",
			Tools: ToolAccessPolicy{
				Mode:          ToolModeReadOnly,
				ReadOnlyTools: StandardReadOnlyTools,
			},
			Quota: QuotaBudget{
				MaxConcurrency: 50,
				MaxTokens:      8192,
			},
		}, nil
	case TierSwarm:
		return AgentRoleSpec{
			Role:        "Swarm",
			Tier:        TierSwarm,
			Model:       "glm-5.3-flash",
			Description: "High-concurrency read-only fast fleet (code auditing, defect detection, hypothesis validation, log scanning, breadth exploration)",
			Tools: ToolAccessPolicy{
				Mode:          ToolModeReadOnly,
				ReadOnlyTools: StandardReadOnlyTools,
			},
			Quota: QuotaBudget{
				MaxConcurrency: 50,
				MaxTokens:      8192,
			},
		}, nil
	default:
		return AgentRoleSpec{}, fmt.Errorf("domain: unknown tier %q", tier)
	}
}
