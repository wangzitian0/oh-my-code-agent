package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// This file implements issue #33/PR-29's "affected qualification fixtures
// run automatically" acceptance criterion.
//
// This project has no separate qualification-suite runner binary distinct
// from its own `go test` (docs/knowledge/README.md §10's 12-item checklist
// describes WHAT a qualification suite must cover; the actual mechanism is
// this repository's own Go test suite exercised against the committed
// fixtures under fixtures/<host>/<version>/... and the packages that
// reference them). "Affected" is therefore defined at Go-package
// granularity, not at individual fixture-directory granularity:
//
//   - every package that imports internal/knowledge itself (a change to
//     Knowledge Pack/Candidate handling can affect any consumer of it --
//     internal/report, internal/runtime, internal/tui, cmd/omca all import
//     it today); and
//   - every package whose own source references this specific host's
//     committed fixtures (fixtures/<host>/...) -- internal/effective,
//     internal/qualify, internal/observe, internal/perf today.
//
// internal/knowledge itself is always included, since the Poller/Candidate
// logic a change would most directly affect lives here.
//
// AffectedPackages determines this set via `go list -json ./...`, the same
// Go-toolchain-only introspection internal/plugin/importboundary_test.go and
// internal/observe/zerowrite_test.go already use elsewhere in this
// repository to avoid a new third-party dependency (there is no vendored
// golang.org/x/tools/go/packages in this module's go.sum). This is a
// best-effort, static approximation of "affected" -- not a general-purpose
// build-graph analyzer -- and is documented as such rather than presented as
// more rigorous than it is. If `go list` itself fails (e.g. this build is
// not inside a Go module, or the toolchain is unavailable), the caller
// (cmd/omca/knowledge.go's `omca knowledge propose`) falls back to running
// this repository's own full `go test ./...` and reporting that result
// honestly, rather than inventing a narrower, unproven heuristic --
// docs/knowledge/README.md's own instruction for exactly this situation.

// knowledgePackageImportPath is this package's own fully qualified import
// path -- always a member of AffectedPackages' returned set (see doc
// comment above).
const knowledgePackageImportPath = "github.com/wangzitian0/oh-my-code-agent/internal/knowledge"

// affectedPackagesTimeout bounds how long `go list -json ./...` may run --
// mirrors internal/auth.invokeTimeout's "a hang can never be mistaken for a
// passing test" discipline, applied here to a Go-toolchain subprocess
// instead of a host binary.
const affectedPackagesTimeout = 60 * time.Second

// goListPackage is the subset of `go list -json`'s per-package object this
// package actually reads. See `go help list`'s -json output for the full
// shape; every field below is a real, documented go list JSON field.
type goListPackage struct {
	ImportPath   string
	Dir          string
	GoFiles      []string
	TestGoFiles  []string
	XTestGoFiles []string
	Imports      []string
	Deps         []string
	TestImports  []string
	XTestImports []string
}

// AffectedPackages returns the Go package import paths (see doc comment
// above) whose tests this project considers "affected" by a change to
// host's Knowledge. moduleDir is the root of the Go module to introspect
// (this repository's own root in every real caller; a small throwaway
// module built entirely by the test in every automated test in this
// package -- see fixtures_test.go).
func AffectedPackages(ctx context.Context, moduleDir, host string) ([]string, error) {
	if strings.TrimSpace(moduleDir) == "" {
		return nil, fmt.Errorf("knowledge: AffectedPackages: moduleDir is empty")
	}
	if strings.TrimSpace(host) == "" {
		return nil, fmt.Errorf("knowledge: AffectedPackages: host is empty")
	}

	stdout, err := runGoList(ctx, moduleDir)
	if err != nil {
		return nil, err
	}

	affected := map[string]bool{knowledgePackageImportPath: true}
	dec := json.NewDecoder(stdout)
	for {
		var p goListPackage
		if err := dec.Decode(&p); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("knowledge: AffectedPackages: decoding `go list -json` output: %w", err)
		}
		if p.ImportPath == "" {
			continue
		}
		if importsKnowledgePackage(p) {
			affected[p.ImportPath] = true
			continue
		}
		refs, err := referencesHostFixtures(p, host)
		if err != nil {
			return nil, err
		}
		if refs {
			affected[p.ImportPath] = true
		}
	}

	out := make([]string, 0, len(affected))
	for pkg := range affected {
		out = append(out, pkg)
	}
	sort.Strings(out)
	return out, nil
}

func runGoList(ctx context.Context, moduleDir string) (*bytes.Buffer, error) {
	runCtx, cancel := context.WithTimeout(ctx, affectedPackagesTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "go", "list", "-json", "./...")
	cmd.Dir = moduleDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("knowledge: AffectedPackages: `go list -json ./...` in %s: %w (stderr: %s)", moduleDir, err, stderr.String())
	}
	return &stdout, nil
}

