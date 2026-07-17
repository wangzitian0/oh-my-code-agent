package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// DirSafeID turns a logical, colon-delimited ID this package or
// internal/context produces (e.g. "generation:sha256:abcd...",
// "worktree:sha256:abcd...") into a plain filesystem-safe directory name.
// ":" is the only character these IDs are ever built with (domain.Canonical
// Digest's "sha256:" prefix, and this project's own "<kind>:" ID prefix
// convention -- see internal/context/worktree.go's Worktree.ID and
// generationid.go's GenerationID), so a simple, deterministic, injective-
// enough replacement is sufficient; "/" is stripped defensively even though
// no known ID shape contains one, since a stray "/" would otherwise be
// silently misread as a path separator by every filepath.Join call below.
func DirSafeID(id string) string {
	return strings.NewReplacer(":", "-", "/", "-").Replace(id)
}

// EnsureGeneration returns req's compiled bootstrap generation, compiling it
// under generationsRoot with Bootstrap only if an equivalent generation is
// not already present there. Generation IDs are content-addressed
// (GenerationID) and Bootstrap is idempotent for identical inputs (PR-09's
// own "rebuilding from identical inputs yields the identical generation ID"
// AC, proven by TestBootstrap_RebuildingIntoFreshOutputDir_YieldsIdenticalID),
// so a present, valid outputDir for the same ID is -- by construction --
// identical to what Bootstrap would produce again; recompiling it would be
// wasted work, not a correctness improvement.
//
// This is the M1 "no Activation/Ledger yet" scope decision this PR's issue
// documents explicitly: there is deliberately no separate pending-then-
// activate step here (that is M2/Reconciler scope, docs/architecture/
// runtime.md §5.4) -- a freshly compiled generation becomes usable the
// moment EnsureGeneration returns, and a caller (omca env, omca run) is
// expected to immediately call SetCurrentGeneration with the result.
//
// generationsRoot is caller-supplied and never resolved internally (the
// same discipline Bootstrap's own outputDir parameter already has) --
// cmd/omca's command layer is the only code that turns a worktree ID into a
// real XDG state path and passes it in here.
func EnsureGeneration(req BootstrapRequest, generationsRoot string) (domain.Generation, string, error) {
	if generationsRoot == "" {
		return domain.Generation{}, "", fmt.Errorf("runtime: EnsureGeneration: generationsRoot is required")
	}
	if !filepath.IsAbs(generationsRoot) {
		return domain.Generation{}, "", fmt.Errorf("runtime: EnsureGeneration: generationsRoot %q is not absolute", generationsRoot)
	}

	id, err := GenerationID(req)
	if err != nil {
		return domain.Generation{}, "", err
	}
	outputDir := filepath.Join(generationsRoot, DirSafeID(id))

	if gen, readErr := ReadGenerationManifest(outputDir); readErr == nil {
		if gen.Metadata.ID != id {
			// Should be unreachable: outputDir's own name is derived from
			// id, so a manifest present there naming a different ID means
			// something outside this package wrote into a content-addressed
			// path it does not own. Fail loudly rather than silently
			// treating a mismatched generation as current.
			return domain.Generation{}, "", fmt.Errorf("runtime: EnsureGeneration: %s contains a manifest for generation %q, expected %q", outputDir, gen.Metadata.ID, id)
		}
		return gen, outputDir, nil
	} else if !os.IsNotExist(readErr) {
		return domain.Generation{}, "", fmt.Errorf("runtime: EnsureGeneration: existing generation directory %s is present but its manifest failed validation, refusing to overwrite a content-addressed path: %w", outputDir, readErr)
	}

	gen, err := Bootstrap(req, outputDir)
	if err != nil {
		return domain.Generation{}, "", err
	}
	return gen, outputDir, nil
}

// ReadGenerationManifest reads and validates generationDir/manifest.json,
// the manifest Bootstrap always writes at a compiled generation's root. A
// missing manifest.json returns an error satisfying os.IsNotExist (so
// EnsureGeneration can tell "nothing compiled here yet" apart from "this
// exists and is broken"); a present-but-invalid manifest returns a non-nil,
// non-os.IsNotExist error.
func ReadGenerationManifest(generationDir string) (domain.Generation, error) {
	data, err := os.ReadFile(filepath.Join(generationDir, "manifest.json"))
	if err != nil {
		return domain.Generation{}, err
	}
	var gen domain.Generation
	if err := json.Unmarshal(data, &gen); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: ReadGenerationManifest: %s: %w", generationDir, err)
	}
	if err := domain.ValidateGeneration(gen); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: ReadGenerationManifest: %s: %w", generationDir, err)
	}
	return gen, nil
}

// CurrentRecord is the small, OMCA-local bookkeeping SetCurrentGeneration
// writes alongside the "current" pointer for one host -- deliberately NOT a
// field on domain.Generation. `omca doctor`'s "host binary moved since
// qualification" acceptance criterion needs to compare the host binary's
// path/version at generation-build time against a fresh detection, but
// domain.Generation (schemas/protocol/generation.v1alpha1.schema.json) has
// no such field, and inventing one there would misrepresent local CLI
// bookkeeping as shared protocol state every adapter/report/knowledge
// consumer has to understand. This type is this package's own, purely
// informational sidecar, read only by internal/shim and cmd/omca's doctor.
type CurrentRecord struct {
	GenerationID   string `json:"generationId"`
	HostBinaryPath string `json:"hostBinaryPath"`
	HostVersion    string `json:"hostVersion"`
	RecordedAt     string `json:"recordedAt"`
}

