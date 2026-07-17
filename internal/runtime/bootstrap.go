package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Bootstrap compiles req into one immutable bootstrap generation and writes
// it under outputDir: outputDir/manifest.json plus outputDir/hosts/<host>/
// <surface>/... (docs/architecture/README.md §8's generation-directory
// shape, scoped to one host per this PR's documented simplification -- see
// doc.go's "per-host, not per-worktree" section). It never reads a native
// filesystem location itself: every physical fact came from
// req.Observations, already computed by internal/observe, and outputDir is
// injected by the caller (never derived from ~/.local/state/omca or any
// other XDG default internally -- doc.go / this issue's own instructions:
// "tests build under t.TempDir(); do not hardcode ~/.local/state/omca into
// the compiler's core logic"). It never reads the clock (req.Now is the one
// timestamp it records).
//
// On success, outputDir and everything under it are read-only (issue #13
// AC "Generated artifact trees are read-only on disk" -- see readonly.go);
// a caller that needs to recompile must target a fresh outputDir, matching
// this project's "the compiler never edits current in place" invariant
// (docs/architecture/runtime.md §5.3). Bootstrap fails atomically only in
// the sense that a returned error means outputDir may contain a partial,
// still-writable tree -- it does not roll back partial writes itself
// (Reconciler-level staging/activation, which would make that guarantee, is
// PR-15/M2 scope, not this compiler's).
func Bootstrap(req BootstrapRequest, outputDir string) (domain.Generation, error) {
	if err := req.validate(); err != nil {
		return domain.Generation{}, err
	}
	if outputDir == "" {
		return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: outputDir is required")
	}
	if !filepath.IsAbs(outputDir) {
		return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: outputDir %q is not absolute", outputDir)
	}

	genID, err := GenerationID(req)
	if err != nil {
		return domain.Generation{}, err
	}
	policyDigest, err := BootstrapPolicyDigest()
	if err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: %w", err)
	}

	files, sources, err := compileHostTree(req)
	if err != nil {
		return domain.Generation{}, err
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: %w", err)
	}

	artifacts := make([]domain.GenerationArtifact, 0, len(files))
	for _, f := range files {
		fullPath := filepath.Join(outputDir, f.RelPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: %w", err)
		}
		if err := os.WriteFile(fullPath, f.Content, 0o644); err != nil {
			return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: %w", err)
		}
		digest, err := domain.CanonicalDigest(string(f.Content))
		if err != nil {
			return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: %w", err)
		}
		artifacts = append(artifacts, domain.GenerationArtifact{Path: f.RelPath, Digest: digest})
	}

	gen := domain.Generation{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Generation",
		Metadata: domain.GenerationMetadata{
			ID:        genID,
			Worktree:  req.Worktree.ID,
			Parent:    req.Parent,
			CreatedAt: req.Now.UTC().Format(time.RFC3339),
		},
		Spec: domain.GenerationSpec{
			DesiredGraphDigest: policyDigest,
			// Knowledge Pack propagation (docs/architecture/runtime.md
			// §5.3's pending-manifest field list: "Knowledge Pack IDs and
			// digests") is out of this PR's scope -- no AC requires it, and
			// internal/knowledge.Resolve is already wired into cmd/omca's
			// `omca context` independently. Left empty (not nil, so this
			// marshals as `[]`, matching the required-array shape) rather
			// than gold-plated here.
			KnowledgePacks: []domain.KnowledgePackRef{},
			Hosts: map[string]domain.GenerationHostEntry{
				req.Detection.Host: {
					Surface:        req.surface(),
					AdapterID:      AdapterID,
					AdapterVersion: req.Detection.Version,
					Ownership:      domain.OwnershipManaged,
					Artifacts:      artifacts,
				},
			},
			Sources: sources,
			Status:  "pending",
		},
	}

	if err := domain.ValidateGeneration(gen); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: compiled an invalid Generation: %w", err)
	}

	manifestPath := filepath.Join(outputDir, "manifest.json")
	manifestBytes, err := json.MarshalIndent(gen, "", "  ")
	if err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: %w", err)
	}

	if err := makeTreeReadOnly(outputDir); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Bootstrap: %w", err)
	}

	return gen, nil
}