// importsKnowledgePackage reports whether p imports internal/knowledge,
// directly or transitively in production code (Deps, which `go list`
// documents as the transitive import list), or directly in its own test
// files (TestImports/XTestImports, which `go list` reports non-transitively
// -- sufficient here since this package's own test files are exactly the
// direct-import case this is meant to catch, e.g.
// internal/mcp/propose_knowledge_gate_test.go).
func importsKnowledgePackage(p goListPackage) bool {
	for _, sets := range [][]string{p.Imports, p.Deps, p.TestImports, p.XTestImports} {
		for _, imp := range sets {
			if imp == knowledgePackageImportPath {
				return true
			}
		}
	}
	return false
}

// referencesHostFixtures reports whether any of p's own source files
// (production or test) contain a literal reference to
// fixtures/<host>/... -- the exact `filepath.Join("fixtures", "<host>", ...)`
// and `"fixtures/<host>/..."` shapes every real committed fixture reference
// in this repository already uses (internal/effective/fixture_test.go,
// internal/qualify/fixtures_test.go, internal/observe/*_test.go,
// internal/perf/perf_test.go as of PR-29). A literal-substring check rather
// than a general path-expression evaluator: precise enough for this
// repository's actual, consistent style, and a false negative here degrades
// to "this package's tests are not run automatically" rather than anything
// unsafe -- the honest full-suite fallback (see doc comment above) is the
// backstop for exactly that case.
func referencesHostFixtures(p goListPackage, host string) (bool, error) {
	needles := []string{
		fmt.Sprintf("%q, %q", "fixtures", host), // filepath.Join("fixtures", "codex", ...)
		"\"fixtures/" + host + "/",
		"\"fixtures/" + host + "\"",
	}
	files := make([]string, 0, len(p.GoFiles)+len(p.TestGoFiles)+len(p.XTestGoFiles))
	files = append(files, p.GoFiles...)
	files = append(files, p.TestGoFiles...)
	files = append(files, p.XTestGoFiles...)
	for _, f := range files {
		full := filepath.Join(p.Dir, f)
		raw, err := os.ReadFile(full)
		if err != nil {
			return false, fmt.Errorf("knowledge: AffectedPackages: reading %s: %w", full, err)
		}
		text := string(raw)
		for _, needle := range needles {
			if strings.Contains(text, needle) {
				return true, nil
			}
		}
	}
	return false, nil
}

// runAffectedFixturesTimeout bounds one `go test -json` invocation.
// Qualification fixtures are ordinary Go tests, but a real hang (a fixture
// that deadlocks, an accidental infinite loop) must not be mistaken for "the
// candidate PR is just slow to open" -- mirrors pollHostTimeout's identical
// concern for one host's HTTP poll.
const runAffectedFixturesTimeout = 5 * time.Minute

