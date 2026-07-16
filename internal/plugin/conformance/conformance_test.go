package conformance

import (
	"context"
	"errors"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
)

// TestFakeAdapterConformance proves the fake adapter passes the full
// conformance suite: every HostAdapter method, the ErrNotDetected taxonomy,
// the ErrUnsupportedOperation and ErrCapabilityDenied taxonomy (FakeAdapter
// declares concepts in both states so this half of Run actually executes
// rather than being skipped), and the Observe zero-write/zero-exec proof.
func TestFakeAdapterConformance(t *testing.T) {
	Run(t, NewFakeAdapter())
}

// TestFakeAdapterDetect pins the fake's Detect contract directly (not just
// through Run) so a regression in its fixed HostInstance is caught here
// first.
func TestFakeAdapterDetect(t *testing.T) {
	adapter := NewFakeAdapter()
	instances, err := adapter.Detect(context.Background(), plugin.DetectRequest{})
	if err != nil {
		t.Fatalf("Detect: unexpected error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("Detect returned %d instances, want 1", len(instances))
	}
	if instances[0].HostID != "codex" {
		t.Errorf("Detect()[0].HostID = %q, want %q", instances[0].HostID, "codex")
	}
}

// TestFakeAdapterCapabilitiesUnknownHost pins the ErrNotDetected path
// directly against Capabilities, independent of Run.
func TestFakeAdapterCapabilitiesUnknownHost(t *testing.T) {
	adapter := NewFakeAdapter()
	_, err := adapter.Capabilities(context.Background(), plugin.HostInstance{HostID: "unknown"})
	if !errors.Is(err, plugin.ErrNotDetected) {
		t.Errorf("Capabilities(unknown host) error = %v, want ErrNotDetected", err)
	}
}

// TestFakeAdapterErrorTaxonomy pins the fake's ErrUnsupportedOperation and
// ErrCapabilityDenied paths directly, independent of Run, as the concrete
// reference real adapters can match their own error handling against.
func TestFakeAdapterErrorTaxonomy(t *testing.T) {
	adapter := NewFakeAdapter()
	ctx := context.Background()
	instances, err := adapter.Detect(ctx, plugin.DetectRequest{})
	if err != nil {
		t.Fatalf("Detect: unexpected error: %v", err)
	}
	host := instances[0]

	t.Run("ErrUnsupportedOperation from Resolve", func(t *testing.T) {
		_, err := adapter.Resolve(ctx, plugin.ResolveRequest{Host: host, Concept: ConceptUnsupported})
		if !errors.Is(err, plugin.ErrUnsupportedOperation) {
			t.Errorf("Resolve(%q) error = %v, want ErrUnsupportedOperation", ConceptUnsupported, err)
		}
	})

	t.Run("ErrCapabilityDenied from Compile", func(t *testing.T) {
		effective, err := adapter.Resolve(ctx, plugin.ResolveRequest{Host: host, Concept: ConceptBlocked})
		if err != nil {
			t.Fatalf("Resolve(%q): unexpected error: %v", ConceptBlocked, err)
		}
		_, err = adapter.Compile(ctx, plugin.CompileRequest{Host: host, Concept: ConceptBlocked, Desired: effective})
		if !errors.Is(err, plugin.ErrCapabilityDenied) {
			t.Errorf("Compile(%q) error = %v, want ErrCapabilityDenied", ConceptBlocked, err)
		}
	})

	t.Run("ErrCapabilityDenied from Launch", func(t *testing.T) {
		err := adapter.Launch(ctx, plugin.LaunchRequest{Host: host, Concept: ConceptBlocked})
		if !errors.Is(err, plugin.ErrCapabilityDenied) {
			t.Errorf("Launch(%q) error = %v, want ErrCapabilityDenied", ConceptBlocked, err)
		}
	})

	t.Run("ErrNotDetected from every per-host method", func(t *testing.T) {
		undetected := plugin.HostInstance{HostID: "not-detected"}
		if _, err := adapter.Capabilities(ctx, undetected); !errors.Is(err, plugin.ErrNotDetected) {
			t.Errorf("Capabilities: error = %v, want ErrNotDetected", err)
		}
		if _, err := adapter.Observe(ctx, plugin.ObserveRequest{Host: undetected}); !errors.Is(err, plugin.ErrNotDetected) {
			t.Errorf("Observe: error = %v, want ErrNotDetected", err)
		}
		if _, err := adapter.Resolve(ctx, plugin.ResolveRequest{Host: undetected}); !errors.Is(err, plugin.ErrNotDetected) {
			t.Errorf("Resolve: error = %v, want ErrNotDetected", err)
		}
		if _, err := adapter.Compile(ctx, plugin.CompileRequest{Host: undetected}); !errors.Is(err, plugin.ErrNotDetected) {
			t.Errorf("Compile: error = %v, want ErrNotDetected", err)
		}
		if _, err := adapter.Verify(ctx, plugin.VerifyRequest{Host: undetected}); !errors.Is(err, plugin.ErrNotDetected) {
			t.Errorf("Verify: error = %v, want ErrNotDetected", err)
		}
		if err := adapter.Launch(ctx, plugin.LaunchRequest{Host: undetected}); !errors.Is(err, plugin.ErrNotDetected) {
			t.Errorf("Launch: error = %v, want ErrNotDetected", err)
		}
	})
}

// TestFakeAdapterObserveReadsContent proves Observe is a genuine inventory
// (not a no-op that would trivially pass the zero-write proof): it must
// actually read and digest the files under the given roots.
func TestFakeAdapterObserveReadsContent(t *testing.T) {
	adapter := NewFakeAdapter()
	ctx := context.Background()
	instances, err := adapter.Detect(ctx, plugin.DetectRequest{})
	if err != nil {
		t.Fatalf("Detect: unexpected error: %v", err)
	}
	host := instances[0]

	dir := t.TempDir()
	writeSandboxFile(t, dir, "settings.json", `{"a":1}`)
	writeSandboxFile(t, dir, "sub/instructions.md", "hello")

	obs, err := adapter.Observe(ctx, plugin.ObserveRequest{Host: host, Roots: []string{dir}})
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(obs.Observations) != 2 {
		t.Fatalf("Observe found %d observations, want 2", len(obs.Observations))
	}
	for _, o := range obs.Observations {
		if o.Digest == "" {
			t.Errorf("Observation %+v has an empty digest", o)
		}
	}
}
