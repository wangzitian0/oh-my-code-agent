// devenv_views.go implements issue #126 (Epic infra2#820 · Stream H3):
// absorbing dev_env `ws-tui`'s three views (Workspace, Project, Tool matrix)
// into OMCA's TUI, each backed by its canonical dev_env JSON command / file
// source and showing that exact command in its header.
//
// Invariant: No view computes a verdict of its own. A missing or failing
// command renders as "unavailable: <command> exit <N>" (or exit -1 when the
// binary cannot be started), never as empty-green.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/assurance"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/hub"
)

// DevEnvViewSpec maps one absorbed ws-tui view to its single canonical header
// command and backing data sources (issue #126).
type DevEnvViewSpec struct {
	View    ViewKind
	Title   string
	Command string
}

// DevEnvViewSpecs is the closed table of absorbed ws-tui views and their
// canonical header commands (issue #126's table test contract: every view ->
// exactly one dev_env command, and the header text equals it).
var DevEnvViewSpecs = []DevEnvViewSpec{
	{
		View:    ViewWorkspace,
		Title:   "Workspace",
		Command: "ws-check-drift --json | ws-doctor --json",
	},
	{
		View:    ViewProject,
		Title:   "Project",
		Command: "ws-agents-lint --json",
	},
	{
		View:    ViewToolMatrix,
		Title:   "Tool Matrix",
		Command: "omca hub status --json | .ws/mcp-runtime.json",
	},
}

// DevEnvCommandRunner executes a command in dir and returns stdout bytes, exit
// code, and error. Injected in tests to simulate binary removal or non-zero
// exit without touching host binaries.
type DevEnvCommandRunner func(dir string, name string, args ...string) ([]byte, int, error)

// DevEnvContext supplies the workspace root, binary lookup directory, and
// optional command runner for the three absorbed ws-tui views.
type DevEnvContext struct {
	WorkspaceRoot string
	BinDir        string
	SocketPath    string
	Runner        DevEnvCommandRunner
}

// DefaultDevEnvCommandRunner runs name with args inside dir with a 5-second
// timeout, resolving name against BinDir first when configured.
func (c DevEnvContext) runCommand(name string, args ...string) ([]byte, int, error) {
	if c.Runner != nil {
		return c.Runner(c.WorkspaceRoot, name, args...)
	}
	var bin string
	if c.BinDir != "" {
		cand := filepath.Join(c.BinDir, name)
		if _, err := os.Stat(cand); err == nil {
			bin = cand
		} else {
			return nil, 127, fmt.Errorf("%s not found in %s", name, c.BinDir)
		}
	} else if p, err := exec.LookPath(name); err == nil {
		bin = p
	} else if home, herr := os.UserHomeDir(); herr == nil {
		cand := filepath.Join(home, ".local", "bin", name)
		if _, err := os.Stat(cand); err == nil {
			bin = cand
		} else {
			return nil, 127, err
		}
	} else {
		return nil, 127, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	if c.WorkspaceRoot != "" {
		if info, err := os.Stat(c.WorkspaceRoot); err == nil && info.IsDir() {
			cmd.Dir = c.WorkspaceRoot
		}
	}
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return out, exitErr.ExitCode(), err
		}
		return out, 127, err
	}
	return out, 0, nil
}

// HeaderForDevEnvView returns the canonical command string shown in the header
// of one of the three absorbed ws-tui views.
func HeaderForDevEnvView(v ViewKind) (string, bool) {
	for _, spec := range DevEnvViewSpecs {
		if spec.View == v {
			return spec.Command, true
		}
	}
	return "", false
}

