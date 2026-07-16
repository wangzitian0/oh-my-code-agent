package plugin

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// runGoList runs `go list` and includes stderr in the failure message: a
// bare exec error (usually just an exit status) hides *why* go list failed
// (missing files, invalid build tags, module resolution errors), which
// matters most exactly when CI hits this check.
func runGoList(t *testing.T, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("go", append([]string{"list"}, args...)...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("go list %s: %v\nstderr:\n%s", strings.Join(args, " "), err, exitErr.Stderr)
		}
		t.Fatalf("go list %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// TestImportBoundary enforces docs/architecture/README.md §9 and ADR 0005:
// core packages reach adapters only through this contract package. No
// internal package other than internal/plugin itself (and the adapters'
// own package tree) may import internal/adapters/* directly.
//
// It shells out to `go list`, part of the Go toolchain rather than a new
// dependency, so this stays within the no-third-party-deps constraint.
//
// This check was verified to actually catch a violation during development:
// a temporary `import _ "github.com/wangzitian0/oh-my-code-agent/internal/adapters/codex"`
// added to internal/observe/doc.go made this test fail with exactly the
// violation message below; the import was then removed.
func TestImportBoundary(t *testing.T) {
	const modulePrefix = "github.com/wangzitian0/oh-my-code-agent/"
	const pluginPkg = modulePrefix + "internal/plugin"
	const adaptersPrefix = modulePrefix + "internal/adapters/"

	out := runGoList(t, modulePrefix+"internal/...")
	pkgs := strings.Fields(string(out))
	if len(pkgs) == 0 {
		t.Fatal("go list returned no internal packages; the check would vacuously pass")
	}

	checked := 0
	for _, pkg := range pkgs {
		if pkg == pluginPkg || strings.HasPrefix(pkg, adaptersPrefix) {
			// The contract package is the intended seam, and adapters
			// obviously import their own package tree.
			continue
		}
		checked++

		depsOut := runGoList(t, "-deps", pkg)
		for _, dep := range strings.Fields(string(depsOut)) {
			if strings.HasPrefix(dep, adaptersPrefix) {
				t.Errorf("import boundary violation: %s depends on %s; core packages must reach adapters only through %s", pkg, dep, pluginPkg)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no packages were subject to the import boundary check; the check would vacuously pass")
	}
}
