package conformance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
)

// Run exercises adapter through the full v1 HostAdapter contract
// (docs/architecture/README.md §9): every method is callable and returns the
// documented type shape, the ErrNotDetected error-taxonomy contract holds
// for every per-host operation, and Observe proves zero writes and zero
// execution against a sandboxed temp directory tree.
//
// Run additionally proves ErrUnsupportedOperation and ErrCapabilityDenied
// whenever adapter's own Capabilities response declares a concept in the
// matching state (a CapabilityUnsupported operation, or a ReconcileBlocked
// concept). FakeAdapter always declares both, so running Run(t,
// NewFakeAdapter()) exercises the complete taxonomy; an adapter whose
// capabilities happen to have neither state simply skips that half of the
// check (logged, not failed), so Run stays usable by any conformant
// adapter — not just ones shaped like the fake.
//
// PR-06's fixture harness and future real adapters (Codex, Claude Code) are
// expected to call this from their own tests: conformance.Run(t, adapter).
func Run(t *testing.T, adapter plugin.HostAdapter) {
	t.Helper()
	ctx := context.Background()

	runID(t, adapter)
	host := runDetect(t, ctx, adapter)
	capManifest := runCapabilities(t, ctx, adapter, host)
	runNotDetectedTaxonomy(t, ctx, adapter)
	runObserveZeroSideEffects(t, ctx, adapter, host)
	runResolveCompileVerifyLaunch(t, ctx, adapter, host, capManifest)
}

func runID(t *testing.T, adapter plugin.HostAdapter) {
	t.Helper()
	if adapter.ID() == "" {
		t.Error("HostAdapter.ID() returned an empty AdapterID")
	}
}

func runDetect(t *testing.T, ctx context.Context, adapter plugin.HostAdapter) plugin.HostInstance {
	t.Helper()
	instances, err := adapter.Detect(ctx, plugin.DetectRequest{})
	if err != nil {
		t.Fatalf("Detect: unexpected error: %v", err)
	}
	if len(instances) == 0 {
		t.Fatal("Detect returned zero HostInstance values; conformance requires the adapter under test to detect at least one host in its test environment")
	}
	return instances[0]
}

func runCapabilities(t *testing.T, ctx context.Context, adapter plugin.HostAdapter, host plugin.HostInstance) plugin.CapabilityManifest {
	t.Helper()
	capManifest, err := adapter.Capabilities(ctx, host)
	if err != nil {
		t.Fatalf("Capabilities(%+v): unexpected error: %v", host, err)
	}
	for concept, entry := range capManifest.Concepts {
		for _, level := range []plugin.Capability{entry.Discover, entry.Parse, entry.Normalize, entry.Resolve, entry.Compile, entry.Verify} {
			if level != "" && !level.Valid() {
				t.Errorf("Capabilities: concept %q declares invalid capability %q", concept, level)
			}
		}
		if entry.ReconcileMode != "" && !entry.ReconcileMode.Valid() {
			t.Errorf("Capabilities: concept %q declares invalid reconcile mode %q", concept, entry.ReconcileMode)
		}
	}
	return capManifest
}

// runNotDetectedTaxonomy proves the universal part of the error taxonomy:
// any conformant adapter must reject a per-host operation invoked against a
// HostInstance its own Detect call did not return.
func runNotDetectedTaxonomy(t *testing.T, ctx context.Context, adapter plugin.HostAdapter) {
	t.Helper()
	notDetected := plugin.HostInstance{HostID: "conformance/not-detected"}

	if _, err := adapter.Capabilities(ctx, notDetected); !errors.Is(err, plugin.ErrNotDetected) {
		t.Errorf("Capabilities(undetected host) error = %v, want ErrNotDetected", err)
	}
	if _, err := adapter.Observe(ctx, plugin.ObserveRequest{Host: notDetected}); !errors.Is(err, plugin.ErrNotDetected) {
		t.Errorf("Observe(undetected host) error = %v, want ErrNotDetected", err)
	}
	if _, err := adapter.Resolve(ctx, plugin.ResolveRequest{Host: notDetected}); !errors.Is(err, plugin.ErrNotDetected) {
		t.Errorf("Resolve(undetected host) error = %v, want ErrNotDetected", err)
	}
	if _, err := adapter.Compile(ctx, plugin.CompileRequest{Host: notDetected}); !errors.Is(err, plugin.ErrNotDetected) {
		t.Errorf("Compile(undetected host) error = %v, want ErrNotDetected", err)
	}
	if _, err := adapter.Verify(ctx, plugin.VerifyRequest{Host: notDetected}); !errors.Is(err, plugin.ErrNotDetected) {
		t.Errorf("Verify(undetected host) error = %v, want ErrNotDetected", err)
	}
	if err := adapter.Launch(ctx, plugin.LaunchRequest{Host: notDetected}); !errors.Is(err, plugin.ErrNotDetected) {
		t.Errorf("Launch(undetected host) error = %v, want ErrNotDetected", err)
	}
}

