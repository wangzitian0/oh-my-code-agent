package qualify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// ObservationRule tells ObserveSandbox how to tag every file found under one
// sandbox subtree: which ontology concept it represents and which scope
// (docs/ontology/README.md §2) it was discovered at. A case supplies one
// rule per subtree it populated (docs/knowledge/README.md §3's input/
// layout: home/, project/, and the host-specific home).
type ObservationRule struct {
	// Root names the sandbox subtree: "home", "project", "codex-home", or
	// "claude-config".
	Root string `yaml:"root"`
	// Concept is the canonical ontology concept ID (ontology.Concept),
	// e.g. "instruction", "skill", "mcp_server".
	Concept string `yaml:"concept"`
	// Scope is the canonical scope kind (domain.KnownScopeKinds), e.g.
	// "user" or "workspace".
	Scope string `yaml:"scope"`
	// Surface is the host surface, e.g. "cli".
	Surface string `yaml:"surface"`
}

// path resolves a rule's Root label to this sandbox's actual directory.
func (sb *Sandbox) path(root string) (string, bool) {
	switch root {
	case "home":
		return sb.Home, true
	case "project":
		return sb.Project, true
	case "codex-home":
		if sb.CodexHome == "" {
			return "", false
		}
		return sb.CodexHome, true
	case "claude-config":
		if sb.ClaudeConfigDir == "" {
			return "", false
		}
		return sb.ClaudeConfigDir, true
	default:
		return "", false
	}
}

// ObserveSandbox inventories every regular file under the subtrees named by
// rules, read-only: it opens and hashes content but never writes, creates,
// removes, or executes anything, mirroring
// internal/plugin/conformance.FakeAdapter.Observe's read-only walk (the
// property that PR-05's Run conformance suite proves against a HostAdapter).
//
// Each Observation's Source.Path is Root-prefixed and relative (e.g.
// "project/AGENTS.md"), never the sandbox's absolute temporary path, so two
// runs from the same committed fixture input/ produce byte-identical
// observations regardless of where the OS placed the temp directory — this
// is what makes the case's content digest (digest.go) reproducible.
func ObserveSandbox(sb *Sandbox, host, hostVersion string, rules []ObservationRule) ([]domain.Observation, error) {
	var observations []domain.Observation

	for _, rule := range rules {
		root, ok := sb.path(rule.Root)
		if !ok {
			continue
		}
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}

		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path) // read-only: no write, no exec
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			sum := sha256.Sum256(data)
			digest := "sha256:" + hex.EncodeToString(sum[:])

			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relLabel := filepath.ToSlash(filepath.Join(rule.Root, rel))

			obs := domain.Observation{
				APIVersion: domain.SupportedAPIVersion,
				Kind:       "Observation",
				Metadata: domain.Metadata{
					ID: fmt.Sprintf("%s:%s:%s", host, rule.Concept, relLabel),
				},
				Spec: domain.ObservationSpec{
					Host:    domain.ObservationHost{ID: host, Version: hostVersion},
					Surface: rule.Surface,
					Concept: rule.Concept,
					Source: domain.ObservationSource{
						Kind:   "file",
						Path:   relLabel,
						Digest: digest,
					},
					Scope: domain.ObservationScope{
						Kind: rule.Scope,
						Root: rule.Root,
					},
					Disposition:   domain.DispositionDiscovered,
					EvidenceLevel: domain.EvidenceLevelParsed,
					RawDigest:     digest,
				},
			}
			if err := domain.ValidateObservation(obs); err != nil {
				return fmt.Errorf("qualify: ObserveSandbox: built an invalid Observation for %s: %w", relLabel, err)
			}
			observations = append(observations, obs)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("qualify: ObserveSandbox: walk %s: %w", root, err)
		}
	}

	sort.Slice(observations, func(i, j int) bool {
		return observations[i].Metadata.ID < observations[j].Metadata.ID
	})
	return observations, nil
}
