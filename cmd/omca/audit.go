package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/wangzitian0/oh-my-code-agent/internal/audit"
)

// runAudit implements `omca audit [flags] [path]`.
// Executes the 3-category 4+3+2=9 Doomsday Swarm Audit.
func runAudit(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(stderr)

	jsonOut := fs.Bool("json", false, "Output results as JSON")
	mode := fs.String("mode", "doomsday", "Audit mode: doomsday (4+3+2=9), blindfold, or contract")
	profile := fs.String("profile", "", "Alias for mode")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	_ = *mode
	if *profile != "" {
		_ = *profile
	}

	targetDir := "."
	extraArgs := fs.Args()
	if len(extraArgs) > 0 {
		targetDir = extraArgs[0]
	}

	barrier, err := audit.BuildBarrier(targetDir)
	if err != nil {
		fmt.Fprintf(stderr, "omca: audit: failed to scan directory %q: %v\n", targetDir, err)
		return 1
	}

	findings := make([]audit.ScoutFinding, 0)

	// Automated baseline observations from the barrier:
	hasTests := false
	for _, f := range barrier.BlindfoldFiles {
		if f.IsTest {
			hasTests = true
			break
		}
	}
	if !hasTests && len(barrier.BlindfoldFiles) > 0 {
		findings = append(findings, audit.ScoutFinding{
			Category: audit.CatEngineeringBlind,
			Scout:    audit.ScoutG3,
			Topic:    "No Automated Tests Found",
			Severity: "HIGH",
			Details:  "No test files detected in source tree under information barrier",
			Evidence: "0 test files",
		})
	}

	// PPT Project check (docs exist but zero source files)
	if len(barrier.DocFiles) > 0 && len(barrier.BlindfoldFiles) == 0 {
		findings = append(findings, audit.ScoutFinding{
			Category: audit.CatModuleContract,
			Scout:    audit.ScoutM2,
			Topic:    "PPT Only Project (No Implementation)",
			Severity: "CRITICAL",
			Details:  "Documentation exists but zero compilable source code was found",
			Evidence: fmt.Sprintf("%d doc files, 0 source files", len(barrier.DocFiles)),
		})
	}

	result := audit.SynthesizeDoomsdayAudit(targetDir, findings)

	if *jsonOut {
		jsonStr, err := audit.FormatJSON(result)
		if err != nil {
			fmt.Fprintf(stderr, "omca: audit: json formatting failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, jsonStr)
	} else {
		fmt.Fprint(stdout, audit.FormatMarkdown(result))
	}

	if result.Verdict == audit.VerdictBlocked {
		return 1
	}
	return 0
}
