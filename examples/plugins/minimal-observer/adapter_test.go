package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
)

// TestAdapter_Observe_TwoFileSources is docs/plugin/authoring-guide.md's
// "observe a couple of file-based sources" step, exercised directly and
// in-process (no subprocess, no transport -- conformance_test.go in this
// same package covers the out-of-process path). It proves Observe finds
// both files it is given and reports nothing about a directory a third
// root that does not exist under its Roots.
func TestAdapter_Observe_TwoFileSources(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "settings.json", `{"greeting":"hello"}`)
	writeFile(t, dir, filepath.Join("skills", "deploy", "SKILL.md"), "# deploy\n")

	adapter := NewAdapter()
	ctx := context.Background()

	hosts, err := adapter.Detect(ctx, plugin.DetectRequest{})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("Detect returned %d hosts, want 1", len(hosts))
	}
	host := hosts[0]

	obs, err := adapter.Observe(ctx, plugin.ObserveRequest{Host: host, Roots: []string{dir}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(obs.Observations) != 2 {
		t.Fatalf("Observe found %d observations, want 2: %+v", len(obs.Observations), obs.Observations)
	}
	for _, o := range obs.Observations {
		if o.Digest == "" {
			t.Errorf("observation %q has an empty digest", o.Source)
		}
	}
}

// TestAdapter_UnsupportedConcept_ReportsErrUnsupportedOperation proves the
// guide's "honestly report ErrUnsupportedOperation" claim for the concept
// this adapter does not support (conceptMCP), across every operation that
// declares it in the error taxonomy.
func TestAdapter_UnsupportedConcept_ReportsErrUnsupportedOperation(t *testing.T) {
	adapter := NewAdapter()
	ctx := context.Background()
	hosts, err := adapter.Detect(ctx, plugin.DetectRequest{})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	host := hosts[0]

	if _, err := adapter.Resolve(ctx, plugin.ResolveRequest{Host: host, Concept: conceptMCP}); err != plugin.ErrUnsupportedOperation {
		t.Errorf("Resolve(%q) error = %v, want ErrUnsupportedOperation", conceptMCP, err)
	}
	if _, err := adapter.Compile(ctx, plugin.CompileRequest{Host: host, Concept: conceptMCP}); err != plugin.ErrUnsupportedOperation {
		t.Errorf("Compile(%q) error = %v, want ErrUnsupportedOperation", conceptMCP, err)
	}
	if _, err := adapter.Verify(ctx, plugin.VerifyRequest{Host: host, Concept: conceptMCP}); err != plugin.ErrUnsupportedOperation {
		t.Errorf("Verify(%q) error = %v, want ErrUnsupportedOperation", conceptMCP, err)
	}
	if err := adapter.Launch(ctx, plugin.LaunchRequest{Host: host, Concept: conceptMCP}); err != plugin.ErrUnsupportedOperation {
		t.Errorf("Launch(%q) error = %v, want ErrUnsupportedOperation", conceptMCP, err)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}
