package plugin

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// hostSwitchInventory is the file set docs/architecture/README.md §9.1
// documents as owning host-specific behavior outside the adapter contract.
//
// It is an inventory, not an allowlist of good practice: every entry is a
// place the frozen HostAdapter contract was supposed to own and does not.
// The list is pinned here so that its growth is a deliberate, reviewed act.
// Adding a `switch host` branch in a twelfth file means the migration this
// inventory scopes just got larger, which is exactly the kind of change that
// should not land as an incidental side effect of a feature — the same
// discipline docs/README.md's change rules state for a conditional
// invariant.
//
// Shrinking it is the goal. When a package's host branches move behind a
// real adapter, delete its entry here and its row in §9.1.
var hostSwitchInventory = []string{
	"cmd/omca/qualify_tui.go",
	"cmd/omca/run.go",
	"internal/auth/invoke.go",
	"internal/auth/mutablestate.go",
	"internal/context/host.go",
	"internal/domain/host_tier.go",
	"internal/observe/request.go",
	"internal/observe/system.go",
	"internal/qualify/realhome.go",
	"internal/qualify/sandbox.go",
	"internal/runtime/compile.go",
}

// hostSwitchMarkers are the literal switch-case forms that identify a
// host-specific branch. Canonical host IDs are the only thing matched: a
// string that merely mentions a host (an error message, a doc comment) is
// not a branch and must not inflate the inventory.
var hostSwitchMarkers = []string{`case "codex"`, `case "claude-code"`}

// TestHostSwitchInventory keeps docs/architecture/README.md §9.1 honest.
//
// The architecture documents an adapter contract that no production code
// drives: internal/plugin is imported by no production file outside itself,
// and the only HostAdapter implementations are the out-of-process transport
// client and a test double. Host semantics live in hardcoded switches
// instead. TestImportBoundary already guards the seam that does exist; this
// guards the one that does not, by failing when the hardcoded surface grows
// or shrinks without the documented inventory being updated to match.
//
// Without it, §9.1 is a snapshot that silently rots, and "how much work is
// the adapter migration" becomes unanswerable again.
func TestHostSwitchInventory(t *testing.T) {
	// Enumerate through `go list` rather than walking the tree, for the same
	// reason TestImportBoundary does: it yields exactly this module's own
	// non-test Go files. A filesystem walk also picks up unrelated checkouts
	// that happen to sit inside the working copy (an agent worktree under
	// .claude/, a vendored clone), which would report phantom violations
	// that no edit to this repository can fix.
	const modulePrefix = "github.com/wangzitian0/oh-my-code-agent/"
	rootOut := runGoList(t, "-m", "-f", "{{.Dir}}")
	root := strings.TrimSpace(string(rootOut))
	if root == "" {
		t.Fatal("go list -m returned no module directory")
	}

	listed := runGoList(t, "-f", `{{$d := .Dir}}{{range .GoFiles}}{{$d}}/{{.}}{{"\n"}}{{end}}`, modulePrefix+"...")

	// Split on newlines, not whitespace: the template above emits one path
	// per line precisely so a module directory containing a space (common
	// enough on developer machines) does not get torn into fragments that
	// fail this test for a reason no source change can fix.
	found := map[string]bool{}
	for _, line := range strings.Split(string(listed), "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(content)
		for _, marker := range hostSwitchMarkers {
			if strings.Contains(text, marker) {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					t.Fatalf("relativize %s: %v", path, err)
				}
				found[filepath.ToSlash(rel)] = true
				break
			}
		}
	}
	if len(found) == 0 {
		t.Fatal("no host switches found anywhere; the markers must have changed shape, so this check would vacuously pass")
	}

	expected := map[string]bool{}
	for _, f := range hostSwitchInventory {
		expected[f] = true
	}

	var added, removed []string
	for f := range found {
		if !expected[f] {
			added = append(added, f)
		}
	}
	for f := range expected {
		if !found[f] {
			removed = append(removed, f)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)

	for _, f := range added {
		t.Errorf("undocumented host switch in %s: host-specific behavior outside the adapter contract must be listed in hostSwitchInventory and in docs/architecture/README.md §9.1, so the size of the adapter migration stays a known number", f)
	}
	for _, f := range removed {
		t.Errorf("%s no longer contains a host switch: if its branches moved behind the adapter contract, delete it from hostSwitchInventory and from the §9.1 table (this is the good direction)", f)
	}
}
