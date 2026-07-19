package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// --- parseGoTestJSON: pure, fast, no subprocess --------------------------

func mustEncodeEvents(t *testing.T, events []goTestEvent) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			t.Fatalf("encoding test event: %v", err)
		}
	}
	return &buf
}

func TestParseGoTestJSON_PassAndFailPackages(t *testing.T) {
	events := []goTestEvent{
		{Action: "run", Package: "example.com/pass", Test: "TestOK"},
		{Action: "pass", Package: "example.com/pass", Test: "TestOK"},
		{Action: "pass", Package: "example.com/pass"},

		{Action: "run", Package: "example.com/fail", Test: "TestBad"},
		{Action: "output", Package: "example.com/fail", Test: "TestBad", Output: "    boom.go:12: unexpected value\n"},
		{Action: "fail", Package: "example.com/fail", Test: "TestBad"},
		{Action: "output", Package: "example.com/fail", Output: "--- FAIL: TestBad (0.00s)\n"},
		{Action: "fail", Package: "example.com/fail"},
	}
	results, err := parseGoTestJSON(mustEncodeEvents(t, events))
	if err != nil {
		t.Fatalf("parseGoTestJSON: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2 entries", results)
	}
	byID := map[string]domain.FixtureResult{}
	for _, r := range results {
		byID[r.ID] = r
	}
	pass, ok := byID["example.com/pass"]
	if !ok || pass.Status != domain.FixtureResultPass {
		t.Errorf("example.com/pass = %+v, want PASS", pass)
	}
	if pass.Detail != "" {
		t.Errorf("PASS package Detail = %q, want empty", pass.Detail)
	}
	fail, ok := byID["example.com/fail"]
	if !ok || fail.Status != domain.FixtureResultFail {
		t.Errorf("example.com/fail = %+v, want FAIL", fail)
	}
	if !strings.Contains(fail.Detail, "TestBad") {
		t.Errorf("FAIL package Detail = %q, want it to name the failing test", fail.Detail)
	}
	if !strings.Contains(fail.Detail, "--- FAIL: TestBad") {
		t.Errorf("FAIL package Detail = %q, want captured package output too", fail.Detail)
	}
}

func TestParseGoTestJSON_NoTestFiles_PackageSkip_ReportsPass(t *testing.T) {
	events := []goTestEvent{
		{Action: "skip", Package: "example.com/notests"},
	}
	results, err := parseGoTestJSON(mustEncodeEvents(t, events))
	if err != nil {
		t.Fatalf("parseGoTestJSON: %v", err)
	}
	if len(results) != 1 || results[0].Status != domain.FixtureResultPass {
		t.Fatalf("results = %+v, want one PASS entry for a package with no test files", results)
	}
}

func TestParseGoTestJSON_UnresolvedPackage_DefaultsToFailNotPass(t *testing.T) {
	// A package go test emitted events for but never resolved to a clean
	// top-level pass/skip/fail (e.g. a build failure that only produced
	// "output" events) must never silently read as a passing result.
	events := []goTestEvent{
		{Action: "output", Package: "example.com/buildbroken", Output: "# example.com/buildbroken\nsyntax error\n"},
	}
	results, err := parseGoTestJSON(mustEncodeEvents(t, events))
	if err != nil {
		t.Fatalf("parseGoTestJSON: %v", err)
	}
	if len(results) != 1 || results[0].Status != domain.FixtureResultFail {
		t.Fatalf("results = %+v, want a FAIL entry (never a fabricated PASS) for an unresolved package", results)
	}
}

func TestParseGoTestJSON_OutputTruncatedToBoundedTail(t *testing.T) {
	var events []goTestEvent
	for i := 0; i < maxFixtureOutputLines+10; i++ {
		events = append(events, goTestEvent{Action: "output", Package: "example.com/verbose", Output: "line\n"})
	}
	events = append(events, goTestEvent{Action: "fail", Package: "example.com/verbose"})
	results, err := parseGoTestJSON(mustEncodeEvents(t, events))
	if err != nil {
		t.Fatalf("parseGoTestJSON: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want one entry", results)
	}
	lines := strings.Count(results[0].Detail, "line")
	if lines > maxFixtureOutputLines {
		t.Errorf("Detail contains %d output lines, want at most %d", lines, maxFixtureOutputLines)
	}
}

