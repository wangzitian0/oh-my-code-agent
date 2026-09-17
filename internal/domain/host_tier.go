package domain

import "fmt"

// HostTier classifies how OMCA manages a host's runtime environment.
// - TierManaged (Tier 1): Host has decoupled configuration/state and independent credentials (e.g. Codex), enabling hermetic PATH shims and virtual HOME.
// - TierBridge (Tier 2): Host tightly couples credentials with OS Keychain or monolithic state files (e.g. Claude Code), governed via MCP Hub Bridge and lifecycle hooks without virtualizing HOME.
// - TierObserved (Tier 3): Host is purely observed or passthrough.
type HostTier string

const (
	TierManaged  HostTier = "MANAGED"
	TierBridge   HostTier = "BRIDGE"
	TierObserved HostTier = "OBSERVED"
)

// HostCapability describes runtime capabilities and isolation support for a host.
type HostCapability struct {
	Tier              HostTier `json:"tier"`
	CanVirtualizeHome bool     `json:"canVirtualizeHome"`
	SupportsHooks     bool     `json:"supportsHooks"`
	NativeMCPProtocol string   `json:"nativeMCPProtocol"` // "stdio", "http", "none"
}

// DefaultHostCapability returns the baseline capabilities for a host.
func DefaultHostCapability(host string) HostCapability {
	switch host {
	case "codex":
		return HostCapability{
			Tier:              TierManaged,
			CanVirtualizeHome: true,
			SupportsHooks:     false,
			NativeMCPProtocol: "stdio",
		}
	case "claude-code":
		return HostCapability{
			Tier:              TierBridge,
			CanVirtualizeHome: false,
			SupportsHooks:     true,
			NativeMCPProtocol: "http",
		}
	default:
		return HostCapability{
			Tier:              TierObserved,
			CanVirtualizeHome: false,
			SupportsHooks:     false,
			NativeMCPProtocol: "none",
		}
	}
}

// Valid reports whether t is a known HostTier.
func (t HostTier) Valid() bool {
	switch t {
	case TierManaged, TierBridge, TierObserved:
		return true
	default:
		return false
	}
}

// ValidateHostTier rejects invalid tiers.
func ValidateHostTier(t HostTier) error {
	if !t.Valid() {
		return fmt.Errorf("domain: invalid host tier %q", t)
	}
	return nil
}
