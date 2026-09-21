package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/audit"
)

// runAudit implements `omca audit [flags] [path]`.
// Executes the 3-category audit: either Lean (1+1+1=3) or Doomsday (4+3+2=9).
func runAudit(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(stderr)

	// Lean (3 scouts) is the default: coordination cost is multiplicative, not
	// additive, and a wider fan-out only scales the lead's burden of disproving
	// findings. Doomsday (9) stays available behind --mode for release sign-off.
	defaultMode := "lean"
	if env := os.Getenv("OMCA_AUDIT_MODE"); env != "" {
		defaultMode = env
	}

	jsonOut := fs.Bool("json", false, "Output results as JSON")
	mode := fs.String("mode", defaultMode, "Audit mode: doomsday (4+3+2=9) or lean (1+1+1=3, token-saving)")
	profile := fs.String("profile", "", "Alias for mode")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	selectedMode := strings.ToLower(strings.TrimSpace(*mode))
	if *profile != "" {
		selectedMode = strings.ToLower(strings.TrimSpace(*profile))
	}
	if selectedMode == "" {
		selectedMode = audit.ModeDoomsday
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

	// Determine scout role assignment based on mode
	scoutTestHoles := audit.ScoutG3
	scoutPPT := audit.ScoutM2
	if selectedMode == audit.ModeLean {
		scoutTestHoles = audit.ScoutG_Lean
		scoutPPT = audit.ScoutM_Lean
	}

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
			Scout:    scoutTestHoles,
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
			Scout:    scoutPPT,
			Topic:    "PPT Only Project (No Implementation)",
			Severity: "CRITICAL",
			Details:  "Documentation exists but zero compilable source code was found",
			Evidence: fmt.Sprintf("%d doc files, 0 source files", len(barrier.DocFiles)),
		})
	}

	result := audit.SynthesizeAudit(targetDir, selectedMode, findings)

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
