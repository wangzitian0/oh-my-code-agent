package qualify

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// invocationKind is the fixed kind literal every invocation.yaml must
// declare (docs/knowledge/README.md §3, §4).
const invocationKind = "FixtureInvocation"

// InvokeSpec is one case's decision about whether to actually run the real
// host binary, and why. Attempted=false is a first-class, expected outcome
// (see doc.go): it means no safe non-interactive introspection path covers
// what this case needs to prove, so Reason must say so instead of the case
// silently skipping proof.
type InvokeSpec struct {
	Attempted bool     `yaml:"attempted"`
	Command   string   `yaml:"command,omitempty"`
	Args      []string `yaml:"args,omitempty"`
	Reason    string   `yaml:"reason"`
}

// BinaryProvenance documents how the pinned host binary version was
// acquired and identified (round-2 audit item on issue #10): install source,
// resolved on-disk path at fixture-authoring time, and a local sha256
// fingerprint of the exact installed binary bytes. This is explicitly a
// local install fingerprint, not a claim of verification against an
// official vendor-published checksum (fixtures/README.md explains why one
// was not obtainable in this pass).
type BinaryProvenance struct {
	AcquisitionMethod string `yaml:"acquisitionMethod"`
	ResolvedPath      string `yaml:"resolvedPath"`
	SHA256            string `yaml:"sha256"`
	VersionSource     string `yaml:"versionSource"`
}

// InvocationManifest is the parsed shape of one fixture case's
// invocation.yaml (docs/knowledge/README.md §3): which host/surface/version/
// platform/cwd/env this case pins, and whether (and how) it actually invokes
// the real binary.
type InvocationManifest struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Host       string            `yaml:"host"`
	Surface    string            `yaml:"surface"`
	Version    string            `yaml:"version"`
	Platform   string            `yaml:"platform"`
	Cwd        string            `yaml:"cwd"`
	Env        map[string]string `yaml:"env,omitempty"`
	Invoke     InvokeSpec        `yaml:"invoke"`
	Binary     BinaryProvenance  `yaml:"binary"`

	// ObservationRules tells ObserveSandbox which sandbox subtrees to walk
	// and how to tag what it finds (observe.go).
	ObservationRules []ObservationRule `yaml:"observationRules"`
}

// LoadInvocationManifest reads and validates one case's invocation.yaml.
func LoadInvocationManifest(path string) (InvocationManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return InvocationManifest{}, fmt.Errorf("qualify: LoadInvocationManifest: %w", err)
	}
	var m InvocationManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return InvocationManifest{}, fmt.Errorf("qualify: LoadInvocationManifest: %s: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return InvocationManifest{}, fmt.Errorf("qualify: LoadInvocationManifest: %s: %w", path, err)
	}
	return m, nil
}

// Validate rejects a structurally malformed or closed-enum-violating
// manifest, mirroring the fail-closed discipline internal/domain's
// Validate* functions use.
func (m InvocationManifest) Validate() error {
	if err := domain.ValidateAPIVersion("FixtureInvocation", m.APIVersion); err != nil {
		return err
	}
	if m.Kind != invocationKind {
		return fmt.Errorf("expected kind %q, got %q", invocationKind, m.Kind)
	}
	if err := domain.ValidateHostID(m.Host); err != nil {
		return err
	}
	if m.Surface == "" {
		return fmt.Errorf("surface is required")
	}
	if m.Version == "" {
		return fmt.Errorf("version is required")
	}
	if m.Cwd != "project" && m.Cwd != "home" {
		return fmt.Errorf("cwd must be %q or %q, got %q", "project", "home", m.Cwd)
	}
	if !m.Invoke.Attempted && m.Invoke.Reason == "" {
		return fmt.Errorf("invoke.reason is required when invoke.attempted is false (the safety boundary requires a stated reason, not a silent skip)")
	}
	if m.Invoke.Attempted && m.Invoke.Command == "" {
		return fmt.Errorf("invoke.command is required when invoke.attempted is true")
	}
	if len(m.ObservationRules) == 0 {
		return fmt.Errorf("observationRules must not be empty")
	}
	for i, r := range m.ObservationRules {
		if r.Root == "" || r.Concept == "" || r.Scope == "" || r.Surface == "" {
			return fmt.Errorf("observationRules[%d]: root, concept, scope, and surface are all required", i)
		}
		if err := domain.ValidateScopeKind(r.Scope); err != nil {
			return fmt.Errorf("observationRules[%d]: %w", i, err)
		}
	}
	return nil
}