// runObserveZeroSideEffects is the named acceptance criterion: it builds a
// sandboxed temp directory tree standing in for native host config files
// (docs/knowledge/README.md §10 item 10, "proof that observation did not
// execute content"), snapshots it, calls Observe, and asserts the tree is
// byte-for-byte unchanged. A canary script is included so that if Observe
// executed it (instead of only inventorying it), the marker file it would
// write is itself caught by the same snapshot diff.
func runObserveZeroSideEffects(t *testing.T, ctx context.Context, adapter plugin.HostAdapter, host plugin.HostInstance) {
	t.Helper()
	sandbox := t.TempDir()

	writeSandboxFile(t, sandbox, "settings.json", `{"permissions":{"allow":["*"]}}`)
	writeSandboxFile(t, sandbox, filepath.Join("skills", "deploy", "SKILL.md"), "# deploy\nunknown-field: yes\n")
	canaryPath := writeSandboxFile(t, sandbox, filepath.Join("bin", "canary.sh"), "#!/bin/sh\necho executed > \"$(dirname \"$0\")/CANARY_MARKER\"\n")
	if err := os.Chmod(canaryPath, 0o755); err != nil {
		t.Fatalf("chmod canary script: %v", err)
	}

	before, err := snapshotTree(sandbox)
	if err != nil {
		t.Fatalf("snapshot sandbox before Observe: %v", err)
	}

	if _, err := adapter.Observe(ctx, plugin.ObserveRequest{Host: host, Roots: []string{sandbox}}); err != nil {
		t.Fatalf("Observe(sandboxed roots): unexpected error: %v", err)
	}

	after, err := snapshotTree(sandbox)
	if err != nil {
		t.Fatalf("snapshot sandbox after Observe: %v", err)
	}

	if diffs := diffSnapshots(before, after); len(diffs) != 0 {
		t.Errorf("Observe modified the sandboxed tree (zero-write/zero-exec violation): %v", diffs)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "bin", "CANARY_MARKER")); err == nil {
		t.Error("Observe executed the sandboxed canary script (zero-exec violation): CANARY_MARKER was created")
	}
}

func writeSandboxFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

// runResolveCompileVerifyLaunch drives the happy path through
// Resolve/Compile/Verify/Launch on a concept the adapter declares as usable,
// then proves ErrUnsupportedOperation and ErrCapabilityDenied whenever the
// adapter's own capability manifest declares a concept in the matching
// state.
func runResolveCompileVerifyLaunch(t *testing.T, ctx context.Context, adapter plugin.HostAdapter, host plugin.HostInstance, capManifest plugin.CapabilityManifest) {
	t.Helper()

	okConcept, ok := firstConceptWith(capManifest, func(e plugin.CapabilityEntry) bool {
		return e.Resolve != plugin.CapabilityUnsupported &&
			e.Compile != plugin.CapabilityUnsupported &&
			e.ReconcileMode != plugin.ReconcileBlocked
	})
	if !ok {
		t.Fatal("adapter's CapabilityManifest declares no concept usable for the resolve/compile/verify/launch happy path; conformance requires at least one")
	}

	obs, err := adapter.Observe(ctx, plugin.ObserveRequest{Host: host})
	if err != nil {
		t.Fatalf("Observe (as Resolve input): unexpected error: %v", err)
	}

	effective, err := adapter.Resolve(ctx, plugin.ResolveRequest{Host: host, Concept: okConcept, Observations: obs})
	if err != nil {
		t.Fatalf("Resolve(%q): unexpected error: %v", okConcept, err)
	}
	if effective.Host != host {
		t.Errorf("Resolve(%q).Host = %+v, want %+v", okConcept, effective.Host, host)
	}

	artifacts, err := adapter.Compile(ctx, plugin.CompileRequest{Host: host, Concept: okConcept, Desired: effective})
	if err != nil {
		t.Fatalf("Compile(%q): unexpected error: %v", okConcept, err)
	}

	if _, err := adapter.Verify(ctx, plugin.VerifyRequest{Host: host, Concept: okConcept, Artifacts: artifacts}); err != nil {
		t.Fatalf("Verify(%q): unexpected error: %v", okConcept, err)
	}

	if err := adapter.Launch(ctx, plugin.LaunchRequest{Host: host, Concept: okConcept, Artifacts: artifacts}); err != nil {
		t.Errorf("Launch(%q): unexpected error: %v", okConcept, err)
	}

	if unsupported, ok := firstConceptWith(capManifest, func(e plugin.CapabilityEntry) bool {
		return e.Resolve == plugin.CapabilityUnsupported
	}); ok {
		if _, err := adapter.Resolve(ctx, plugin.ResolveRequest{Host: host, Concept: unsupported, Observations: obs}); !errors.Is(err, plugin.ErrUnsupportedOperation) {
			t.Errorf("Resolve(%q) [capability UNSUPPORTED] error = %v, want ErrUnsupportedOperation", unsupported, err)
		}
	} else {
		t.Log("adapter declares no CapabilityUnsupported concept; skipping the ErrUnsupportedOperation proof (FakeAdapter always declares one)")
	}

	if blocked, ok := firstConceptWith(capManifest, func(e plugin.CapabilityEntry) bool {
		return e.ReconcileMode == plugin.ReconcileBlocked
	}); ok {
		if _, err := adapter.Compile(ctx, plugin.CompileRequest{Host: host, Concept: blocked, Desired: effective}); !errors.Is(err, plugin.ErrCapabilityDenied) {
			t.Errorf("Compile(%q) [reconcile BLOCKED] error = %v, want ErrCapabilityDenied", blocked, err)
		}
		if err := adapter.Launch(ctx, plugin.LaunchRequest{Host: host, Concept: blocked}); !errors.Is(err, plugin.ErrCapabilityDenied) {
			t.Errorf("Launch(%q) [reconcile BLOCKED] error = %v, want ErrCapabilityDenied", blocked, err)
		}
	} else {
		t.Log("adapter declares no ReconcileBlocked concept; skipping the ErrCapabilityDenied proof (FakeAdapter always declares one)")
	}
}

// firstConceptWith returns the first concept (in stable, sorted order, since
// Go map iteration order is randomized) whose CapabilityEntry satisfies
// pred.
func firstConceptWith(m plugin.CapabilityManifest, pred func(plugin.CapabilityEntry) bool) (string, bool) {
	keys := make([]string, 0, len(m.Concepts))
	for k := range m.Concepts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if pred(m.Concepts[k]) {
			return k, true
		}
	}
	return "", false
}
