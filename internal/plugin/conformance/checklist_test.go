package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// knownSubChecks is the qualification checklist's own source of truth for
// which conformance.Run sub-checks exist today
// (docs/plugin/authoring-guide.md's "Qualification checklist" section
// declares one checklist item per entry here, by name). It is a plain,
// manually maintained list rather than something derived automatically from
// Run's body, so that TestRun_SubChecksMatchKnownList below has something
// independent to compare Run's actual body against -- the whole point of a
// drift guard is that the two sides are written separately and then checked
// for agreement, not that one mechanically generates the other.
//
// Whenever a sub-check is added to, renamed in, or removed from Run's body
// (conformance.go), update this list AND docs/plugin/authoring-guide.md's
// checklist table in the same change. TestRun_SubChecksMatchKnownList fails
// if this list and Run's actual body disagree; TestRun_SubChecksMatchDocumentedChecklist
// fails if this list and the guide's checklist disagree. Between the two, a
// conformance sub-check can never silently exist without a corresponding,
// accurate checklist item.
var knownSubChecks = []string{
	"runID",
	"runDetect",
	"runCapabilities",
	"runNotDetectedTaxonomy",
	"runObserveZeroSideEffects",
	"runResolveCompileVerifyLaunch",
}

// runSubChecksFromSource parses this package's own conformance.go source
// (not the running binary -- the .go file on disk) and returns the
// bare-identifier function calls made directly in Run's body, in source
// order. Parsing the real source, rather than hand-copying the list a
// second time, is what makes this a genuine drift guard: a contributor who
// adds a new call inside Run necessarily changes what this function returns
// the next time the test suite runs, whether or not they remembered to
// update knownSubChecks or the guide.
//
// A bare identifier call (runXxx(...)) is what every existing sub-check
// looks like; a selector call like t.Helper() or context.Background() is
// deliberately excluded (ast.Ident vs ast.SelectorExpr), which is exactly
// what keeps this list to the six real sub-checks instead of every call
// Run's body happens to make.
func runSubChecksFromSource(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	srcPath := filepath.Join(filepath.Dir(thisFile), "conformance.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, srcPath, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", srcPath, err)
	}

	var calls []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "Run" {
			continue
		}
		for _, stmt := range fn.Body.List {
			ast.Inspect(stmt, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if ident, ok := call.Fun.(*ast.Ident); ok {
					calls = append(calls, ident.Name)
				}
				return true
			})
		}
	}
	return calls
}

// TestRun_SubChecksMatchKnownList is the drift guard's first half: it fails
// if Run's actual body calls a bare-identifier function that knownSubChecks
// does not name (a sub-check was added or renamed and this list was not
// updated), or if knownSubChecks names something Run's body no longer calls
// (a sub-check was removed or renamed and this list still claims it
// exists).
func TestRun_SubChecksMatchKnownList(t *testing.T) {
	actual := runSubChecksFromSource(t)
	if len(actual) == 0 {
		t.Fatal("parsed zero calls out of Run's body; the source-parsing step itself is broken, which would make this test vacuously pass")
	}

	got := append([]string(nil), actual...)
	sort.Strings(got)
	want := append([]string(nil), knownSubChecks...)
	sort.Strings(want)

	same := len(got) == len(want)
	if same {
		for i := range got {
			if got[i] != want[i] {
				same = false
				break
			}
		}
	}
	if !same {
		t.Fatalf("Run's body calls %v, but knownSubChecks (this file) declares %v -- "+
			"update knownSubChecks and docs/plugin/authoring-guide.md's qualification checklist together",
			got, want)
	}
}

// TestRun_SubChecksMatchDocumentedChecklist is the drift guard's second
// half: every name in knownSubChecks (and therefore, per the test above,
// every real conformance.Run sub-check) must appear in the qualification
// checklist published at docs/plugin/authoring-guide.md, so a sub-check can
// never silently exist in code without a corresponding checklist item a
// third-party adapter author can read.
func TestRun_SubChecksMatchDocumentedChecklist(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	guidePath := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "docs", "plugin", "authoring-guide.md")
	data, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatalf("reading %s: %v", guidePath, err)
	}
	doc := string(data)

	for _, name := range knownSubChecks {
		if !strings.Contains(doc, name) {
			t.Errorf("qualification checklist at %s does not mention conformance sub-check %q; add a checklist item that maps to it", guidePath, name)
		}
	}
}
