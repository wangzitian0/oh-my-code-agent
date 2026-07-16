package domain

import "fmt"

// ActivationSelection is a set of asset IDs to enable or disable.
type ActivationSelection struct {
	Skills     []string `json:"skills,omitempty"`
	MCPServers []string `json:"mcpServers,omitempty"`
}

// HostActivation refines the host-neutral selection for exactly one
// canonical host ID (docs/product/requirements.md §4.3).
type HostActivation struct {
	Enable  ActivationSelection `json:"enable,omitempty"`
	Disable ActivationSelection `json:"disable,omitempty"`
}

// ActivationMetadata carries the worktree a local Activation belongs to.
type ActivationMetadata struct {
	Worktree string `json:"worktree"`
}

// ActivationSpec is the body of an Activation document. Hosts entries follow
// the same rules as host-scoped Profile intent: they refine the host-neutral
// selection for one host and can never re-enable a DENIED asset.
type ActivationSpec struct {
	Enable  ActivationSelection       `json:"enable,omitempty"`
	Disable ActivationSelection       `json:"disable,omitempty"`
	Hosts   map[string]HostActivation `json:"hosts,omitempty"`
}

// Activation records worktree-specific choices without modifying a shared
// Profile (docs/product/requirements.md §4.3).
type Activation struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   ActivationMetadata `json:"metadata"`
	Spec       ActivationSpec     `json:"spec"`
}

// ValidateActivation validates an Activation document in isolation:
// apiVersion/kind, required metadata, and hosts-selector keys that name
// canonical host IDs. It does not know about Profile policy — see
// ValidateActivationAgainstProfiles for the DENIED cross-check.
func ValidateActivation(a Activation) error {
	if err := ValidateAPIVersion("Activation", a.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind("Activation", a.Kind); err != nil {
		return err
	}
	if a.Metadata.Worktree == "" {
		return fmt.Errorf("Activation: metadata.worktree is required")
	}
	for host := range a.Spec.Hosts {
		if err := ValidateHostID(host); err != nil {
			return fmt.Errorf("Activation: spec.hosts: %w", err)
		}
	}
	return nil
}

// deniedScope records, for one asset ID, the set of hosts a Profile denies
// it for. An empty (but present) host set means the deny is host-neutral and
// applies to every host.
type deniedScope struct {
	hostNeutral bool
	hosts       map[string]bool
}

func (d deniedScope) appliesTo(host string) bool {
	if d.hostNeutral {
		return true
	}
	return d.hosts[host]
}

// deniedAssets collects every DENIED asset ID across a set of Profiles.
func deniedAssets(profiles []Profile) map[string]deniedScope {
	denied := map[string]deniedScope{}
	record := func(ref AssetRef) {
		if ref.Intent != IntentDenied {
			return
		}
		scope := denied[ref.ID]
		if len(ref.Hosts) == 0 {
			scope.hostNeutral = true
		} else {
			if scope.hosts == nil {
				scope.hosts = map[string]bool{}
			}
			for _, h := range ref.Hosts {
				scope.hosts[h] = true
			}
		}
		denied[ref.ID] = scope
	}
	for _, p := range profiles {
		for _, ref := range p.Spec.Assets.Skills {
			record(ref)
		}
		for _, ref := range p.Spec.Assets.MCPServers {
			record(ref)
		}
		for _, ref := range p.Spec.Assets.Instructions {
			record(ref)
		}
	}
	return denied
}

// ValidateActivationAgainstProfiles rejects an Activation that tries to
// enable an asset the applicable Profiles deny, host-neutrally or for the
// specific host the Activation entry targets. DENIED can never be weakened
// by a lower scope (init.md invariant; docs/product/requirements.md §4.3).
//
// This checks only the direct scope an Activation entry names; it does not
// perform full cross-host intent resolution (e.g. whether a host-neutral
// enable indirectly reaches a host that has its own host-scoped deny). Full
// resolution across REQUIRED/DEFAULT/AVAILABLE/DENIED and hosts selectors is
// the intent resolution engine's responsibility (roadmap PR-13).
func ValidateActivationAgainstProfiles(a Activation, profiles []Profile) error {
	denied := deniedAssets(profiles)

	checkSelection := func(host string, sel ActivationSelection) error {
		for _, id := range sel.Skills {
			if scope, ok := denied[id]; ok && scope.appliesTo(host) {
				return fmt.Errorf("Activation: cannot enable skill %q: denied by profile policy", id)
			}
		}
		for _, id := range sel.MCPServers {
			if scope, ok := denied[id]; ok && scope.appliesTo(host) {
				return fmt.Errorf("Activation: cannot enable mcpServer %q: denied by profile policy", id)
			}
		}
		return nil
	}

	if err := checkSelection("", a.Spec.Enable); err != nil {
		return err
	}
	for host, ha := range a.Spec.Hosts {
		if err := checkSelection(host, ha.Enable); err != nil {
			return err
		}
	}
	return nil
}