// currentLinkPath / currentRecordPath compute the two files that together
// make up one host's "current" pointer under worktreeStateDir (docs/
// architecture/README.md §8's `current -> generations/<generation-id>`
// layout, narrowed to one pointer per host per this PR's per-host
// generation scope -- see internal/runtime/doc.go's "Per-host, not
// per-worktree, generations in this PR").
func currentLinkPath(worktreeStateDir, host string) string {
	return filepath.Join(worktreeStateDir, "current", host)
}

func currentRecordPath(worktreeStateDir, host string) string {
	return currentLinkPath(worktreeStateDir, host) + ".json"
}

// SetCurrentGeneration makes generationDir the "current" generation for
// host under worktreeStateDir: a symlink at
// worktreeStateDir/current/<host> pointing at generationDir (relative, so
// the whole worktree state tree stays relocatable), plus a CurrentRecord
// JSON sidecar recording generation/binary identity at the moment this ran.
//
// This is the "lighter form of a current pointer" this PR's own scope note
// explicitly allows without building the full M2 Activation/Ledger
// machinery (docs/project/roadmap.md M2): there is no "pending" distinct
// from "current" in M1, so SetCurrentGeneration is simply called right
// after EnsureGeneration returns, unconditionally replacing whatever the
// pointer previously named. Both files are written to a temporary sibling
// path first and then os.Rename'd into place, which POSIX guarantees is
// atomic for a same-filesystem rename -- not the compare-and-swap
// M2/Reconciler will eventually need, but enough that a concurrent reader
// (the PATH shim, doctor) never observes a half-written pointer.
func SetCurrentGeneration(worktreeStateDir, host, generationDir string, gen domain.Generation, detection hostcontext.HostDetection, now time.Time) error {
	if worktreeStateDir == "" {
		return fmt.Errorf("runtime: SetCurrentGeneration: worktreeStateDir is required")
	}
	if err := domain.ValidateHostID(host); err != nil {
		return fmt.Errorf("runtime: SetCurrentGeneration: %w", err)
	}
	currentDir := filepath.Join(worktreeStateDir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		return fmt.Errorf("runtime: SetCurrentGeneration: %w", err)
	}

	linkPath := currentLinkPath(worktreeStateDir, host)
	rel, err := filepath.Rel(currentDir, generationDir)
	if err != nil {
		return fmt.Errorf("runtime: SetCurrentGeneration: %w", err)
	}
	suffix := fmt.Sprintf(".tmp-%d-%d", os.Getpid(), now.UnixNano())
	tmpLink := linkPath + suffix
	_ = os.Remove(tmpLink) // best-effort: clear any leftover from a prior failed attempt
	if err := os.Symlink(rel, tmpLink); err != nil {
		return fmt.Errorf("runtime: SetCurrentGeneration: %w", err)
	}
	if err := os.Rename(tmpLink, linkPath); err != nil {
		_ = os.Remove(tmpLink)
		return fmt.Errorf("runtime: SetCurrentGeneration: %w", err)
	}

	record := CurrentRecord{
		GenerationID:   gen.Metadata.ID,
		HostBinaryPath: detection.BinaryPath,
		HostVersion:    detection.Version,
		RecordedAt:     now.UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("runtime: SetCurrentGeneration: %w", err)
	}
	recordPath := currentRecordPath(worktreeStateDir, host)
	tmpRecord := recordPath + suffix
	_ = os.Remove(tmpRecord)
	if err := os.WriteFile(tmpRecord, data, 0o644); err != nil {
		return fmt.Errorf("runtime: SetCurrentGeneration: %w", err)
	}
	if err := os.Rename(tmpRecord, recordPath); err != nil {
		_ = os.Remove(tmpRecord)
		return fmt.Errorf("runtime: SetCurrentGeneration: %w", err)
	}
	return nil
}

// CurrentGenerationDir resolves host's "current" generation directory under
// worktreeStateDir (SetCurrentGeneration's symlink target), returning an
// absolute, cleaned path. A caller that finds no pointer at all (this host
// has never had SetCurrentGeneration called for it in this worktree state
// dir) gets an os.IsNotExist-satisfying error -- an expected, non-fatal
// outcome (internal/shim.Build and doctor's staleness check both treat it
// as "not yet managed," not a crash).
func CurrentGenerationDir(worktreeStateDir, host string) (string, error) {
	linkPath := currentLinkPath(worktreeStateDir, host)
	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	return filepath.Clean(target), nil
}

// ReadCurrentRecord reads the CurrentRecord sidecar SetCurrentGeneration
// wrote for host under worktreeStateDir.
func ReadCurrentRecord(worktreeStateDir, host string) (CurrentRecord, error) {
	data, err := os.ReadFile(currentRecordPath(worktreeStateDir, host))
	if err != nil {
		return CurrentRecord{}, err
	}
	var rec CurrentRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return CurrentRecord{}, fmt.Errorf("runtime: ReadCurrentRecord: %w", err)
	}
	return rec, nil
}
