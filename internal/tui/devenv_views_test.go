package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// TestDevEnvViews_TableMapsEveryViewToExactHeaderCommand verifies issue #126's
// first acceptance criterion: every absorbed ws-tui view maps to exactly one
// canonical dev_env command string, and the rendered view's header line equals
// `Header: <command>`.
func TestDevEnvViews_TableMapsEveryViewToExactHeaderCommand(t *testing.T) {
	if len(DevEnvViewSpecs) != 3 {
		t.Fatalf("len(DevEnvViewSpecs) = %d, want 3 (Workspace, Project, Tool Matrix)", len(DevEnvViewSpecs))
	}

	tmp := t.TempDir()
	ctx := DevEnvContext{
		WorkspaceRoot: tmp,
		BinDir:        tmp, // empty bin dir: commands fail cleanly, header still rendered verbatim
	}

	for _, spec := range DevEnvViewSpecs {
		t.Run(spec.Title, func(t *testing.T) {
			if spec.Command == "" {
				t.Fatalf("spec %q has empty Command", spec.Title)
			}
			gotHeader, ok := HeaderForDevEnvView(spec.View)
			if !ok || gotHeader != spec.Command {
				t.Fatalf("HeaderForDevEnvView(%v) = (%q, %v), want (%q, true)", spec.View, gotHeader, ok, spec.Command)
			}

			m := NewModel(report.Artifact{}).WithDevEnvContext(ctx).SetActive(spec.View)
			rendered := m.renderActive()
			lines := strings.Split(rendered, "\n")
			if len(lines) == 0 {
				t.Fatalf("renderActive() for %s returned empty string", spec.Title)
			}
			wantFirstLine := "Header: " + spec.Command
			if lines[0] != wantFirstLine {
				t.Errorf("first line of %s view = %q, want %q", spec.Title, lines[0], wantFirstLine)
			}
		})
	}
}

// TestWorkspaceView_KillingWsDoctorShowsUnavailableNeverGreen verifies issue
// #126's second acceptance criterion: renaming/killing the `ws-doctor` binary
// makes the Workspace view render `unavailable: ws-doctor --json exit ...`,
// never empty-green or passing.
func TestWorkspaceView_KillingWsDoctorShowsUnavailableNeverGreen(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("MkdirAll(binDir): %v", err)
	}

	// Install working ws-check-drift and ws-doctor scripts.
	driftBin := filepath.Join(binDir, "ws-check-drift")
	driftScript := `#!/bin/sh
printf '{"workspace":"zitian","path":"%s","drift_found":false,"messages":[],"skills":["audit"]}\n' "$1"
`
	if err := os.WriteFile(driftBin, []byte(driftScript), 0755); err != nil {
		t.Fatalf("WriteFile(ws-check-drift): %v", err)
	}

	docBin := filepath.Join(binDir, "ws-doctor")
	docScript := `#!/bin/sh
printf '{"all_passed":true,"checks":[{"name":"Git Identity","ok":true,"detail":"ok"}]}\n'
`
	if err := os.WriteFile(docBin, []byte(docScript), 0755); err != nil {
		t.Fatalf("WriteFile(ws-doctor): %v", err)
	}

	ctx := DevEnvContext{
		WorkspaceRoot: tmp,
		BinDir:        binDir,
	}

	// 1. With ws-doctor present, it renders PASS.
	healthyOut := RenderWorkspaceView(ctx)
	if !strings.Contains(healthyOut, "[PASS] Git Identity: ok") {
		t.Fatalf("expected healthy ws-doctor output before rename, got:\n%s", healthyOut)
	}

	// 2. Rename ws-doctor binary -> Workspace view must show unavailable, not green.
	if err := os.Rename(docBin, docBin+".renamed"); err != nil {
		t.Fatalf("Rename(ws-doctor): %v", err)
	}

	deadOut := RenderWorkspaceView(ctx)
	if !strings.Contains(deadOut, "unavailable: ws-doctor --json exit ") {
		t.Errorf("expected Workspace view to show 'unavailable: ws-doctor --json exit ...' when binary is missing, got:\n%s", deadOut)
	}
	if strings.Contains(deadOut, "all_passed: true") || strings.Contains(deadOut, "[PASS]") {
		t.Errorf("Workspace view still rendered green/PASS after ws-doctor was killed:\n%s", deadOut)
	}
}

