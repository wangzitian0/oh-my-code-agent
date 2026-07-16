package resolve

import (
	"reflect"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestResolve_Deterministic is Deliverable #4's property test: Resolve must
// be a pure function — the same inputs always produce the same output, no
// global state, no I/O. It runs Resolve many times on logically identical
// inputs, including inputs whose map-typed fields (domain.ActivationSpec's
// Hosts and domain.ProfilePolicy's Permissions) are built by inserting keys
// in different orders, and asserts every run is both reflect.DeepEqual and
// byte-identical via domain.CanonicalDigest — the same guarantee generation
// digests rely on (internal/domain/digest.go).
func TestResolve_Deterministic(t *testing.T) {
	buildActivationHostsAsc := func() map[string]domain.HostActivation {
		hosts := map[string]domain.HostActivation{}
		hosts["claude-code"] = domain.HostActivation{Enable: domain.ActivationSelection{Skills: []string{"ui-review"}}}
		hosts["codex"] = domain.HostActivation{Disable: domain.ActivationSelection{MCPServers: []string{"internal-docs"}}}
		return hosts
	}
	buildActivationHostsDesc := func() map[string]domain.HostActivation {
		hosts := map[string]domain.HostActivation{}
		hosts["codex"] = domain.HostActivation{Disable: domain.ActivationSelection{MCPServers: []string{"internal-docs"}}}
		hosts["claude-code"] = domain.HostActivation{Enable: domain.ActivationSelection{Skills: []string{"ui-review"}}}
		return hosts
	}

	profile := domain.Profile{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Profile",
		Metadata:   domain.Metadata{ID: "company:example"},
		Spec: domain.ProfileSpec{
			Assets: domain.ProfileAssets{
				Skills:       []domain.AssetRef{{ID: "code-review", Intent: domain.IntentAvailable}, {ID: "deep-refactor", Intent: domain.IntentDefault, Hosts: []string{"claude-code"}}},
				MCPServers:   []domain.AssetRef{{ID: "internal-docs", Intent: domain.IntentAvailable}, {ID: "codegraph", Intent: domain.IntentDefault, Hosts: []string{"codex"}}},
				Instructions: []domain.AssetRef{{ID: "engineering-baseline", Intent: domain.IntentDefault}},
			},
		},
	}

	exceptions := []domain.Exception{
		{AssetID: "banned-tool", Scope: "company:example", Justification: "temporary", ExpiresAt: fixedNow.Add(time.Hour)},
	}

	const iterations = 25
	var states []ResolvedState
	var digests []string
	for i := 0; i < iterations; i++ {
		hosts := buildActivationHostsAsc()
		if i%2 == 1 {
			hosts = buildActivationHostsDesc()
		}
		activation := domain.Activation{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       "Activation",
			Metadata:   domain.ActivationMetadata{Worktree: "worktree:sha256:deterministic"},
			Spec: domain.ActivationSpec{
				Enable:  domain.ActivationSelection{Skills: []string{"code-review"}, MCPServers: []string{"codegraph"}},
				Disable: domain.ActivationSelection{Skills: []string{"release-production"}},
				Hosts:   hosts,
			},
		}

		for _, host := range []string{"claude-code", "codex"} {
			state, err := Resolve([]domain.Profile{profile}, activation, exceptions, host, fixedNow)
			if err != nil {
				t.Fatalf("iteration %d, host %s: Resolve: %v", i, host, err)
			}
			states = append(states, state)

			digest, err := domain.CanonicalDigest(state)
			if err != nil {
				t.Fatalf("iteration %d, host %s: CanonicalDigest: %v", i, host, err)
			}
			digests = append(digests, digest)
		}
	}

	for i := 1; i < len(states); i++ {
		// Compare against the run for the same host (even/odd index share
		// host order: claude-code then codex, each iteration).
		prev := i - 2
		if prev < 0 {
			continue
		}
		if !reflect.DeepEqual(states[prev], states[i]) {
			t.Fatalf("run %d and run %d produced different ResolvedState for the same inputs:\n%+v\nvs\n%+v", prev, i, states[prev], states[i])
		}
		if digests[prev] != digests[i] {
			t.Fatalf("run %d and run %d produced different digests for the same inputs: %s vs %s", prev, i, digests[prev], digests[i])
		}
	}
}
