package plugin

import "fmt"

// Registry is the in-process plugin registry. Core packages look up an
// adapter by canonical host ID and drive it through the HostAdapter
// interface; they never import an adapter package directly
// (docs/architecture/README.md §9; ADR 0005).
type Registry struct {
	expectedContractVersion string
	adapters                map[string]registryEntry
}

type registryEntry struct {
	manifest PluginManifest
	adapter  HostAdapter
}

// NewRegistry creates a registry that only accepts manifests whose
// ContractVersion has the same major version as expectedContractVersion
// (typically plugin.ContractVersion).
func NewRegistry(expectedContractVersion string) *Registry {
	return &Registry{
		expectedContractVersion: expectedContractVersion,
		adapters:                make(map[string]registryEntry),
	}
}

// Register validates manifest, rejects a ContractVersion major-version
// mismatch, and adds adapter for every host the manifest declares. It
// rejects registering a second adapter for the same host ID.
func (r *Registry) Register(manifest PluginManifest, adapter HostAdapter) error {
	if adapter == nil {
		return fmt.Errorf("plugin: cannot register a nil adapter")
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("plugin: invalid manifest: %w", err)
	}
	if err := CompatibleContractVersion(r.expectedContractVersion, manifest.ContractVersion); err != nil {
		return fmt.Errorf("%w: %v", ErrContractVersionMismatch, err)
	}
	for _, host := range manifest.Hosts {
		if _, exists := r.adapters[host.HostID]; exists {
			return fmt.Errorf("plugin: host %q is already registered", host.HostID)
		}
	}
	for _, host := range manifest.Hosts {
		r.adapters[host.HostID] = registryEntry{manifest: manifest, adapter: adapter}
	}
	return nil
}

// Lookup returns the adapter registered for the canonical host ID hostID, if
// any.
func (r *Registry) Lookup(hostID string) (HostAdapter, bool) {
	entry, ok := r.adapters[hostID]
	if !ok {
		return nil, false
	}
	return entry.adapter, true
}

// Manifest returns the manifest the adapter for hostID was registered with,
// if any.
func (r *Registry) Manifest(hostID string) (PluginManifest, bool) {
	entry, ok := r.adapters[hostID]
	if !ok {
		return PluginManifest{}, false
	}
	return entry.manifest, true
}
