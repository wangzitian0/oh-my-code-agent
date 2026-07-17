package observe

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// observeRoot applies every rule in rules under root (an already-verified-
// present directory), building one Observation per discovered source. It
// gathers every candidate path first, sorts them, then reads and builds —
// never relying on filepath.WalkDir's own (already-deterministic, but not
// a documented contract this package should depend on) directory-entry
// order.
func observeRoot(host, hostVersion, surface, scopeKind, root string, rules []sourceRule) ([]domain.Observation, error) {
	var out []domain.Observation

	for _, rule := range rules {
		var candidates []string

		switch rule.kind {
		case ruleCandidateFiles:
			for _, name := range rule.files {
				candidates = append(candidates, filepath.Join(root, name))
			}

		case ruleWalkDir:
			dir := root
			if rule.dir != "" {
				dir = filepath.Join(root, rule.dir)
			}
			info, err := os.Stat(dir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("observe: observeRoot: stat %s: %w", dir, err)
			}
			if !info.IsDir() {
				continue
			}
			err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					// A directory-listing failure (e.g. permission denied
					// enumerating a subdirectory) means this package cannot
					// even determine what exists here — a more fundamental
					// problem than one file's content being unreadable
					// (handled below via readErr, without aborting the
					// walk). This is surfaced as a real error rather than
					// silently degraded.
					return fmt.Errorf("walk %s: %w", path, walkErr)
				}
				if d.IsDir() {
					return nil
				}
				if rule.marker != "" && d.Name() != rule.marker {
					return nil
				}
				candidates = append(candidates, path)
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("observe: observeRoot: %w", err)
			}
		}

		sort.Strings(candidates)
		for _, path := range candidates {
			obs, present, err := observeFile(host, hostVersion, surface, rule.concept, scopeKind, root, path)
			if err != nil {
				return nil, err
			}
			if present {
				out = append(out, obs)
			}
		}
	}

	return out, nil
}

// observeFile builds the Observation for one candidate path, or reports
// present=false if path simply does not exist (a candidate filename that
// does not apply to this host layout — not every candidate is expected to
// exist, and a missing one is not an error or a record, it is silence).
//
// A path that Lstat proves exists but whose content cannot subsequently be
// read (permission denied, a dangling symlink, ...) still produces a
// record: existence is E0 (EvidenceLevelDiscovered) evidence in its own
// right, and silently dropping a source this package can prove is there
// would defeat the "lossless inventory" goal the PR-08 acceptance criteria
// name. Only genuine non-existence (os.IsNotExist) is silent.
func observeFile(host, hostVersion, surface, concept, scopeKind, scopeRoot, path string) (domain.Observation, bool, error) {
	info, statErr := os.Lstat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return domain.Observation{}, false, nil
		}
		return domain.Observation{}, false, fmt.Errorf("observe: observeFile: stat %s: %w", path, statErr)
	}
	if info.IsDir() {
		return domain.Observation{}, false, nil
	}

	data, readErr := os.ReadFile(path) // read-only: never write, never exec
	obs, err := buildObservation(host, hostVersion, surface, concept, scopeKind, scopeRoot, path, data, readErr)
	if err != nil {
		return domain.Observation{}, false, err
	}
	return obs, true, nil
}

// discoveredOnlyPlaceholder is the fixed, content-independent value digested
// for an E0 record (see buildObservation): a stable marker distinguishing
// "we know this path exists but could not read it" from any real file
// content, so an E0 RawDigest/ParsedDigest can never collide with a real E1
// digest of actual (even empty) file content.
var discoveredOnlyPlaceholder = map[string]any{"discovered": true, "readable": false}