// RenderWorkspaceView renders the absorbed Workspace view backed by
// `ws-check-drift --json` and `ws-doctor --json`. Neither command's failure is
// ever rendered as green; any missing or non-zero command prints
// `unavailable: <command> exit <N>`.
func RenderWorkspaceView(ctx DevEnvContext) string {
	header, _ := HeaderForDevEnvView(ViewWorkspace)
	var b strings.Builder
	fmt.Fprintf(&b, "Header: %s\n\n", header)

	// 1. ws-check-drift --json
	driftArgs := []string{"--json"}
	if ctx.WorkspaceRoot != "" {
		driftArgs = []string{"--json", "--workspace", ctx.WorkspaceRoot}
	}
	driftOut, driftExit, driftErr := ctx.runCommand("ws-check-drift", driftArgs...)
	fmt.Fprintln(&b, "Source: ws-check-drift --json")
	if driftErr != nil || driftExit != 0 {
		code := driftExit
		if code == 0 {
			code = 1
		}
		fmt.Fprintf(&b, "  unavailable: ws-check-drift --json exit %d\n", code)
	} else {
		var payload struct {
			Workspace  string   `json:"workspace"`
			Path       string   `json:"path"`
			DriftFound bool     `json:"drift_found"`
			Messages   []string `json:"messages"`
			Skills     []string `json:"skills"`
		}
		if err := json.Unmarshal(driftOut, &payload); err != nil {
			fmt.Fprintf(&b, "  unavailable: ws-check-drift --json exit 1 (invalid JSON: %v)\n", err)
		} else {
			fmt.Fprintf(&b, "  workspace: %s (%s)\n", payload.Workspace, payload.Path)
			fmt.Fprintf(&b, "  drift_found: %v (messages=%d, skills=%d)\n",
				payload.DriftFound, len(payload.Messages), len(payload.Skills))
			for _, msg := range payload.Messages {
				fmt.Fprintf(&b, "    - %s\n", msg)
			}
		}
	}

	// 2. ws-doctor --json
	fmt.Fprintln(&b, "\nSource: ws-doctor --json")
	docOut, docExit, docErr := ctx.runCommand("ws-doctor", "--json")
	if docErr != nil || docExit != 0 {
		code := docExit
		if code == 0 {
			code = 1
		}
		fmt.Fprintf(&b, "  unavailable: ws-doctor --json exit %d\n", code)
	} else {
		var docPayload struct {
			AllPassed bool `json:"all_passed"`
			Checks    []struct {
				Name   string `json:"name"`
				OK     *bool  `json:"ok"`
				Detail string `json:"detail"`
			} `json:"checks"`
		}
		if err := json.Unmarshal(docOut, &docPayload); err != nil {
			fmt.Fprintf(&b, "  unavailable: ws-doctor --json exit 1 (invalid JSON: %v)\n", err)
		} else {
			fmt.Fprintf(&b, "  all_passed: %v (%d check(s))\n", docPayload.AllPassed, len(docPayload.Checks))
			for _, ch := range docPayload.Checks {
				state := "INFO"
				if ch.OK != nil {
					if *ch.OK {
						state = "PASS"
					} else {
						state = "FAIL"
					}
				}
				fmt.Fprintf(&b, "    [%s] %s: %s\n", state, ch.Name, ch.Detail)
			}
		}
	}

	return b.String()
}

// RenderProjectView renders the absorbed Project view backed by per-checkout
// `<checkout>/.ws-render/manifest.json` (or `.ws-publish/manifest.json`) and
// `ws-agents-lint --json`.
func RenderProjectView(ctx DevEnvContext) string {
	header, _ := HeaderForDevEnvView(ViewProject)
	var b strings.Builder
	fmt.Fprintf(&b, "Header: %s\n\n", header)

	// 1. Per-checkout manifests
	fmt.Fprintln(&b, "Source: <checkout>/.ws-render/manifest.json")
	manifests := readCheckoutManifests(ctx.WorkspaceRoot)
	if len(manifests) == 0 {
		fmt.Fprintln(&b, "  unavailable: no checkout manifest.json found")
	} else {
		for _, m := range manifests {
			fmt.Fprintf(&b, "  %s  adapter=%s repo=%s workspace=%s\n",
				m.CheckoutName, m.Adapter, m.RepoID, m.WorkspaceID)
		}
	}

	// 2. ws-agents-lint --json
	fmt.Fprintln(&b, "\nSource: ws-agents-lint --json")
	lintArgs := []string{"--json"}
	if ctx.WorkspaceRoot != "" {
		lintArgs = []string{"--json", "--workspace", ctx.WorkspaceRoot}
	}
	lintOut, lintExit, lintErr := ctx.runCommand("ws-agents-lint", lintArgs...)
	if lintErr != nil || lintExit != 0 {
		code := lintExit
		if code == 0 {
			code = 1
		}
		fmt.Fprintf(&b, "  unavailable: ws-agents-lint --json exit %d\n", code)
	} else {
		var report struct {
			Checked  int `json:"checked"`
			Findings []struct {
				Repo   string `json:"repo"`
				Kind   string `json:"kind"`
				Detail string `json:"detail"`
			} `json:"findings"`
		}
		if err := json.Unmarshal(lintOut, &report); err != nil {
			fmt.Fprintf(&b, "  unavailable: ws-agents-lint --json exit 1 (invalid JSON: %v)\n", err)
		} else {
			fmt.Fprintf(&b, "  checked: %d repo(s), findings: %d\n", report.Checked, len(report.Findings))
			for _, f := range report.Findings {
				fmt.Fprintf(&b, "    - %s [%s]: %s\n", f.Repo, f.Kind, f.Detail)
			}
		}
	}

	return b.String()
}

type checkoutManifestSummary struct {
	CheckoutName string
	Adapter      string
	RepoID       string
	WorkspaceID  string
}

