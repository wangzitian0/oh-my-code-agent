package plugin

import (
	"os/exec"
	"strings"
	"testing"
)

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

	out, err := exec.Command("go", "list", modulePrefix+"internal/...").Output()
	if err != nil {
		t.Fatalf("go list %sinternal/...: %v", modulePrefix, err)
	}
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

		depsOut, err := exec.Command("go", "list", "-deps", pkg).Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", pkg, err)
		}
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