// buildObservation constructs one fully-populated, domain.ValidateObservation
// -clean Observation for path. If readErr is non-nil, path was proven to
// exist (observeFile already Lstat'd it) but its content could not be
// retrieved: the record is EvidenceLevelDiscovered (E0), and RawDigest/
// ParsedDigest are both the digest of a fixed placeholder rather than of any
// content (there is none to digest). Otherwise the record is
// EvidenceLevelParsed (E1): RawDigest is the canonical digest of the raw
// file text, and OpaqueVendorFields carries the source's content —
// losslessly parsed into a generic JSON value for a ".json"-suffixed source
// (Claude Code's .claude.json/.mcp.json — every field survives because
// nothing is cherry-picked into a hand-modeled struct) or else retained
// verbatim as opaque text (Instructions markdown, Codex's TOML config,
// SKILL.md) — see doc.go's redaction-safety point for why both branches are
// still safe once passed through internal/domain/redact at the output
// boundary. ParsedDigest is the canonical digest of that same
// OpaqueVendorFields value, so it changes if and only if the retained
// content changes.
func buildObservation(host, hostVersion, surface, concept, scopeKind, scopeRoot, path string, data []byte, readErr error) (domain.Observation, error) {
	var (
		evidenceLevel           domain.EvidenceLevel
		rawDigest, parsedDigest string
		opaque                  map[string]any
	)

	if readErr != nil {
		evidenceLevel = domain.EvidenceLevelDiscovered
		digest, err := domain.CanonicalDigest(discoveredOnlyPlaceholder)
		if err != nil {
			return domain.Observation{}, fmt.Errorf("observe: buildObservation: %w", err)
		}
		rawDigest = digest
		parsedDigest = digest
		opaque = map[string]any{"readable": false}
	} else {
		evidenceLevel = domain.EvidenceLevelParsed
		raw := string(data)
		rd, err := domain.CanonicalDigest(raw)
		if err != nil {
			return domain.Observation{}, fmt.Errorf("observe: buildObservation: %w", err)
		}
		rawDigest = rd

		opaque = map[string]any{"content": parseContent(path, data)}
		pd, err := domain.CanonicalDigest(opaque)
		if err != nil {
			return domain.Observation{}, fmt.Errorf("observe: buildObservation: %w", err)
		}
		parsedDigest = pd
	}

	obs := domain.Observation{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Observation",
		Metadata: domain.Metadata{
			ID: fmt.Sprintf("%s:%s:%s", host, concept, path),
		},
		Spec: domain.ObservationSpec{
			Host:    domain.ObservationHost{ID: host, Version: hostVersion},
			Surface: surface,
			Concept: concept,
			Source: domain.ObservationSource{
				Kind:   "file",
				Path:   path,
				Digest: rawDigest,
			},
			Scope: domain.ObservationScope{
				Kind: scopeKind,
				Root: scopeRoot,
			},
			Disposition:        domain.DispositionDiscovered,
			EvidenceLevel:      evidenceLevel,
			RawDigest:          rawDigest,
			ParsedDigest:       parsedDigest,
			OpaqueVendorFields: opaque,
		},
	}
	if err := domain.ValidateObservation(obs); err != nil {
		return domain.Observation{}, fmt.Errorf("observe: buildObservation: built an invalid Observation for %s: %w", path, err)
	}
	return obs, nil
}

// parseContent returns the value this package retains for path's content,
// opaquely, inside OpaqueVendorFields["content"]: a generic, fully lossless
// JSON decode for a ".json"-suffixed path (both of this PR's JSON-shaped MCP
// sources), or the raw text itself for everything else (Instructions
// markdown, Codex's TOML config, SKILL.md's YAML-frontmatter-plus-markdown
// body). A ".json" file whose content fails to parse (malformed JSON) falls
// back to raw text rather than erroring the whole observation — this
// package's job is lossless inventory, not validation, so a source that
// exists but is not valid JSON is still fully retained, just not
// structurally decoded.
//
// TOML is deliberately never parsed: this project has no TOML dependency
// (go.mod: only gopkg.in/yaml.v3), and Codex's config.toml is the one source
// in this PR's scope that would need one. Keeping it as opaque text avoids
// either hand-rolling a parser (real risk of a subtly lossy implementation
// masquerading as "lossless") or adding a dependency for a single call site.
// This is safe for the AC's redaction requirement too: a TOML
// `KEY = "value"` assignment is exactly the shape
// internal/domain/redact's secretShapePattern's ENV_STYLE_NAME=value branch
// already matches (see redact_test.go in this package), so an env-block
// secret in a Codex config.toml is still caught even though the file is
// never structurally parsed.
func parseContent(path string, data []byte) any {
	if strings.HasSuffix(path, ".json") {
		var generic any
		if err := json.Unmarshal(data, &generic); err == nil {
			return generic
		}
	}
	return string(data)
}