// RunAffectedFixtures runs `go test -json` for packages (every package
// AffectedPackages returned, or, when packages is empty, "./..." as the
// honest full-suite fallback this file's own doc comment describes) inside
// moduleDir, and returns one domain.FixtureResult per Go package actually
// exercised -- this is what populates
// domain.KnowledgeCandidateSpec.FixtureResults, superseding whatever
// NOT_RUN placeholders PollHost recorded (PollHost has no way to know the
// future outcome of a suite this function is responsible for actually
// running).
func RunAffectedFixtures(ctx context.Context, moduleDir string, packages []string) ([]domain.FixtureResult, error) {
	if strings.TrimSpace(moduleDir) == "" {
		return nil, fmt.Errorf("knowledge: RunAffectedFixtures: moduleDir is empty")
	}
	targets := packages
	if len(targets) == 0 {
		targets = []string{"./..."}
	}

	runCtx, cancel := context.WithTimeout(ctx, runAffectedFixturesTimeout)
	defer cancel()
	args := append([]string{"test", "-json"}, targets...)
	cmd := exec.CommandContext(runCtx, "go", args...)
	cmd.Dir = moduleDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr != nil {
		var exitErr *exec.ExitError
		// A non-zero exit purely because at least one test/package failed is
		// the expected, honestly-reportable outcome this function exists to
		// surface (a FAIL FixtureResult, not a Go error) -- the -json stream
		// on stdout still records exactly which package(s) failed, decoded
		// below. Any OTHER kind of error (the `go` binary itself missing,
		// the context timeout firing, moduleDir not a module, ...) means no
		// trustworthy per-package result exists at all and must fail closed
		// as a real error instead of a fabricated result.
		if !errors.As(runErr, &exitErr) {
			return nil, fmt.Errorf("knowledge: RunAffectedFixtures: `go test -json %s` in %s: %w (stderr: %s)", strings.Join(targets, " "), moduleDir, runErr, stderr.String())
		}
	}

	results, err := parseGoTestJSON(&stdout)
	if err != nil {
		return nil, fmt.Errorf("knowledge: RunAffectedFixtures: parsing `go test -json` output: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("knowledge: RunAffectedFixtures: `go test -json %s` in %s produced no package results (stderr: %s)", strings.Join(targets, " "), moduleDir, stderr.String())
	}
	return results, nil
}

// goTestEvent mirrors one line of `go test -json`'s TestEvent stream (see
// `go help test`'s "-json" flag documentation). Only the fields this
// package's own aggregation needs are declared.
type goTestEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

// parseGoTestJSON decodes a `go test -json` event stream and aggregates it
// into one domain.FixtureResult per package: PASS/FAIL from that package's
// own top-level (Test=="") pass/fail/skip event, with Detail listing any
// individual failing test names and a bounded tail of that package's own
// captured output for a FAIL, so a maintainer reading the candidate PR sees
// why a fixture failed without needing to re-run it themselves.
func parseGoTestJSON(r io.Reader) ([]domain.FixtureResult, error) {
	type pkgAgg struct {
		status      string
		failedTests []string
		testOutput  map[string][]string // per-test captured output, e.g. a t.Fatal message
		output      []string            // package-level (Test=="") captured output
	}
	aggs := make(map[string]*pkgAgg)
	order := make([]string, 0, 8)

	dec := json.NewDecoder(r)
	for {
		var ev goTestEvent
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding go test -json event: %w", err)
		}
		if ev.Package == "" {
			continue
		}
		a, ok := aggs[ev.Package]
		if !ok {
			a = &pkgAgg{testOutput: make(map[string][]string)}
			aggs[ev.Package] = a
			order = append(order, ev.Package)
		}
		switch {
		case ev.Test == "" && ev.Action == "pass":
			a.status = domain.FixtureResultPass
		case ev.Test == "" && ev.Action == "skip":
			if a.status == "" {
				a.status = domain.FixtureResultPass
			}
		case ev.Test == "" && ev.Action == "fail":
			a.status = domain.FixtureResultFail
		case ev.Test != "" && ev.Action == "fail":
			a.failedTests = append(a.failedTests, ev.Test)
		case ev.Test == "" && ev.Action == "output":
			trimmed := strings.TrimRight(ev.Output, "\n")
			if trimmed != "" {
				a.output = append(a.output, trimmed)
			}
		case ev.Test != "" && ev.Action == "output":
			// A failing test's own explanatory output (e.g. a t.Fatal/
			// t.Errorf message) is emitted keyed to that specific Test, not
			// to the package-level (Test=="") output stream -- captured
			// here regardless of pass/fail so it is available if that test
			// name later turns up in failedTests above (event order is not
			// guaranteed to put "fail" before every "output" event for the
			// same test).
			trimmed := strings.TrimRight(ev.Output, "\n")
			if trimmed != "" {
				a.testOutput[ev.Test] = append(a.testOutput[ev.Test], trimmed)
			}
		}
	}

	sort.Strings(order)
	out := make([]domain.FixtureResult, 0, len(order))
	for _, pkg := range order {
		a := aggs[pkg]
		status := a.status
		if status == "" {
			// A package go test reported events for but never resolved to a
			// clean pass/skip/fail top-level event (should not happen for a
			// well-formed `go test -json` stream) is reported honestly as
			// FAIL rather than silently defaulting to PASS -- an unproven
			// outcome must never read as a passing qualification result.
			status = domain.FixtureResultFail
		}
		detail := fixtureDetail(a.failedTests, a.testOutput, a.output, status)
		out = append(out, domain.FixtureResult{ID: pkg, Status: status, Detail: detail})
	}
	return out, nil
}

// maxFixtureOutputLines bounds how much raw `go test` output one FAIL
// FixtureResult's Detail carries -- a candidate PR's report is meant to be
// read by a maintainer, not to reproduce an entire (possibly huge) test log.
const maxFixtureOutputLines = 20

func fixtureDetail(failedTests []string, testOutput map[string][]string, pkgOutput []string, status string) string {
	if status == domain.FixtureResultPass {
		return ""
	}
	var b strings.Builder
	if len(failedTests) > 0 {
		fmt.Fprintf(&b, "failed: %s", strings.Join(failedTests, ", "))
	}

	combined := make([]string, 0, len(pkgOutput))
	for _, name := range failedTests {
		combined = append(combined, testOutput[name]...)
	}
	combined = append(combined, pkgOutput...)
	if len(combined) > 0 {
		tail := combined
		if len(tail) > maxFixtureOutputLines {
			tail = tail[len(tail)-maxFixtureOutputLines:]
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.Join(tail, "\n"))
	}
	if b.Len() == 0 {
		return "go test reported a failure with no captured output or failing test name"
	}
	return b.String()
}

// WithFixtureResults returns a copy of c with Spec.FixtureResults replaced
// by results -- the real, just-executed qualification suite outcome
// superseding whatever NOT_RUN placeholders PollHost recorded.
func WithFixtureResults(c domain.KnowledgeCandidate, results []domain.FixtureResult) domain.KnowledgeCandidate {
	out := c
	out.Spec.FixtureResults = append([]domain.FixtureResult(nil), results...)
	return out
}
