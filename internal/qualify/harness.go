package qualify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// RunResult is everything one Harness.Run call produced, for a caller (a Go
// test, or a future PR-08 observation pipeline) to assert against.
type RunResult struct {
	Sandbox *Sandbox

	// Invocation is what RunInvocation did (or why it was skipped).
	Invocation InvocationResult

	// OutsideWorldDiffs must be empty: it is the zero-write proof (diff of
	// Sandbox.Outside before/after the invocation).
	OutsideWorldDiffs []string
	// CanaryExecuted must be false: the zero-exec proof.
	CanaryExecuted bool

	// Observations is what ObserveSandbox actually computed from the
	// populated sandbox.
	Observations []domain.Observation
	// ObservationMismatches is empty when Observations matches the case's
	// committed expected-observations.json exactly.
	ObservationMismatches []string

	// Digest is CaseOutput.Digest() over {Manifest, Observations,
	// Effective} — the reproducibility fingerprint for this run.
	Digest string
}

// Run executes the full PR-06 fixture harness pipeline against one loaded
// Case: build a fresh sandbox under root, populate it from the case's
// input/, plant the outside-world canary, invoke the real host binary (or
// record why it was skipped), snapshot-diff the outside world, compute
// actual observations, diff them against the case's committed expectation,
// and compute the reproducibility digest.
//
// root must be a fresh, case-specific directory the caller controls (a
// t.TempDir() in tests); Run never reuses a directory across cases and never
// touches any path outside root plus whatever pathEnv/the invoked binary's
// own dynamic-linker resolution requires.
func Run(ctx context.Context, root string, c *Case, pathEnv string) (RunResult, error) {
	sb, err := NewSandbox(root, c.Host)
	if err != nil {
		return RunResult{}, err
	}

	if err := sb.PopulateFromInput(c.InputDir()); err != nil {
		return RunResult{}, fmt.Errorf("qualify: Run: %w", err)
	}

	canaryMarker, err := sb.PlantOutsideCanary()
	if err != nil {
		return RunResult{}, fmt.Errorf("qualify: Run: %w", err)
	}

	before, err := snapshotTree(sb.Outside)
	if err != nil {
		return RunResult{}, fmt.Errorf("qualify: Run: snapshot outside-world before: %w", err)
	}

	invResult, err := RunInvocation(ctx, sb, c.Manifest, pathEnv)
	if err != nil {
		return RunResult{}, fmt.Errorf("qualify: Run: %w", err)
	}

	after, err := snapshotTree(sb.Outside)
	if err != nil {
		return RunResult{}, fmt.Errorf("qualify: Run: snapshot outside-world after: %w", err)
	}

	outsideDiffs := diffSnapshots(before, after)

	canaryExecuted := false
	if _, statErr := os.Stat(canaryMarker); statErr == nil {
		canaryExecuted = true
	}

	observations, err := ObserveSandbox(sb, c.Host, c.Manifest.Version, c.Manifest.ObservationRules)
	if err != nil {
		return RunResult{}, fmt.Errorf("qualify: Run: %w", err)
	}

	mismatches := compareObservations(observations, c.ExpectedObservations)

	output := CaseOutput{
		Manifest:     c.Manifest,
		Observations: observations,
		Effective:    c.ExpectedEffective,
	}
	digest, err := output.Digest()
	if err != nil {
		return RunResult{}, fmt.Errorf("qualify: Run: %w", err)
	}

	return RunResult{
		Sandbox:               sb,
		Invocation:            invResult,
		OutsideWorldDiffs:     outsideDiffs,
		CanaryExecuted:        canaryExecuted,
		Observations:          observations,
		ObservationMismatches: mismatches,
		Digest:                digest,
	}, nil
}

// compareObservations reports every mismatch between actual (what
// ObserveSandbox computed) and expected (the case's committed
// expected-observations.json), keyed by Metadata.ID so the message names
// exactly which logical observation differs.
func compareObservations(actual, expected []domain.Observation) []string {
	actualByID := make(map[string]domain.Observation, len(actual))
	for _, o := range actual {
		actualByID[o.Metadata.ID] = o
	}
	expectedByID := make(map[string]domain.Observation, len(expected))
	for _, o := range expected {
		expectedByID[o.Metadata.ID] = o
	}

	var mismatches []string
	for id, exp := range expectedByID {
		act, ok := actualByID[id]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("missing observation: %s", id))
			continue
		}
		if !sameObservation(act, exp) {
			mismatches = append(mismatches, fmt.Sprintf("observation mismatch: %s", id))
		}
	}
	for id := range actualByID {
		if _, ok := expectedByID[id]; !ok {
			mismatches = append(mismatches, fmt.Sprintf("unexpected observation: %s", id))
		}
	}
	sort.Strings(mismatches)
	return mismatches
}

func sameObservation(a, b domain.Observation) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(aj) == string(bj)
}
