package runtime

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestBootstrapRequest_Validate_RejectsMismatchedObservationHost is a
// regression test for a real gap Copilot's review of this PR caught:
// validate() checked an observation's host ID against req.Detection.Host
// but not its version or surface. An observation gathered under a stale
// host version (e.g. after an upgrade, if a caller reused a cached Observe
// result against a freshly re-detected HostDetection) would otherwise
// silently pass validation, letting GenerationID digest req.Detection.Version
// while the actual observed sources came from a different version --
// producing a manifest that cannot be reproduced or verified from its own
// stated inputs. This covers all three mismatch axes: host ID, version, and
// surface.
func TestBootstrapRequest_Validate_RejectsMismatchedObservationHost(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	wt := tr.worktree(t)

	t.Run("mismatched version", func(t *testing.T) {
		det := tr.detection("0.144.5")
		det.Version = "0.145.0" // observations were gathered under 0.144.5
		req := BootstrapRequest{Detection: det, Worktree: wt, Observations: obs, Now: now}
		if err := req.validate(); err == nil {
			t.Fatal("validate() accepted observations gathered under a different host version than req.Detection.Version; want an error")
		} else if !strings.Contains(err.Error(), "version") {
			t.Errorf("error = %q, want it to mention the version mismatch", err.Error())
		}
	})

	t.Run("mismatched surface", func(t *testing.T) {
		det := tr.detection("0.144.5")
		det.Surface = "vscode-extension" // observations were gathered under "cli"
		req := BootstrapRequest{Detection: det, Worktree: wt, Observations: obs, Now: now}
		if err := req.validate(); err == nil {
			t.Fatal("validate() accepted observations gathered under a different surface than req.Detection.Surface; want an error")
		} else if !strings.Contains(err.Error(), "surface") {
			t.Errorf("error = %q, want it to mention the surface mismatch", err.Error())
		}
	})

	t.Run("mismatched host ID", func(t *testing.T) {
		claudeTr := newClaudeFixtureTree(t)
		req := BootstrapRequest{Detection: claudeTr.detection("2.1.211"), Worktree: wt, Observations: obs, Now: now}
		if err := req.validate(); err == nil {
			t.Fatal("validate() accepted codex observations against a claude-code Detection; want an error")
		}
	})

	t.Run("matching request is accepted", func(t *testing.T) {
		req := BootstrapRequest{Detection: tr.detection("0.144.5"), Worktree: wt, Observations: obs, Now: now}
		if err := req.validate(); err != nil {
			t.Fatalf("validate() rejected a correctly-matched request: %v", err)
		}
	})
}
