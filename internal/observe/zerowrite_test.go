package observe

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/qualify"
)

// TestObserve_NeverImportsOSExec is a static proof of "zero exec": it asks
// the Go toolchain (not this package's own code) which packages the
// production (non-test) internal/observe package directly imports.
// Deliberately `.Imports` (direct imports only), not `-deps`'s transitive
// closure: internal/observe imports internal/context for HostDetection/
// NativeHome, and internal/context itself legitimately imports os/exec for
// its own single, safety-bounded `--version` probe (host.go) — that
// dependency existing somewhere in the transitive graph says nothing about
// whether internal/observe's own code ever calls exec.Command. Checking
// direct imports instead answers the actual question this test is for:
// does this package's own source ever import os/exec at all. Since it
// never does, there is no code path — now or after an unnoticed future
// edit that adds a NEW direct import — that could invoke a subprocess.
// This is a stronger guarantee than a runtime canary alone (see
// TestObserve_ZeroWriteZeroExec_SandboxedRealisticHome below): a canary
// only proves "did not execute this one planted script during this one
// test run," while this proves the package structurally cannot execute
// anything, ever. Modeled on internal/plugin/importboundary_test.go's
// TestImportBoundary, which uses the analogous `go list` technique for a
// different import-boundary rule (that one does need the transitive `-deps`
// form, since it must catch an indirect path to internal/adapters too).
func TestObserve_NeverImportsOSExec(t *testing.T) {
	const pkg = "github.com/wangzitian0/oh-my-code-agent/internal/observe"

	out, err := exec.Command("go", "list", "-f", "{{.Imports}}", pkg).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("go list -f {{.Imports}} %s: %v\nstderr:\n%s", pkg, err, exitErr.Stderr)
		}
		t.Fatalf("go list -f {{.Imports}} %s: %v", pkg, err)
	}

	trimmed := strings.TrimSpace(string(out))
	trimmed = strings.TrimPrefix(trimmed, "[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	imports := strings.Fields(trimmed)
	if len(imports) == 0 {
		t.Fatal("go list reported no imports at all; this check would vacuously pass")
	}
	for _, imp := range imports {
		if imp == "os/exec" {
			t.Fatal("internal/observe (production code) must never directly import os/exec — package observe discovers and inventories coding-agent sources without executing them (doc.go)")
		}
	}
}

// realisticSandboxedCodexHome populates a qualify.Sandbox with this repo's
// own already-committed, realistic Codex fixture inputs (PR-06's
// instructions-collision, skill-collision, and mcp-merge cases), combined
// into one sandbox so a single Observe call exercises Instructions, Skills,
// and MCP sources at once — a genuine "sandboxed copy of a real host home
// layout" per this PR's acceptance criterion, not a fabricated stand-in.
func realisticSandboxedCodexHome(t *testing.T, sb *qualify.Sandbox) {
	t.Helper()
	fixturesRoot := repoFixturesDir()
	for _, c := range []string{"instructions-collision", "skill-collision", "mcp-merge"} {
		inputDir := filepath.Join(fixturesRoot, "codex", "0.144.5", c, "input")
		if err := sb.PopulateFromInput(inputDir); err != nil {
			t.Fatalf("PopulateFromInput(%s): %v", inputDir, err)
		}
	}
}

// realisticSandboxedClaudeHome is realisticSandboxedCodexHome's Claude Code
// counterpart.
func realisticSandboxedClaudeHome(t *testing.T, sb *qualify.Sandbox) {
	t.Helper()
	fixturesRoot := repoFixturesDir()
	for _, c := range []string{"instructions-collision", "skill-collision", "mcp-merge"} {
		inputDir := filepath.Join(fixturesRoot, "claude-code", "2.1.211", c, "input")
		if err := sb.PopulateFromInput(inputDir); err != nil {
			t.Fatalf("PopulateFromInput(%s): %v", inputDir, err)
		}
	}
}

