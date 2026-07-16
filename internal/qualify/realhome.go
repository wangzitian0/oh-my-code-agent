package qualify

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RealHomeCheckEnvVar gates the opt-in check that snapshot-diffs the actual
// real host config paths (not a sandbox stand-in) around a fixture run. It
// defaults to off.
//
// Why opt-in rather than the default: the automated, always-on zero-write
// proof (Sandbox.Outside, see harness.go and doc.go) already gives a
// deterministic, CI-safe guarantee that this harness's own env redirection
// is never bypassed. A literal real-home diff is a stronger, more direct
// statement of the same property, but on a live developer machine that has
// real Codex/Claude Code sessions running concurrently (including, on the
// machine this PR was authored on, the very session authoring it), the
// documented config paths receive ambient, legitimate background writes
// unrelated to this test — for example, Claude Code's own settings.json was
// observed to change within a two-invocation window with no sandboxed
// subprocess of this harness ever pointed at it. Asserting byte-for-byte
// equality of the real environment on every `go test ./...` run would make
// the suite flaky for a reason that has nothing to do with a real isolation
// bug. This check exists for a maintainer to run explicitly (and for this
// PR's own evidence trail, see fixtures/README.md) rather than gating CI.
const RealHomeCheckEnvVar = "OMCA_QUALIFY_VERIFY_REAL_HOME"

// RealHomeCheckEnabled reports whether the opt-in real-home check should
// run in this process.
func RealHomeCheckEnabled() bool {
	return os.Getenv(RealHomeCheckEnvVar) != ""
}

// RealHomePaths returns the specific real config paths
// docs/architecture/runtime.md §7.1 (Codex) / §7.2 (Claude Code) name as
// what an adapter must inventory, rooted at realHome. It deliberately
// excludes session/log/cache/state paths (runtime.md §9, "Mutable State"):
// those are expected to change continuously from ordinary host use
// independent of anything this harness does, so including them would turn
// this into a test of "is the machine idle" rather than "did our code write
// outside its sandbox."
func RealHomePaths(host, realHome string) []string {
	switch host {
	case "codex":
		return []string{
			filepath.Join(realHome, ".codex", "config.toml"),
			filepath.Join(realHome, ".codex", "AGENTS.md"),
			filepath.Join(realHome, ".codex", "AGENTS.override.md"),
			filepath.Join(realHome, ".codex", "skills"),
			filepath.Join(realHome, ".agents", "skills"),
			filepath.Join("/etc", "codex"),
		}
	case "claude-code":
		return []string{
			filepath.Join(realHome, ".claude", "settings.json"),
			filepath.Join(realHome, ".claude", "CLAUDE.md"),
			filepath.Join(realHome, ".claude", "rules"),
			filepath.Join(realHome, ".claude", "skills"),
			filepath.Join(realHome, ".claude", "agents"),
			filepath.Join(realHome, ".agents", "skills"),
		}
	default:
		return nil
	}
}

// RealHomeSnapshot is a composite before/after snapshot across every path
// RealHomePaths names.
type RealHomeSnapshot map[string]snapshot

// SnapshotRealHome snapshots every path in paths (each individually, since
// they need not share a common parent — e.g. `/etc/codex` alongside
// `$HOME/.codex/...`). A path that does not exist snapshots as empty, not an
// error (see snapshotTree).
func SnapshotRealHome(paths []string) (RealHomeSnapshot, error) {
	out := make(RealHomeSnapshot, len(paths))
	for _, p := range paths {
		snap, err := snapshotTree(p)
		if err != nil {
			return nil, fmt.Errorf("qualify: SnapshotRealHome: %w", err)
		}
		out[p] = snap
	}
	return out, nil
}

// DiffRealHomeSnapshots reports every difference across every watched path,
// prefixed with the path so a human can see exactly which real config
// location changed. An empty result is the real-home zero-write proof.
func DiffRealHomeSnapshots(before, after RealHomeSnapshot) []string {
	var diffs []string
	for path, beforeSnap := range before {
		afterSnap := after[path]
		for _, d := range diffSnapshots(beforeSnap, afterSnap) {
			diffs = append(diffs, fmt.Sprintf("%s: %s", path, d))
		}
	}
	sort.Strings(diffs)
	return diffs
}
