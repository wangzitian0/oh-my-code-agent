package audit

import (
	"fmt"
	"math"
	"strings"
)

// AuditCategory defines the three orthogonal evaluation dimensions.
type AuditCategory string

const (
	CatModuleContract   AuditCategory = "MODULE_CONTRACT"   // Cat 1: 4 Scouts (向内看: 契约与影响)
	CatEngineeringBlind AuditCategory = "ENGINEERING_BLIND" // Cat 2: 3 Scouts (向下看: 裸机工程盲审)
	CatGoalCompleteness AuditCategory = "GOAL_COMPLETENESS" // Cat 3: 2 Scouts (向上看: 目标与副作用)
)

const (
	ModeDoomsday = "doomsday"
	ModeLean     = "lean"
)

// ScoutRole defines each specialized scout in Doomsday (9) or Lean (3) configurations.
type ScoutRole string

const (
	// Lean Configuration: 1 Scout per category (1 + 1 + 1 = 3 Scouts)
	ScoutM_Lean ScoutRole = "M_CONTRACT_SYNTHESIS" // Cat 1: 综合契约与影响
	ScoutG_Lean ScoutRole = "G_ENGINEERING_BLIND"  // Cat 2: 裸机工程通用盲审
	ScoutT_Lean ScoutRole = "T_GOAL_SIDE_EFFECTS"  // Cat 3: 宏观目标与副作用

	// Doomsday Configuration (4 + 3 + 2 = 9 Scouts)
	// Category 1: 4 Scouts
	ScoutM1 ScoutRole = "M1_API_BREAKING"
	ScoutM2 ScoutRole = "M2_SPEC_DEVIATION"
	ScoutM3 ScoutRole = "M3_DEPENDENCY_SPILL"
	ScoutM4 ScoutRole = "M4_SEMANTIC_DRIFT"

	// Category 2: 3 Scouts (Hard Information Barrier)
	ScoutG1 ScoutRole = "G1_INFRA_SRE"
	ScoutG2 ScoutRole = "G2_STAFF_ENG"
	ScoutG3 ScoutRole = "G3_QA_TEST_HOLES"

	// Category 3: 2 Scouts
	ScoutT1 ScoutRole = "T1_GOAL_COMPLETION"
	ScoutT2 ScoutRole = "T2_SIDE_EFFECTS"
)

// ScoutFinding represents a structured finding reported by one of the scouts.
type ScoutFinding struct {
	Category AuditCategory `json:"category"`
	Scout    ScoutRole     `json:"scout"`
	Topic    string        `json:"topic"`
	Severity string        `json:"severity"` // "CRITICAL", "HIGH", "MEDIUM", "LOW", "CLEAN"
	Details  string        `json:"details"`
	Evidence string        `json:"evidence"`
}

// AuditVerdict defines the gate outcome.
type AuditVerdict string

const (
	VerdictPass    AuditVerdict = "PASS"
	VerdictWarn    AuditVerdict = "WARN"
	VerdictBlocked AuditVerdict = "BLOCKED"
)

// DoomsdayAuditResult holds the aggregated audit outcome (3-scout Lean or 9-scout Doomsday).
type DoomsdayAuditResult struct {
	Target              string          `json:"target"`
	Mode                string          `json:"mode"`
	TotalScoutsDeployed int             `json:"total_scouts_deployed"`
	ModuleScore         float64         `json:"module_score"`      // Cat 1 Score (0.0~1.0)
	EngineeringScore    float64         `json:"engineering_score"` // Cat 2 Score (0.0~1.0)
	GoalScore           float64         `json:"goal_score"`        // Cat 3 Score (0.0~1.0)
	OverallScore        float64         `json:"overall_score"`     // Composite Score
	Verdict             AuditVerdict    `json:"verdict"`
	Findings            []ScoutFinding  `json:"findings"`
	CriticalBlockers    []string        `json:"critical_blockers"`
	WarningIssues       []string        `json:"warning_issues"`
	Summary             string          `json:"summary"`
}

