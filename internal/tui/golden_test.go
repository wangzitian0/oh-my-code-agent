package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// compareGolden compares got against the committed file at
// testdata/golden/<name> — the shared helper every one of this package's
// four view snapshot tests (overview_test.go, drift_test.go,
// assets_test.go, generations_test.go) uses. With -update it (re)writes
// the file instead of comparing, the same convention fixture_test.go's
// TestRegenerateFixtureArtifact uses for the shared fixture artifact: a
// deliberate rendering change requires deliberately re-running with
// -update and reviewing the resulting diff to testdata/golden/<name>, not
// a silently-passing test.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v (run `go test ./internal/tui/... -update` to create it)", path, err)
	}
	if got != string(want) {
		t.Errorf("%s does not match committed golden file %s.\nRun `go test ./internal/tui/... -update` and review the diff if this change is intentional.\n\n--- got ---\n%s\n--- want ---\n%s", name, path, got, string(want))
	}
}
