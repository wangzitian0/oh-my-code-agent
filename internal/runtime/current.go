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

// pointerLinkPath / pointerRecordPath compute the two files that together
// make up one host's named pointer ("current" or "pending") under
// worktreeStateDir (docs/architecture/runtime.md §5's
// `current -> generations/<generation-id>` / `pending -> generations/
// <generation-id>` layout, narrowed to one pointer per host per this
// package's per-host generation scope -- see internal/runtime/doc.go's
// "Per-host, not per-worktree, generations in this PR"). Originally
// "current"-specific (PR-09's currentLinkPath/currentRecordPath); factored
// into a pointerName parameter so pending.go's identically-shaped "pending"
// pointer (PR-14, issue #18) reuses the exact same path convention instead
// of redefining it a second time.
func pointerLinkPath(worktreeStateDir, pointerName, host string) string {
	return filepath.Join(worktreeStateDir, pointerName, host)
}

func pointerRecordPath(worktreeStateDir, pointerName, host string) string {
	return pointerLinkPath(worktreeStateDir, pointerName, host) + ".json"
}

// setGenerationPointer makes generationDir the named pointer ("current" or
// "pending") for host under worktreeStateDir: a symlink at
// worktreeStateDir/<pointerName>/<host> pointing at generationDir (relative,
// so the whole worktree state tree stays relocatable), plus a CurrentRecord
// JSON sidecar recording generation/binary identity at the moment this ran.
// Both files are written to a temporary sibling path first and then
// os.Rename'd into place, which POSIX guarantees is atomic for a
// same-filesystem rename -- not the compare-and-swap PR-15's Activation
// transaction will eventually need for "current" specifically, but enough
// that a concurrent reader (the PATH shim, doctor) never observes a
// half-written pointer.
//
// This is the shared implementation behind SetCurrentGeneration (PR-09) and
// SetPendingGeneration (PR-14, pending.go): "current" and "pending" record
// exactly the same kind of fact -- which generation, compiled against which
// host binary/version, at what time -- and differ only in which pointer
// they update and when a caller chooses to call them. Neither function
// compares against the other pointer or moves one into the other; that
// comparison-and-atomic-switch is explicitly PR-15's job (docs/architecture/
// runtime.md §5.4's Activation transaction: "validate pending -> ... ->
// atomically switch current ... -> append Ledger entry"). This package's
// scope is limited to making both pointers exist and be independently
// writable/readable.
func setGenerationPointer(worktreeStateDir, pointerName, host, generationDir string, gen domain.Generation, detection hostcontext.HostDetection, now time.Time) error {
	if worktreeStateDir == "" {
		return fmt.Errorf("runtime: setGenerationPointer(%s): worktreeStateDir is required", pointerName)
	}
	if err := domain.ValidateHostID(host); err != nil {
		return fmt.Errorf("runtime: setGenerationPointer(%s): %w", pointerName, err)
	}
	pointerDir := filepath.Join(worktreeStateDir, pointerName)
	if err := os.MkdirAll(pointerDir, 0o755); err != nil {
		return fmt.Errorf("runtime: setGenerationPointer(%s): %w", pointerName, err)
	}

	linkPath := pointerLinkPath(worktreeStateDir, pointerName, host)
	rel, err := filepath.Rel(pointerDir, generationDir)
	if err != nil {
		return fmt.Errorf("runtime: setGenerationPointer(%s): %w", pointerName, err)
	}
	suffix := fmt.Sprintf(".tmp-%d-%d", os.Getpid(), now.UnixNano())
	tmpLink := linkPath + suffix
	_ = os.Remove(tmpLink) // best-effort: clear any leftover from a prior failed attempt
	if err := os.Symlink(rel, tmpLink); err != nil {
		return fmt.Errorf("runtime: setGenerationPointer(%s): %w", pointerName, err)
	}
	if err := os.Rename(tmpLink, linkPath); err != nil {
		_ = os.Remove(tmpLink)
		return fmt.Errorf("runtime: setGenerationPointer(%s): %w", pointerName, err)
	}

	record := CurrentRecord{
		GenerationID:   gen.Metadata.ID,
		HostBinaryPath: detection.BinaryPath,
		HostVersion:    detection.Version,
		RecordedAt:     now.UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("runtime: setGenerationPointer(%s): %w", pointerName, err)
	}
	recordPath := pointerRecordPath(worktreeStateDir, pointerName, host)
	tmpRecord := recordPath + suffix
	_ = os.Remove(tmpRecord)
	if err := os.WriteFile(tmpRecord, data, 0o644); err != nil {
		return fmt.Errorf("runtime: setGenerationPointer(%s): %w", pointerName, err)
	}
	if err := os.Rename(tmpRecord, recordPath); err != nil {
		_ = os.Remove(tmpRecord)
		return fmt.Errorf("runtime: setGenerationPointer(%s): %w", pointerName, err)
	}
	return nil
}