// --- AffectedPackages: a small throwaway module built entirely by this test

// writeAffectedPackagesFixtureModule builds, inside t.TempDir(), a tiny Go
// module whose module path is deliberately the real
// "github.com/wangzitian0/oh-my-code-agent" so that a stub
// internal/knowledge package inside it resolves the exact import path
// AffectedPackages itself looks for -- entirely self-contained, no
// dependency on this repository's own real internal/knowledge package.
func writeAffectedPackagesFixtureModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("go.mod", "module github.com/wangzitian0/oh-my-code-agent\n\ngo 1.22\n")
	write("internal/knowledge/stub.go", "package knowledge\n")
	write("internal/consumer/consumer.go", `package consumer

import _ "github.com/wangzitian0/oh-my-code-agent/internal/knowledge"

func Noop() {}
`)
	write("internal/fixtureuser/fixtureuser_test.go", `package fixtureuser

import (
	"path/filepath"
	"testing"
)

func TestUsesFixture(t *testing.T) {
	_ = filepath.Join("fixtures", "acme-host", "1.0", "some-case")
}
`)
	write("internal/unrelated/unrelated.go", "package unrelated\n\nfunc Noop() {}\n")
	return root
}

func TestAffectedPackages_FixtureModule_IncludesSelfImportersAndFixtureReferencers(t *testing.T) {
	root := writeAffectedPackagesFixtureModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pkgs, err := AffectedPackages(ctx, root, "acme-host")
	if err != nil {
		t.Fatalf("AffectedPackages: %v", err)
	}
	sort.Strings(pkgs)

	want := map[string]bool{
		"github.com/wangzitian0/oh-my-code-agent/internal/knowledge":   true, // always included
		"github.com/wangzitian0/oh-my-code-agent/internal/consumer":    true, // imports internal/knowledge
		"github.com/wangzitian0/oh-my-code-agent/internal/fixtureuser": true, // references fixtures/acme-host
	}
	got := map[string]bool{}
	for _, p := range pkgs {
		got[p] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("AffectedPackages = %v, want it to include %q", pkgs, k)
		}
	}
	if got["github.com/wangzitian0/oh-my-code-agent/internal/unrelated"] {
		t.Errorf("AffectedPackages = %v, want it to NOT include the unrelated package", pkgs)
	}
}

func TestAffectedPackages_EmptyHostOrModuleDir_Errors(t *testing.T) {
	ctx := context.Background()
	if _, err := AffectedPackages(ctx, "", "codex"); err == nil {
		t.Error("AffectedPackages with empty moduleDir: want an error")
	}
	if _, err := AffectedPackages(ctx, t.TempDir(), ""); err == nil {
		t.Error("AffectedPackages with empty host: want an error")
	}
}

// --- AffectedPackages / RunAffectedFixtures against this real repository -

// repoRootForTest resolves this repository's own module root the same way
// internal/knowledge/repository.go's defaultHostsDir does: relative to this
// source file's own location, so it is correct regardless of the test
// runner's working directory.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("repoRootForTest: runtime.Caller(0) failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func TestAffectedPackages_RealRepository_IncludesKnownConsumersAndFixtureReferencers(t *testing.T) {
	root := repoRootForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), affectedPackagesTimeout)
	defer cancel()

	pkgs, err := AffectedPackages(ctx, root, "codex")
	if err != nil {
		t.Fatalf("AffectedPackages: %v", err)
	}
	got := map[string]bool{}
	for _, p := range pkgs {
		got[p] = true
	}
	const modulePrefix = "github.com/wangzitian0/oh-my-code-agent/"
	// internal/report imports internal/knowledge in production code
	// (internal/report/adapter.go, build.go).
	if !got[modulePrefix+"internal/report"] {
		t.Errorf("AffectedPackages(codex) = %v, want it to include internal/report (imports internal/knowledge)", pkgs)
	}
	// internal/effective's own fixture_test.go references
	// fixtures/codex/0.144.5/...
	if !got[modulePrefix+"internal/effective"] {
		t.Errorf("AffectedPackages(codex) = %v, want it to include internal/effective (references fixtures/codex)", pkgs)
	}
	if !got[knowledgePackageImportPath] {
		t.Errorf("AffectedPackages(codex) = %v, want it to always include internal/knowledge itself", pkgs)
	}
}

