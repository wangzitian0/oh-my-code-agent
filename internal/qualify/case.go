package qualify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Case is one loaded fixture case directory
// (fixtures/<host>/<version>/<case>/, docs/knowledge/README.md §3).
type Case struct {
	// Dir is the case's directory, e.g.
	// fixtures/codex/0.144.5/instructions-collision.
	Dir string
	// Host, Version, Name are parsed from Dir's path segments.
	Host    string
	Version string
	Name    string

	Manifest             InvocationManifest
	ExpectedObservations []domain.Observation
	ExpectedEffective    ExpectedEffectiveDocument
}

// LoadCase reads and validates every required file in one fixture case
// directory: invocation.yaml, expected-observations.json,
// expected-effective.json, and confirms expected-generation/ exists (its
// content is a deferral note in this PR — Generation compilation is M2 work,
// docs/project/roadmap.md — not a fabricated compiled artifact).
func LoadCase(dir string) (*Case, error) {
	host, version, name, err := parseCasePath(dir)
	if err != nil {
		return nil, err
	}

	manifest, err := LoadInvocationManifest(filepath.Join(dir, "invocation.yaml"))
	if err != nil {
		return nil, err
	}
	if manifest.Host != host {
		return nil, fmt.Errorf("qualify: LoadCase: %s: invocation.yaml host %q does not match directory host %q", dir, manifest.Host, host)
	}
	if manifest.Version != version {
		return nil, fmt.Errorf("qualify: LoadCase: %s: invocation.yaml version %q does not match directory version %q", dir, manifest.Version, version)
	}

	observations, err := loadExpectedObservations(filepath.Join(dir, "expected-observations.json"))
	if err != nil {
		return nil, err
	}

	effective, err := LoadExpectedEffective(filepath.Join(dir, "expected-effective.json"))
	if err != nil {
		return nil, err
	}

	genDir := filepath.Join(dir, "expected-generation")
	info, err := os.Stat(genDir)
	if err != nil {
		return nil, fmt.Errorf("qualify: LoadCase: %s: expected-generation/ is required by docs/knowledge/README.md §3 even when empty of compiled artifacts: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("qualify: LoadCase: %s: expected-generation must be a directory", dir)
	}

	return &Case{
		Dir:                  dir,
		Host:                 host,
		Version:              version,
		Name:                 name,
		Manifest:             manifest,
		ExpectedObservations: observations,
		ExpectedEffective:    effective,
	}, nil
}

// InputDir is this case's synthetic fixture input directory.
func (c *Case) InputDir() string {
	return filepath.Join(c.Dir, "input")
}

func loadExpectedObservations(path string) ([]domain.Observation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("qualify: loadExpectedObservations: %w", err)
	}
	var observations []domain.Observation
	if err := json.Unmarshal(raw, &observations); err != nil {
		return nil, fmt.Errorf("qualify: loadExpectedObservations: %s: %w", path, err)
	}
	for i, obs := range observations {
		if err := domain.ValidateObservation(obs); err != nil {
			return nil, fmt.Errorf("qualify: loadExpectedObservations: %s: entry %d: %w", path, i, err)
		}
	}
	return observations, nil
}

// parseCasePath extracts host/version/case-name from a
// fixtures/<host>/<version>/<case> directory path.
func parseCasePath(dir string) (host, version, name string, err error) {
	clean := filepath.Clean(dir)
	name = filepath.Base(clean)
	versionDir := filepath.Dir(clean)
	version = filepath.Base(versionDir)
	hostDir := filepath.Dir(versionDir)
	host = filepath.Base(hostDir)
	if host == "" || version == "" || name == "" || host == "." || version == "." {
		return "", "", "", fmt.Errorf("qualify: parseCasePath: %s does not match fixtures/<host>/<version>/<case>", dir)
	}
	return host, version, name, nil
}