// generationPointerDir resolves the named pointer's ("current" or
// "pending") generation directory for host under worktreeStateDir, returning
// an absolute, cleaned path. A caller that finds no pointer at all gets an
// os.IsNotExist-satisfying error -- an expected, non-fatal outcome.
func generationPointerDir(worktreeStateDir, pointerName, host string) (string, error) {
	linkPath := pointerLinkPath(worktreeStateDir, pointerName, host)
	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	return filepath.Clean(target), nil
}

// readPointerRecord reads the CurrentRecord sidecar setGenerationPointer
// wrote for the named pointer ("current" or "pending") and host under
// worktreeStateDir.
func readPointerRecord(worktreeStateDir, pointerName, host string) (CurrentRecord, error) {
	data, err := os.ReadFile(pointerRecordPath(worktreeStateDir, pointerName, host))
	if err != nil {
		return CurrentRecord{}, err
	}
	var rec CurrentRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return CurrentRecord{}, fmt.Errorf("runtime: readPointerRecord(%s): %w", pointerName, err)
	}
	return rec, nil
}

// SetCurrentGeneration makes generationDir the "current" generation for
// host under worktreeStateDir. See setGenerationPointer for the shared
// implementation and PR-14's "pending" pointer (pending.go), which reuses
// it.
//
// This is the "lighter form of a current pointer" PR-09's own scope note
// explicitly allowed without building the full M2 Activation/Ledger
// machinery (docs/project/roadmap.md M2): SetCurrentGeneration is simply
// called right after EnsureGeneration returns, unconditionally replacing
// whatever the pointer previously named -- there was, at that time, no
// "pending" distinct from "current." PR-14 (issue #18) adds "pending" as a
// genuinely separate pointer (pending.go); this function's own behavior is
// unchanged.
func SetCurrentGeneration(worktreeStateDir, host, generationDir string, gen domain.Generation, detection hostcontext.HostDetection, now time.Time) error {
	return setGenerationPointer(worktreeStateDir, "current", host, generationDir, gen, detection, now)
}

// CurrentGenerationDir resolves host's "current" generation directory under
// worktreeStateDir (SetCurrentGeneration's symlink target), returning an
// absolute, cleaned path. A caller that finds no pointer at all (this host
// has never had SetCurrentGeneration called for it in this worktree state
// dir) gets an os.IsNotExist-satisfying error -- an expected, non-fatal
// outcome (internal/shim.Build and doctor's staleness check both treat it
// as "not yet managed," not a crash).
func CurrentGenerationDir(worktreeStateDir, host string) (string, error) {
	return generationPointerDir(worktreeStateDir, "current", host)
}

// ReadCurrentRecord reads the CurrentRecord sidecar SetCurrentGeneration
// wrote for host under worktreeStateDir.
func ReadCurrentRecord(worktreeStateDir, host string) (CurrentRecord, error) {
	return readPointerRecord(worktreeStateDir, "current", host)
}