func readCheckoutManifests(wsRoot string) []checkoutManifestSummary {
	if wsRoot == "" {
		return nil
	}
	entries, err := os.ReadDir(wsRoot)
	if err != nil {
		return nil
	}
	var out []checkoutManifestSummary
	for _, d := range entries {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		co := filepath.Join(wsRoot, d.Name())
		for _, rel := range []string{
			filepath.Join(".ws-render", "manifest.json"),
			filepath.Join(".ws-publish", "manifest.json"),
		} {
			p := filepath.Join(co, rel)
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			var parsed struct {
				Adapter     string `json:"adapter"`
				RepoID      string `json:"repo_id"`
				WorkspaceID string `json:"workspace_id"`
			}
			if json.Unmarshal(data, &parsed) == nil {
				out = append(out, checkoutManifestSummary{
					CheckoutName: d.Name(),
					Adapter:      parsed.Adapter,
					RepoID:       parsed.RepoID,
					WorkspaceID:  parsed.WorkspaceID,
				})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckoutName < out[j].CheckoutName })
	return out
}

// RenderToolMatrixView renders the absorbed Tool matrix view backed by
// `omca hub status --json` (hub worker/tool list) and `.ws/mcp-runtime.json`
// resolution, labelled with the evidence level (E1/E2/E3) from the Knowledge
// Pack ceiling table (internal/assurance.Ceilings).
func RenderToolMatrixView(ctx DevEnvContext) string {
	header, _ := HeaderForDevEnvView(ViewToolMatrix)
	var b strings.Builder
	fmt.Fprintf(&b, "Header: %s\n\n", header)

	// 1. Hub worker & tool list (`omca hub status --json`)
	fmt.Fprintln(&b, "Source: omca hub status --json")
	hubOut, hubExit, hubErr := ctx.runCommand("omca", "hub", "status", "--json")
	if hubErr != nil || hubExit != 0 {
		code := hubExit
		if code == 0 {
			code = 1
		}
		fmt.Fprintf(&b, "  unavailable: omca hub status --json exit %d\n", code)
	} else {
		var snap hub.DashboardSnapshot
		var rawStatus struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(hubOut, &rawStatus) == nil && rawStatus.Status == "stopped" {
			fmt.Fprintln(&b, "  unavailable: omca hub status --json exit 1 (hub stopped)")
		} else if err := json.Unmarshal(hubOut, &snap); err != nil {
			fmt.Fprintf(&b, "  unavailable: omca hub status --json exit 1 (invalid JSON: %v)\n", err)
		} else {
			fmt.Fprintf(&b, "  connected_hosts: %d, active_workers: %d\n", len(snap.Connected), len(snap.ActiveWorkers))
			names := make([]string, 0, len(snap.Tools))
			for name := range snap.Tools {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				ts := snap.Tools[name]
				fmt.Fprintf(&b, "    tool %-28s status=%-10s [evidence=%s]\n",
					name, ts.Status, domain.EvidenceLevelHostReported)
			}
		}
	}

	// 2. .ws/mcp-runtime.json resolution
	fmt.Fprintln(&b, "\nSource: .ws/mcp-runtime.json")
	if ctx.WorkspaceRoot == "" {
		fmt.Fprintln(&b, "  unavailable: .ws/mcp-runtime.json exit 1 (no workspace root)")
	} else {
		runtimePath := filepath.Join(ctx.WorkspaceRoot, ".ws", "mcp-runtime.json")
		data, err := os.ReadFile(runtimePath)
		if err != nil {
			fmt.Fprintln(&b, "  unavailable: .ws/mcp-runtime.json exit 1")
		} else {
			var spec struct {
				Version int `json:"version"`
				Servers map[string]struct {
					Command string `json:"command"`
				} `json:"servers"`
			}
			if err := json.Unmarshal(data, &spec); err != nil {
				fmt.Fprintf(&b, "  unavailable: .ws/mcp-runtime.json exit 1 (invalid JSON: %v)\n", err)
			} else {
				sNames := make([]string, 0, len(spec.Servers))
				for s := range spec.Servers {
					sNames = append(sNames, s)
				}
				sort.Strings(sNames)
				for _, s := range sNames {
					fmt.Fprintf(&b, "    server %-26s cmd=%s [evidence=%s]\n",
						s, spec.Servers[s].Command, domain.EvidenceLevelParsed)
				}
			}
		}
	}

	// 3. Host Knowledge Pack Evidence Ceilings (E1/E2/E3)
	fmt.Fprintln(&b, "\nKnowledge Pack Evidence Levels (E1/E2/E3):")
	for _, c := range assurance.Ceilings {
		fmt.Fprintf(&b, "  %-12s %-12s [%s]\n", c.Host, c.Concept, c.Ceiling)
	}

	return b.String()
}
