package audit

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatJSON serializes the DoomsdayAuditResult to JSON.
func FormatJSON(res *DoomsdayAuditResult) (string, error) {
	bytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// FormatMarkdown formats the DoomsdayAuditResult into an executive summary report.
func FormatMarkdown(res *DoomsdayAuditResult) string {
	var b strings.Builder

	b.WriteString("# 🛡️ OMCA 9 马仔末日代码审计报告 (Doomsday Audit)\n\n")
	b.WriteString(fmt.Sprintf("**目标路径**: `%s`  \n", res.Target))
	b.WriteString(fmt.Sprintf("**审计裁决**: `%s`  \n", res.Verdict))
	b.WriteString(fmt.Sprintf("**三维综合得分**: `%.2f / 1.00`  \n", res.OverallScore))
	b.WriteString(fmt.Sprintf("**马仔部署规模**: `4 + 3 + 2 = %d 位`\n\n", res.TotalScoutsDeployed))

	b.WriteString("### 📊 三维正交得分矩阵\n\n")
	b.WriteString("| 审计维度 | 涵盖马仔 | 考察核心 | 维度得分 |\n")
	b.WriteString("| :--- | :---: | :--- | :---: |\n")
	b.WriteString(fmt.Sprintf("| **第一类: 模块契约与影响** | 4 位 (M1~M4) | API 兼容性 / README 设计承诺 / 上下游穿透 / 配置漂移 | `%.2f` |\n", res.ModuleScore))
	b.WriteString(fmt.Sprintf("| **第二类: 裸机工程通用盲审** | 3 位 (G1~G3) | 资源泄漏 / 并发安全 / 异常被吞 / 绿灯空跑 (信息结界) | `%.2f` |\n", res.EngineeringScore))
	b.WriteString(fmt.Sprintf("| **第三类: 宏观目标与终局** | 2 位 (T1~T2) | 原始诉求是否做透 / 系统性副作用与反噬风险 | `%.2f` |\n\n", res.GoalScore))

	if len(res.CriticalBlockers) > 0 {
		b.WriteString("### 🚨 严重阻断项 (Critical Blockers)\n\n")
		for _, blk := range res.CriticalBlockers {
			b.WriteString(fmt.Sprintf("- ❌ %s\n", blk))
		}
		b.WriteString("\n")
	}

	if len(res.WarningIssues) > 0 {
		b.WriteString("### ⚠️ 需关注缺陷 (Warnings & Edge Cases)\n\n")
		for _, w := range res.WarningIssues {
			b.WriteString(fmt.Sprintf("- ⚠️ %s\n", w))
		}
		b.WriteString("\n")
	}

	b.WriteString("### 📝 审计概要与裁决\n\n")
	b.WriteString(fmt.Sprintf("> %s\n\n", res.Summary))

	return b.String()
}
