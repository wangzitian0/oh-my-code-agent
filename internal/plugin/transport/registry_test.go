package transport

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/conformance"
)

// pipedRemoteAdapter wires a RemoteAdapter to a real Serve loop over an
// in-memory pipe (no subprocess needed for this file's purpose: proving
// plugin.Registry treats a RemoteAdapter exactly like an in-process
// adapter — the real-subprocess case is separately proven end-to-end by
// conformance_test.go and sandbox_test.go). serveDone reports Serve's
// return value once the caller closes the client.
func pipedRemoteAdapter(t *testing.T, manifest plugin.PluginManifest, adapter plugin.HostAdapter) (*RemoteAdapter, <-chan error) {
	t.Helper()
	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()

	serveDone := make(chan error, 1)
	go func() { serveDone <- Serve(reqR, respW, manifest, adapter) }()

	client := newRemoteAdapter(reqW, respR, func() error {
		_ = reqW.Close()
		return nil
	})
	got, err := client.Handshake(context.Background())
	if err != nil {
		t.Fatalf("Handshake: unexpected error: %v", err)
	}
	client.manifest = got
	return client, serveDone
}

// TestRegistry_AcceptsRemoteAdapter_LikeAnyOtherHostAdapter is issue #29's
// third acceptance-criteria proof ("the core loads it without
// recompilation"): plugin.Registry.Register/Lookup never once type-asserts
// or otherwise inspects the concrete HostAdapter it is given, so a
// RemoteAdapter registers and gets looked up exactly like
// conformance.NewFakeAdapter() (or any first-party in-process adapter)
// would — the core binary that calls Register only ever needs the
// RemoteAdapter's manifest and the plugin.HostAdapter interface, never the
// external binary's own source.
func TestRegistry_AcceptsRemoteAdapter_LikeAnyOtherHostAdapter(t *testing.T) {
	manifest := plugin.PluginManifest{
		AdapterID:       "remote-fake",
		AdapterVersion:  "0.0.0-test",
		ContractVersion: plugin.ContractVersion,
		Hosts: []plugin.HostSelector{
			{HostID: "codex", Surfaces: []string{"cli"}, VersionRange: "0.144.0"},
		},
	}
	remote, serveDone := pipedRemoteAdapter(t, manifest, conformance.NewFakeAdapter())
	defer func() {
		_ = remote.Close()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return within 2s of the client closing")
		}
	}()

	registry := plugin.NewRegistry(plugin.ContractVersion)
	if err := registry.Register(remote.Manifest(), remote); err != nil {
		t.Fatalf("Register(remote adapter): unexpected error: %v", err)
	}

	looked, ok := registry.Lookup("codex")
	if !ok {
		t.Fatal("Lookup(codex) = false after registering a RemoteAdapter")
	}
	if looked.ID() != remote.ID() {
		t.Errorf("Lookup(codex).ID() = %q, want %q", looked.ID(), remote.ID())
	}

	// Drive one real call through the exact interface value the registry
	// handed back -- proof the registry's own caller-facing surface works
	// unmodified for a remote adapter, not just that Register/Lookup accept
	// the type.
	instances, err := looked.Detect(context.Background(), plugin.DetectRequest{})
	if err != nil {
		t.Fatalf("Detect via Registry.Lookup's returned adapter: unexpected error: %v", err)
	}
	if len(instances) != 1 || instances[0].HostID != "codex" {
		t.Errorf("Detect() = %+v, want one codex HostInstance", instances)
	}

	gotManifest, ok := registry.Manifest("codex")
	if !ok || gotManifest.AdapterID != manifest.AdapterID {
		t.Errorf("Manifest(codex) = %+v, %v, want %+v, true", gotManifest, ok, manifest)
	}
}

// TestRegistry_RejectsRemoteAdapterContractVersionMismatch proves the
// version-mismatch half of "a contract violation produces a clear
// diagnostic, not a crash": a RemoteAdapter whose handshake manifest
// declares an incompatible ContractVersion is rejected by
// Registry.Register's pre-existing CompatibleContractVersion check -- the
// exact same path an in-process adapter's mismatched manifest already hits
// (internal/plugin/registry_test.go's own
// TestRegisterRejectsContractMajorVersionMismatch) -- never a panic and
// never a silently-accepted incompatible plugin.
func TestRegistry_RejectsRemoteAdapterContractVersionMismatch(t *testing.T) {
	manifest := plugin.PluginManifest{
		AdapterID:       "remote-fake-v2",
		AdapterVersion:  "0.0.0-test",
		ContractVersion: "v2", // incompatible with the registry's expected v1
		Hosts: []plugin.HostSelector{
			{HostID: "codex", Surfaces: []string{"cli"}, VersionRange: "0.144.0"},
		},
	}
	remote, serveDone := pipedRemoteAdapter(t, manifest, conformance.NewFakeAdapter())
	defer func() {
		_ = remote.Close()
		<-serveDone
	}()

	registry := plugin.NewRegistry(plugin.ContractVersion) // "v1"
	err := registry.Register(remote.Manifest(), remote)
	if err == nil {
		t.Fatal("Register(remote adapter declaring v2): want an error, got nil")
	}
	if !errors.Is(err, plugin.ErrContractVersionMismatch) {
		t.Errorf("Register error = %v, want it to wrap plugin.ErrContractVersionMismatch", err)
	}
	if _, ok := registry.Lookup("codex"); ok {
		t.Fatal("Lookup(codex) succeeded after a rejected Register call")
	}
}
