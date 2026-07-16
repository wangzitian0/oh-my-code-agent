package plugin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// ContractVersion is the plugin contract version this build of the package
// implements. It follows a "vMAJOR" or "vMAJOR.MINOR" shape; within one major
// version the contract evolves additive-only, so a manifest declaring any
// "v1.x" is compatible with a "v1" registry (ADR 0005 item 4).
const ContractVersion = "v1"

// HostSelector narrows a manifest to one canonical host, the surfaces it
// supports, and the native version range it has been qualified against
// (docs/architecture/README.md §9 "Hosts []HostSelector // canonical host ID,
// surfaces, version ranges"; docs/knowledge/README.md §4 metadata.host,
// metadata.versionRange).
type HostSelector struct {
	HostID       string
	Surfaces     []string
	VersionRange string
}

// Validate checks that a HostSelector names a canonical host ID and carries
// the surfaces and version range every selector must declare.
func (s HostSelector) Validate() error {
	if err := domain.ValidateHostID(s.HostID); err != nil {
		return fmt.Errorf("host selector: %w", err)
	}
	if len(s.Surfaces) == 0 {
		return fmt.Errorf("host selector %q: surfaces must not be empty", s.HostID)
	}
	for _, surface := range s.Surfaces {
		if surface == "" {
			return fmt.Errorf("host selector %q: surface entries must not be empty", s.HostID)
		}
	}
	if s.VersionRange == "" {
		return fmt.Errorf("host selector %q: versionRange is required", s.HostID)
	}
	return nil
}

// KnowledgeRef references one Knowledge Pack this adapter depends on
// (docs/knowledge/README.md §4 metadata.id, e.g. "codex:cli:0.144"). The
// Knowledge Pack type itself is a later PR's job; this manifest only needs
// the reference shape.
type KnowledgeRef struct {
	ID string
}

// FixtureRef references one qualification fixture case
// (docs/architecture/README.md §6 "fixtures/<host>/<version>/<case>"). The
// fixture harness itself is a later PR's job; this manifest only needs the
// reference shape.
type FixtureRef struct {
	Path string
}

// PluginManifest is what one host adapter plugin declares about itself
// (docs/architecture/README.md §9).
type PluginManifest struct {
	AdapterID       AdapterID
	AdapterVersion  string
	ContractVersion string
	Hosts           []HostSelector
	KnowledgePacks  []KnowledgeRef
	Fixtures        []FixtureRef
}

// Validate checks the manifest's own shape: required identity fields and
// well-formed host selectors. It does not check ContractVersion
// compatibility against any particular registry — see
// CompatibleContractVersion and Registry.Register for that.
func (m PluginManifest) Validate() error {
	if m.AdapterID == "" {
		return fmt.Errorf("plugin manifest: adapterID is required")
	}
	if m.AdapterVersion == "" {
		return fmt.Errorf("plugin manifest %q: adapterVersion is required", m.AdapterID)
	}
	if m.ContractVersion == "" {
		return fmt.Errorf("plugin manifest %q: contractVersion is required", m.AdapterID)
	}
	if len(m.Hosts) == 0 {
		return fmt.Errorf("plugin manifest %q: at least one host selector is required", m.AdapterID)
	}
	seenHosts := make(map[string]bool, len(m.Hosts))
	for _, host := range m.Hosts {
		if err := host.Validate(); err != nil {
			return fmt.Errorf("plugin manifest %q: %w", m.AdapterID, err)
		}
		// The registry keys one adapter per host ID (Registry.Lookup), so a
		// manifest declaring the same host ID twice can never be served
		// correctly regardless of how its surfaces differ between entries.
		if seenHosts[host.HostID] {
			return fmt.Errorf("plugin manifest %q: host %q is declared more than once", m.AdapterID, host.HostID)
		}
		seenHosts[host.HostID] = true
	}
	for _, kp := range m.KnowledgePacks {
		if kp.ID == "" {
			return fmt.Errorf("plugin manifest %q: knowledge pack ref id must not be empty", m.AdapterID)
		}
	}
	for _, f := range m.Fixtures {
		if f.Path == "" {
			return fmt.Errorf("plugin manifest %q: fixture ref path must not be empty", m.AdapterID)
		}
	}
	return nil
}

// contractMajorVersion extracts the leading "vN" major component from a
// contract version string such as "v1" or "v1.3". It fails closed on any
// value that does not start with a numeric major version after a leading
// "v".
func contractMajorVersion(v string) (int, error) {
	if !strings.HasPrefix(v, "v") {
		return 0, fmt.Errorf("contract version %q: must start with %q", v, "v")
	}
	rest := v[1:]
	if idx := strings.IndexByte(rest, '.'); idx >= 0 {
		rest = rest[:idx]
	}
	major, err := strconv.Atoi(rest)
	if err != nil || major < 0 {
		return 0, fmt.Errorf("contract version %q: invalid major version component", v)
	}
	return major, nil
}

// CompatibleContractVersion reports whether candidate's major version
// matches expected's major version. Within one major version the contract is
// additive-only, so any minor revision of a matching major version is
// compatible; a differing major version is a breaking-change boundary and is
// never compatible (ADR 0005 "Contract versioning policy").
func CompatibleContractVersion(expected, candidate string) error {
	expMajor, err := contractMajorVersion(expected)
	if err != nil {
		return fmt.Errorf("expected version: %w", err)
	}
	gotMajor, err := contractMajorVersion(candidate)
	if err != nil {
		return fmt.Errorf("candidate version: %w", err)
	}
	if expMajor != gotMajor {
		return fmt.Errorf("contract major version %d (from %q) does not match expected major version %d (from %q)", gotMajor, candidate, expMajor, expected)
	}
	return nil
}