// TestProjectAndToolMatrixViews_RenderManifestsAndEvidenceLevels verifies that
// Project view reads `<checkout>/.ws-render/manifest.json` + `ws-agents-lint --json`
// and Tool Matrix view labels rows with evidence levels (E1/E2/E3) from the
// Knowledge Pack ceiling table.
func TestProjectAndToolMatrixViews_RenderManifestsAndEvidenceLevels(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("MkdirAll(binDir): %v", err)
	}

	// Create a checkout with .ws-render/manifest.json
	coDir := filepath.Join(tmp, "demo-repo", ".ws-render")
	if err := os.MkdirAll(coDir, 0755); err != nil {
		t.Fatalf("MkdirAll(coDir): %v", err)
	}
	manifestJSON := `{"adapter":"rules-v2","repo_id":"demo-repo","workspace_id":"zitian"}`
	if err := os.WriteFile(filepath.Join(coDir, "manifest.json"), []byte(manifestJSON), 0644); err != nil {
		t.Fatalf("WriteFile(manifest.json): %v", err)
	}

	// Create .ws/mcp-runtime.json
	wsDotDir := filepath.Join(tmp, ".ws")
	if err := os.MkdirAll(wsDotDir, 0755); err != nil {
		t.Fatalf("MkdirAll(.ws): %v", err)
	}
	runtimeJSON := `{"version":1,"servers":{"basic-memory":{"command":"omca"}}}`
	if err := os.WriteFile(filepath.Join(wsDotDir, "mcp-runtime.json"), []byte(runtimeJSON), 0644); err != nil {
		t.Fatalf("WriteFile(mcp-runtime.json): %v", err)
	}

	// Fake ws-agents-lint and omca hub status
	lintBin := filepath.Join(binDir, "ws-agents-lint")
	lintScript := `#!/bin/sh
printf '{"checked":1,"findings":[]}\n'
`
	if err := os.WriteFile(lintBin, []byte(lintScript), 0755); err != nil {
		t.Fatalf("WriteFile(ws-agents-lint): %v", err)
	}

	omcaBin := filepath.Join(binDir, "omca")
	omcaScript := `#!/bin/sh
printf '{"uptime":1000,"connected_hosts":[],"active_workers":[],"tools":{"zitian:basic-memory":{"status":"RUNNING"}}}\n'
`
	if err := os.WriteFile(omcaBin, []byte(omcaScript), 0755); err != nil {
		t.Fatalf("WriteFile(omca): %v", err)
	}

	ctx := DevEnvContext{
		WorkspaceRoot: tmp,
		BinDir:        binDir,
	}

	projOut := RenderProjectView(ctx)
	if !strings.Contains(projOut, "demo-repo  adapter=rules-v2 repo=demo-repo workspace=zitian") {
		t.Errorf("Project view did not render checkout manifest:\n%s", projOut)
	}
	if !strings.Contains(projOut, "checked: 1 repo(s), findings: 0") {
		t.Errorf("Project view did not render ws-agents-lint output:\n%s", projOut)
	}

	matrixOut := RenderToolMatrixView(ctx)
	for _, want := range []string{
		"zitian:basic-memory",
		"[evidence=E3]",
		"basic-memory",
		"[evidence=E1]",
		"[E2]",
	} {
		if !strings.Contains(matrixOut, want) {
			t.Errorf("Tool Matrix view missing %q:\n%s", want, matrixOut)
		}
	}
}
