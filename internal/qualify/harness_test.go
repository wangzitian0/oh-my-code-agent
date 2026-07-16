package qualify

import (
	"context"
	"os"
	"testing"
)

// TestRunEndToEnd drives the full PR-06 harness pipeline against the
// package's own testdata case and checks every proof the issue #10
// acceptance criteria name: zero writes outside the sandbox, zero
// execution of a planted canary, observations matching the committed
// expectation, and a content digest.
func TestRunEndToEnd(t *testing.T) {
	c, err := LoadCase(testdataCaseDir)
	if err != nil {
		t.Fatalf("LoadCase: %v", err)
	}

	result, err := Run(context.Background(), t.TempDir(), c, os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.Invocation.Skipped {
		t.Errorf("Invocation = %+v, want Skipped=true (this case's manifest declares invoke.attempted=false)", result.Invocation)
	}
	if len(result.OutsideWorldDiffs) != 0 {
		t.Errorf("OutsideWorldDiffs = %v, want empty (zero-write proof)", result.OutsideWorldDiffs)
	}
	if result.CanaryExecuted {
		t.Error("CanaryExecuted = true, want false (zero-exec proof)")
	}
	if len(result.ObservationMismatches) != 0 {
		t.Errorf("ObservationMismatches = %v, want empty", result.ObservationMismatches)
	}
	if len(result.Observations) != 2 {
		t.Errorf("len(Observations) = %d, want 2", len(result.Observations))
	}
	if result.Digest == "" {
		t.Error("Digest is empty")
	}
}

// TestRunIsReproducible runs the harness twice from the same committed
// testdata case (fresh sandbox each time, exactly like two independent
// `make fixtures` invocations) and asserts the digests are identical — the
// issue #10 acceptance criterion "make fixtures twice from committed inputs
// produces identical output digests."
func TestRunIsReproducible(t *testing.T) {
	c, err := LoadCase(testdataCaseDir)
	if err != nil {
		t.Fatalf("LoadCase: %v", err)
	}

	first, err := Run(context.Background(), t.TempDir(), c, os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("Run (first): %v", err)
	}
	second, err := Run(context.Background(), t.TempDir(), c, os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("Run (second): %v", err)
	}

	if first.Digest != second.Digest {
		t.Errorf("Run() digest not reproducible: first=%q second=%q", first.Digest, second.Digest)
	}
}

// TestRunDetectsAWriteOutsideTheSandbox proves the zero-write proof would
// actually catch a violation, not just vacuously pass because nothing ever
// differs (mirrors internal/plugin/conformance's own diff-detection self-
// test). Run's own before/after snapshots happen inside one function call,
// so this test exercises the same snapshot/diff primitives Run uses,
// directly, against a stand-in tree it controls.
func TestRunDetectsAWriteOutsideTheSandbox(t *testing.T) {
	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sb.PlantOutsideCanary(); err != nil {
		t.Fatal(err)
	}

	before, err := snapshotTree(sb.Outside)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate an isolation failure: something wrote to Outside.
	if err := writeFile(sb.Outside+"/leaked.txt", "isolation failed", 0o644); err != nil {
		t.Fatal(err)
	}

	after, err := snapshotTree(sb.Outside)
	if err != nil {
		t.Fatal(err)
	}

	diffs := diffSnapshots(before, after)
	if len(diffs) != 1 || diffs[0] != "added: leaked.txt" {
		t.Errorf("diffSnapshots = %v, want [\"added: leaked.txt\"]", diffs)
	}
}
