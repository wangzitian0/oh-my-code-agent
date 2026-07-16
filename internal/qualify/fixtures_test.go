package qualify

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// repoFixturesDir locates the repository's top-level fixtures/ directory
// relative to this source file's own location (the same runtime.Caller
// trick internal/ontology's defaultConceptsDir uses), so this test resolves
// correctly regardless of the caller's working directory.
func repoFixturesDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "fixtures")
}

// discoverFixtureCases finds every fixtures/<host>/<version>/<case>
// directory by locating each invocation.yaml.
func discoverFixtureCases(t *testing.T) []string {
	t.Helper()
	root := repoFixturesDir()
	var cases []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "invocation.yaml" {
			cases = append(cases, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("discoverFixtureCases: %v", err)
	}
	sort.Strings(cases)
	return cases
}

// TestFixtureCorpus is the `make fixtures` entry point (Makefile's
// `go test ./... -run Fixture -v`): it drives every committed real fixture
// case in fixtures/ through the full harness pipeline and asserts every
// issue #10 acceptance criterion the harness itself can check
// automatically: zero writes outside the sandbox, zero canary execution,
// and observations matching each case's committed expectation.
func TestFixtureCorpus(t *testing.T) {
	root := repoFixturesDir()
	cases := discoverFixtureCases(t)
	if len(cases) == 0 {
		t.Fatalf("no fixture cases discovered under %s", root)
	}

	pathEnv := os.Getenv("PATH")

	for _, dir := range cases {
		dir := dir
		name, err := filepath.Rel(root, dir)
		if err != nil {
			name = dir
		}
		t.Run(name, func(t *testing.T) {
			c, err := LoadCase(dir)
			if err != nil {
				t.Fatalf("LoadCase(%s): %v", dir, err)
			}

			result, err := Run(context.Background(), t.TempDir(), c, pathEnv)
			if err != nil {
				t.Fatalf("Run(%s): %v", dir, err)
			}

			if len(result.OutsideWorldDiffs) != 0 {
				t.Errorf("%s: OutsideWorldDiffs = %v, want empty (zero-write proof)", name, result.OutsideWorldDiffs)
			}
			if result.CanaryExecuted {
				t.Errorf("%s: CanaryExecuted = true, want false (zero-exec proof)", name)
			}
			if len(result.ObservationMismatches) != 0 {
				t.Errorf("%s: ObservationMismatches = %v, want empty", name, result.ObservationMismatches)
			}
			if result.Digest == "" {
				t.Errorf("%s: Digest is empty", name)
			}

			t.Logf(
				"case=%s host=%s version=%s invocation.skipped=%v invocation.reason=%q digest=%s",
				c.Name, c.Host, c.Version, result.Invocation.Skipped, result.Invocation.SkipReason, result.Digest,
			)
		})
	}
}

// TestFixtureCorpusDigestReproducibility re-runs the entire committed
// fixture corpus a second time (fresh sandbox per case, exactly like a
// second `make fixtures` invocation) and asserts every case's digest is
// identical across both runs — the issue #10 acceptance criterion
// "`make fixtures` twice from committed inputs produces identical output
// digests," exercised directly rather than only by a human running the
// Makefile target twice by hand.
func TestFixtureCorpusDigestReproducibility(t *testing.T) {
	cases := discoverFixtureCases(t)
	pathEnv := os.Getenv("PATH")

	first := make(map[string]string, len(cases))
	for _, dir := range cases {
		c, err := LoadCase(dir)
		if err != nil {
			t.Fatalf("LoadCase(%s): %v", dir, err)
		}
		result, err := Run(context.Background(), t.TempDir(), c, pathEnv)
		if err != nil {
			t.Fatalf("Run (first pass, %s): %v", dir, err)
		}
		first[dir] = result.Digest
	}

	for _, dir := range cases {
		c, err := LoadCase(dir)
		if err != nil {
			t.Fatalf("LoadCase(%s): %v", dir, err)
		}
		result, err := Run(context.Background(), t.TempDir(), c, pathEnv)
		if err != nil {
			t.Fatalf("Run (second pass, %s): %v", dir, err)
		}
		if result.Digest != first[dir] {
			t.Errorf("%s: digest not reproducible: first=%q second=%q", dir, first[dir], result.Digest)
		}
	}
}