// TestObserve_ZeroWriteZeroExec_SandboxedRealisticHome is the dynamic half
// of the "zero-write/zero-exec proof runs in CI against sandboxed copies of
// real host home layouts" acceptance criterion. It reuses
// internal/qualify's Sandbox and snapshot/diff machinery exactly as
// qualify/doc.go invites PR-08 to ("PR-08's real observation code is
// expected to reuse Sandbox, snapshot/diff, and the Observation-building
// helpers rather than re-deriving them") rather than reinventing an
// equivalent proof:
//
//  1. Build a fresh Sandbox and populate it with this repo's own committed,
//     realistic fixture home layouts (never the real machine's ~/.codex or
//     ~/.claude — see fixtures/README.md's safety boundary, which matters
//     doubly here since the `claude` binary running this very test suite is
//     itself a Claude Code session).
//  2. Plant an executable canary script under Sandbox.Outside
//     (PlantOutsideCanary) — it only ever produces its marker file if
//     something actually executes it.
//  3. Snapshot every populated root before calling Observe.
//  4. Call Observe over the sandbox's paths.
//  5. Snapshot again and diff: an empty diff is the zero-write proof; the
//     canary marker still not existing is a second, independent zero-exec
//     signal alongside the static import-boundary check above.
func TestObserve_ZeroWriteZeroExec_SandboxedRealisticHome(t *testing.T) {
	for _, host := range []string{"codex", "claude-code"} {
		t.Run(host, func(t *testing.T) {
			sb, err := qualify.NewSandbox(t.TempDir(), host)
			if err != nil {
				t.Fatalf("NewSandbox: %v", err)
			}

			// PR-16 (issue #20): a synthetic system/managed root, exercising
			// the same new code paths as the rest of this test — never the
			// real machine's /etc/codex or managed policy directory (see
			// system.go's SystemRoot doc comment).
			systemRoot := filepath.Join(sb.Root, "system-root")
			workingDir := filepath.Join(sb.Project, "nested", "dir")

			var req Request
			switch host {
			case "codex":
				realisticSandboxedCodexHome(t, sb)
				// New PR-16 source-reading code paths: multiplexed hook/
				// policy tags on config.toml (already populated by
				// realisticSandboxedCodexHome's mcp-merge fixture), a
				// discoverOnly credential file, a plugin.json marker, a
				// system root, and a nested directory-chain level.
				mustWriteFile(t, filepath.Join(sb.CodexHome, "auth.json"), `{"OPENAI_API_KEY":"sandboxed-not-a-real-secret"}`)
				mustWriteFile(t, filepath.Join(sb.CodexHome, "plugins", "demo", ".codex-plugin", "plugin.json"), `{"name":"demo"}`)
				mustWriteFile(t, filepath.Join(systemRoot, "config.toml"), "approval_policy = \"never\"\n")
				mustWriteFile(t, filepath.Join(systemRoot, "skills", "audited", "SKILL.md"), "---\nname: audited\n---\n")
				mustWriteFile(t, filepath.Join(workingDir, "AGENTS.md"), "# nested\n")

				req = Request{
					Detection: hostcontext.HostDetection{
						Host:    "codex",
						Surface: "cli",
						Version: "0.144.5",
						NativeHomes: []hostcontext.NativeHome{
							{Name: "CODEX_HOME", Path: sb.CodexHome},
							{Name: "HOME/.agents/skills", Path: filepath.Join(sb.Home, ".agents", "skills")},
						},
					},
					WorktreeRoot:     sb.Project,
					SystemRoots:      []SystemRoot{{Name: "ETC_CODEX", Path: systemRoot}},
					WorkingDirectory: workingDir,
					SessionInputs: []SessionInput{
						{Concept: conceptMCPServer, Kind: "flag", Name: "-c mcp_servers.x.command", Value: "./x"},
					},
				}
			case "claude-code":
				realisticSandboxedClaudeHome(t, sb)
				mustWriteFile(t, filepath.Join(sb.ClaudeConfigDir, "settings.json"), `{"hooks":{"PreToolUse":[]},"permissions":{"deny":[]},"enabledPlugins":[]}`)
				mustWriteFile(t, filepath.Join(sb.Project, "CLAUDE.local.md"), "# local, gitignored\n")
				mustWriteFile(t, filepath.Join(systemRoot, "CLAUDE.md"), "# managed\n")
				mustWriteFile(t, filepath.Join(systemRoot, "managed-settings.json"), `{"permissions":{"deny":[]}}`)
				mustWriteFile(t, filepath.Join(workingDir, "CLAUDE.md"), "# nested\n")

				req = Request{
					Detection: hostcontext.HostDetection{
						Host:    "claude-code",
						Surface: "cli",
						Version: "2.1.211",
						NativeHomes: []hostcontext.NativeHome{
							{Name: "CLAUDE_CONFIG_DIR", Path: sb.ClaudeConfigDir},
							{Name: "HOME/.agents/skills", Path: filepath.Join(sb.Home, ".agents", "skills")},
						},
					},
					WorktreeRoot:     sb.Project,
					SystemRoots:      []SystemRoot{{Name: "CLAUDE_MANAGED", Path: systemRoot}},
					WorkingDirectory: workingDir,
					SessionInputs: []SessionInput{
						{Concept: conceptMCPServer, Kind: "flag", Name: "--mcp-config", Value: "extra.json"},
					},
				}
			}

			canaryMarker, err := sb.PlantOutsideCanary()
			if err != nil {
				t.Fatalf("PlantOutsideCanary: %v", err)
			}

			var watched []string
			for _, p := range []string{sb.Home, sb.CodexHome, sb.ClaudeConfigDir, sb.Project, sb.Outside, systemRoot} {
				if p != "" {
					watched = append(watched, p)
				}
			}
			before, err := qualify.SnapshotRealHome(watched)
			if err != nil {
				t.Fatalf("SnapshotRealHome (before): %v", err)
			}

			obs, err := Observe(req)
			if err != nil {
				t.Fatalf("Observe: %v", err)
			}
			if len(obs) == 0 {
				t.Fatal("Observe returned no observations against a populated realistic fixture home; this proof would be vacuous")
			}
			assertValid(t, obs)

			after, err := qualify.SnapshotRealHome(watched)
			if err != nil {
				t.Fatalf("SnapshotRealHome (after): %v", err)
			}

			diffs := qualify.DiffRealHomeSnapshots(before, after)
			if len(diffs) != 0 {
				t.Errorf("Observe wrote to the sandboxed home it was only supposed to read:\n%s", strings.Join(diffs, "\n"))
			}

			if _, statErr := os.Stat(canaryMarker); statErr == nil {
				t.Error("Observe executed the planted canary script — zero-exec proof failed")
			} else if !os.IsNotExist(statErr) {
				t.Errorf("unexpected error checking canary marker: %v", statErr)
			}
		})
	}
}