func TestRunAffectedFixtures_RealRepository_DomainPackage_Passes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping a real `go test` subprocess invocation in -short mode")
	}
	root := repoRootForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), runAffectedFixturesTimeout)
	defer cancel()

	results, err := RunAffectedFixtures(ctx, root, []string{"./internal/domain/..."})
	if err != nil {
		t.Fatalf("RunAffectedFixtures: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("RunAffectedFixtures: got 0 results, want at least one package result")
	}
	for _, r := range results {
		if r.Status != domain.FixtureResultPass {
			t.Errorf("package %s = %s (%s), want PASS -- internal/domain's own suite is expected to be green", r.ID, r.Status, r.Detail)
		}
	}
}

// --- RunAffectedFixtures: a small throwaway module built entirely by this
// test, with one passing and one failing package, proving PASS/FAIL
// classification against a real `go test -json` subprocess.

func writeFixtureRunnerModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("go.mod", "module omca-fixturerunner-test\n\ngo 1.22\n")
	write("passpkg/passpkg_test.go", `package passpkg

import "testing"

func TestOK(t *testing.T) {}
`)
	write("failpkg/failpkg_test.go", `package failpkg

import "testing"

func TestBad(t *testing.T) {
	t.Fatal("boom")
}
`)
	return root
}

func TestRunAffectedFixtures_ThrowawayModule_ClassifiesPassAndFail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping a real `go test` subprocess invocation in -short mode")
	}
	root := writeFixtureRunnerModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	results, err := RunAffectedFixtures(ctx, root, nil) // nil -> "./..." fallback
	if err != nil {
		t.Fatalf("RunAffectedFixtures: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want exactly 2 package results", results)
	}
	byID := map[string]domain.FixtureResult{}
	for _, r := range results {
		byID[r.ID] = r
	}
	pass, ok := byID["omca-fixturerunner-test/passpkg"]
	if !ok || pass.Status != domain.FixtureResultPass {
		t.Errorf("passpkg = %+v, want PASS", pass)
	}
	fail, ok := byID["omca-fixturerunner-test/failpkg"]
	if !ok || fail.Status != domain.FixtureResultFail {
		t.Errorf("failpkg = %+v, want FAIL", fail)
	}
	if !strings.Contains(fail.Detail, "TestBad") {
		t.Errorf("failpkg.Detail = %q, want it to name TestBad", fail.Detail)
	}
	if !strings.Contains(fail.Detail, "boom") {
		t.Errorf("failpkg.Detail = %q, want it to include the captured failure output", fail.Detail)
	}
}

func TestRunAffectedFixtures_EmptyModuleDir_Errors(t *testing.T) {
	if _, err := RunAffectedFixtures(context.Background(), "", nil); err == nil {
		t.Error("RunAffectedFixtures with empty moduleDir: want an error")
	}
}

// --- WithFixtureResults ---------------------------------------------------

func TestWithFixtureResults_ReplacesSpecFixtureResults(t *testing.T) {
	c := domain.KnowledgeCandidate{
		APIVersion: domain.SupportedAPIVersion, Kind: "KnowledgeCandidate",
		Metadata: domain.KnowledgeCandidateMetadata{ID: "c1", Host: "codex", Surface: "cli", CollectedAt: "t", Automation: "test"},
		Spec: domain.KnowledgeCandidateSpec{
			ChangedSources: []domain.ChangedSource{{SourceID: "s1", NewDigest: "d"}},
			FixtureResults: []domain.FixtureResult{{ID: "placeholder", Status: domain.FixtureResultNotRun}},
		},
	}
	updated := WithFixtureResults(c, []domain.FixtureResult{{ID: "real-pkg", Status: domain.FixtureResultPass}})
	if len(updated.Spec.FixtureResults) != 1 || updated.Spec.FixtureResults[0].ID != "real-pkg" {
		t.Fatalf("WithFixtureResults = %+v, want the NOT_RUN placeholder replaced", updated.Spec.FixtureResults)
	}
	// The original candidate must not be mutated -- WithFixtureResults
	// returns a copy.
	if c.Spec.FixtureResults[0].ID != "placeholder" {
		t.Fatalf("WithFixtureResults mutated its input candidate: %+v", c.Spec.FixtureResults)
	}
}