// SynthesizeAudit aggregates findings and computes the 3D score for either Lean (3 scouts) or Doomsday (9 scouts) modes.
func SynthesizeAudit(target string, mode string, findings []ScoutFinding) *DoomsdayAuditResult {
	if mode == "" {
		mode = ModeDoomsday
	}
	modeLower := strings.ToLower(mode)

	totalScouts := 9
	resolvedMode := ModeDoomsday
	if modeLower == ModeLean {
		totalScouts = 3
		resolvedMode = ModeLean
	}

	res := &DoomsdayAuditResult{
		Target:              target,
		Mode:                resolvedMode,
		TotalScoutsDeployed: totalScouts,
		ModuleScore:         1.0,
		EngineeringScore:    1.0,
		GoalScore:           1.0,
		Findings:            findings,
		CriticalBlockers:    make([]string, 0),
		WarningIssues:       make([]string, 0),
	}

	var modDeductions, engDeductions, goalDeductions float64

	for _, f := range findings {
		sev := strings.ToUpper(strings.TrimSpace(f.Severity))
		if sev == "CLEAN" {
			continue
		}

		desc := fmt.Sprintf("[%s] %s: %s (Evidence: %s)", f.Scout, f.Topic, f.Details, f.Evidence)

		deduction := 0.0
		switch sev {
		case "CRITICAL":
			deduction = 0.50
			res.CriticalBlockers = append(res.CriticalBlockers, desc)
		case "HIGH":
			deduction = 0.25
			res.CriticalBlockers = append(res.CriticalBlockers, desc)
		case "MEDIUM":
			deduction = 0.10
			res.WarningIssues = append(res.WarningIssues, desc)
		case "LOW":
			deduction = 0.05
			res.WarningIssues = append(res.WarningIssues, desc)
		}

		switch f.Category {
		case CatModuleContract:
			modDeductions += deduction
		case CatEngineeringBlind:
			engDeductions += deduction
		case CatGoalCompleteness:
			goalDeductions += deduction
		}
	}

	res.ModuleScore = math.Max(0.0, math.Round((1.0-modDeductions)*100)/100)
	res.EngineeringScore = math.Max(0.0, math.Round((1.0-engDeductions)*100)/100)
	res.GoalScore = math.Max(0.0, math.Round((1.0-goalDeductions)*100)/100)

	// Weighted Composite: 35% Module + 35% Engineering + 30% Goal
	res.OverallScore = math.Round((0.35*res.ModuleScore+0.35*res.EngineeringScore+0.30*res.GoalScore)*100) / 100

	// Verdict logic
	if len(res.CriticalBlockers) > 0 || res.OverallScore < 0.60 {
		res.Verdict = VerdictBlocked
		res.Summary = fmt.Sprintf("🚨 审计未通过 (BLOCKED): 发现 %d 项严重阻断性缺陷，三维综合评分 %.2f",
			len(res.CriticalBlockers), res.OverallScore)
	} else if len(res.WarningIssues) > 0 || res.OverallScore < 0.85 {
		res.Verdict = VerdictWarn
		res.Summary = fmt.Sprintf("⚠️ 审计带条件通过 (WARN): 发现 %d 项需关注缺陷，三维综合评分 %.2f",
			len(res.WarningIssues), res.OverallScore)
	} else {
		res.Verdict = VerdictPass
		if resolvedMode == ModeLean {
			res.Summary = fmt.Sprintf("✅ 审计全绿通过 (PASS): 3 马仔精简审查无阻断，三维综合评分 %.2f",
				res.OverallScore)
		} else {
			res.Summary = fmt.Sprintf("✅ 审计全绿通过 (PASS): 9 马仔末日审查无阻断，三维综合评分 %.2f",
				res.OverallScore)
		}
	}

	return res
}

// SynthesizeDoomsdayAudit aggregates findings across all 9 scouts (backwards compatibility).
func SynthesizeDoomsdayAudit(target string, findings []ScoutFinding) *DoomsdayAuditResult {
	return SynthesizeAudit(target, ModeDoomsday, findings)
}
