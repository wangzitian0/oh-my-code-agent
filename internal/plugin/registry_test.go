package plugin

import (
	"context"
	"errors"
	"testing"
)

// stubAdapter is a minimal, do-nothing HostAdapter used only to exercise the
// registry in this file. It is not the conformance reference adapter (see
// internal/plugin/conformance for that); it exists purely so registry tests
// do not need a fully behaved implementation.
type stubAdapter struct {
	id AdapterID
}

func (s stubAdapter) ID() AdapterID { return s.id }

func (s stubAdapter) Detect(context.Context, DetectRequest) ([]HostInstance, error) {
	return nil, nil
}

func (s stubAdapter) Capabilities(context.Context, HostInstance) (CapabilityManifest, error) {
	return CapabilityManifest{}, nil
}

func (s stubAdapter) Observe(context.Context, ObserveRequest) (ObservationSet, error) {
	return ObservationSet{}, nil
}

func (s stubAdapter) Resolve(context.Context, ResolveRequest) (HostEffectiveState, error) {
	return HostEffectiveState{}, nil
}

func (s stubAdapter) Compile(context.Context, CompileRequest) (ArtifactSet, error) {
	return ArtifactSet{}, nil
}

func (s stubAdapter) Verify(context.Context, VerifyRequest) (EvidenceSet, error) {
	return EvidenceSet{}, nil
}

func (s stubAdapter) Launch(context.Context, LaunchRequest) error {
	return nil
}

func validManifest(adapterID AdapterID, hostID, contractVersion string) PluginManifest {
	return PluginManifest{
		AdapterID:       adapterID,
		AdapterVersion:  "0.1.0",
		ContractVersion: contractVersion,
		Hosts: []HostSelector{
			{HostID: hostID, Surfaces: []string{"cli"}, VersionRange: ">=1.0.0"},
		},
	}
}

// TestRegisterRejectsContractMajorVersionMismatch is the literal first
// acceptance criterion for PR-05: the registry must reject a manifest whose
// ContractVersion major component does not match what the registry expects.
func TestRegisterRejectsContractMajorVersionMismatch(t *testing.T) {
	r := NewRegistry(ContractVersion) // "v1"
	manifest := validManifest("mismatched-adapter", "codex", "v2")

	err := r.Register(manifest, stubAdapter{id: manifest.AdapterID})
	if err == nil {
		t.Fatal("Register with contract version v2 against a v1 registry: got nil error, want ErrContractVersionMismatch")
	}
	if !errors.Is(err, ErrContractVersionMismatch) {
		t.Fatalf("Register error = %v, want it to wrap ErrContractVersionMismatch", err)
	}

	if _, ok := r.Lookup("codex"); ok {
		t.Fatal("Lookup(codex) succeeded after a rejected Register call")
	}
}

func TestRegisterAcceptsMatchingMajorVersionWithDifferentMinor(t *testing.T) {
	r := NewRegistry("v1.0")
	manifest := validManifest("minor-ok-adapter", "codex", "v1.4")

	if err := r.Register(manifest, stubAdapter{id: manifest.AdapterID}); err != nil {
		t.Fatalf("Register with compatible minor version: unexpected error: %v", err)
	}
	adapter, ok := r.Lookup("codex")
	if !ok {
		t.Fatal("Lookup(codex) = false after a successful Register")
	}
	if adapter.ID() != manifest.AdapterID {
		t.Errorf("Lookup(codex).ID() = %q, want %q", adapter.ID(), manifest.AdapterID)
	}
}

func TestRegisterRejectsInvalidManifest(t *testing.T) {
	r := NewRegistry(ContractVersion)
	manifest := PluginManifest{} // missing everything

	if err := r.Register(manifest, stubAdapter{id: "empty"}); err == nil {
		t.Fatal("Register with an empty manifest: got nil error, want a validation error")
	}
}

func TestRegisterRejectsNilAdapter(t *testing.T) {
	r := NewRegistry(ContractVersion)
	manifest := validManifest("nil-adapter", "codex", ContractVersion)

	if err := r.Register(manifest, nil); err == nil {
		t.Fatal("Register with a nil adapter: got nil error, want an error")
	}
}

func TestRegisterRejectsDuplicateHost(t *testing.T) {
	r := NewRegistry(ContractVersion)
	first := validManifest("first-adapter", "codex", ContractVersion)
	second := validManifest("second-adapter", "codex", ContractVersion)

	if err := r.Register(first, stubAdapter{id: first.AdapterID}); err != nil {
		t.Fatalf("first Register: unexpected error: %v", err)
	}
	if err := r.Register(second, stubAdapter{id: second.AdapterID}); err == nil {
		t.Fatal("second Register for the same host: got nil error, want an error")
	}

	adapter, ok := r.Lookup("codex")
	if !ok {
		t.Fatal("Lookup(codex) = false, want the first adapter still registered")
	}
	if adapter.ID() != first.AdapterID {
		t.Errorf("Lookup(codex).ID() = %q, want first adapter %q to remain registered", adapter.ID(), first.AdapterID)
	}
}

func TestLookupUnknownHost(t *testing.T) {
	r := NewRegistry(ContractVersion)
	if _, ok := r.Lookup("does-not-exist"); ok {
		t.Fatal("Lookup(does-not-exist) = true, want false")
	}
}

func TestRegistryManifest(t *testing.T) {
	r := NewRegistry(ContractVersion)
	manifest := validManifest("codex-adapter", "codex", ContractVersion)
	if err := r.Register(manifest, stubAdapter{id: manifest.AdapterID}); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}

	got, ok := r.Manifest("codex")
	if !ok {
		t.Fatal("Manifest(codex) = false, want true")
	}
	if got.AdapterID != manifest.AdapterID {
		t.Errorf("Manifest(codex).AdapterID = %q, want %q", got.AdapterID, manifest.AdapterID)
	}

	if _, ok := r.Manifest("does-not-exist"); ok {
		t.Fatal("Manifest(does-not-exist) = true, want false")
	}
}

