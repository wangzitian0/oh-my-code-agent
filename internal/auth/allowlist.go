package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// AllowlistedShare is one narrow, fixture-backed sharing exception,
// docs/architecture/runtime.md §9: "Sharing state through symlinks is
// allowed only for an explicit allowlist backed by fixtures. A broad
// symlink to the native host home defeats isolation." RelPath must name a
// specific sub-path inside the native home — never the native home root
// itself (ValidateAllowlist rejects any entry that would amount to that).
type AllowlistedShare struct {
	Host     string
	Category string
	// RelPath is the path, relative to the native home, this entry permits
	// symlinking. Must be a single non-empty path component or nested path
	// that stays strictly inside the native home (no "", ".", "/", or ".."
	// segments).
	RelPath string
	Class   domain.MutableStateClass
}

// allowlist is the closed, explicit set of narrow shares this project
// currently sanctions. This is the ONLY place a new share can be added —
// PlanAllowlistedSymlinks draws exclusively from this literal slice, so no
// caller can pass in an arbitrary path to symlink instead, and the
// allowlist can never be broadened by anything other than a reviewed change
// to this file. Both entries mirror mutablestate.go's own "cache" rows: a
// low-risk, recreatable, non-sensitive class this project is willing to
// back with a real fixture (see TestPlanAllowlistedSymlinks_CreatesWorkingSymlink).
//
// Deliberately unexported (Copilot review finding): an exported, mutable
// package-level slice could be reassigned or appended to at runtime by any
// other package in the module — including adding a narrow-looking but
// sensitive entry like "auth.json" — which would silently defeat the
// "closed allowlist" guarantee this whole package exists to provide, since
// init()'s validation below only ever runs once, at package load, long
// before any such runtime mutation. Allowlist() (below) is the read-only,
// defensive-copy accessor for anything outside this package that needs to
// inspect the current set.
var allowlist = []AllowlistedShare{
	{Host: "codex", Category: "cache", RelPath: "cache", Class: domain.MutableStateWorktreeShared},
	{Host: "claude-code", Category: "cache", RelPath: "cache", Class: domain.MutableStateWorktreeShared},
}

// Allowlist returns a defensive copy of the current closed allowlist —
// read-only by construction, since mutating the returned slice can never
// affect the package-internal allowlist var PlanAllowlistedSymlinks
// actually draws from.
func Allowlist() []AllowlistedShare {
	return append([]AllowlistedShare(nil), allowlist...)
}

// ValidateAllowlist rejects any entry that would amount to a broad symlink
// of the entire native home (empty, ".", "/", or a ".."-escaping RelPath),
// or that names a Class not eligible to appear in a sharing allowlist at
// all (domain.MutableStateClass.SharesAcrossGenerations must be true — a
// generation-local, host-global-external, or prohibited-import entry has no
// business being symlinked into another generation in the first place).
// Called both by init() below (so a broken entry in the literal Allowlist
// above fails at package-init time, not silently at first use) and directly
// by tests exercising a hypothetical bad entry.
func ValidateAllowlist(entries []AllowlistedShare) error {
	for i, e := range entries {
		if err := domain.ValidateHostID(e.Host); err != nil {
			return fmt.Errorf("auth: ValidateAllowlist[%d]: %w", i, err)
		}
		if err := rejectsBroadRelPath(e.RelPath); err != nil {
			return fmt.Errorf("auth: ValidateAllowlist[%d] (host=%s category=%s): %w", i, e.Host, e.Category, err)
		}
		if !e.Class.SharesAcrossGenerations() {
			return fmt.Errorf("auth: ValidateAllowlist[%d] (host=%s category=%s): class %q is not a sharing class (must be worktree-shared or identity-shared)", i, e.Host, e.Category, e.Class)
		}
	}
	return nil
}

// rejectsBroadRelPath is the hard structural guard against exactly the
// failure mode docs/architecture/runtime.md §9 warns about: "a broad
// symlink to the native host home defeats isolation." It is deliberately
// the single choke point both ValidateAllowlist and PlanAllowlistedSymlinks
// call — there is no second, divergent copy of this check anywhere in this
// package.
func rejectsBroadRelPath(relPath string) error {
	if relPath == "" {
		return fmt.Errorf("relPath is empty -- would symlink the entire native home root")
	}
	clean := filepath.Clean(relPath)
	if clean == "." || clean == "/" || filepath.IsAbs(clean) {
		return fmt.Errorf("relPath %q resolves to %q -- would symlink the entire native home root or an absolute path outside it", relPath, clean)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("relPath %q escapes the native home via '..'", relPath)
	}
	return nil
}

func init() {
	if err := ValidateAllowlist(allowlist); err != nil {
		panic("auth: the production allowlist itself is invalid: " + err.Error())
	}
}

// SymlinkPlan is one native-home-to-generation symlink PlanAllowlistedSymlinks
// computed. Applying it (via CreateAllowlistedSymlinks) creates a symlink at
// Link pointing at Target.
type SymlinkPlan struct {
	Category string
	Target   string // the real, native path being linked to
	Link     string // the path inside the generation this creates
	Class    domain.MutableStateClass
}

// PlanAllowlistedSymlinks returns the symlink operations for every
// allowlist entry matching host, rooted at nativeHome (the real native home
// directory, e.g. a resolved CODEX_HOME) and generationHome (the isolated
// generation directory these symlinks should land inside). This is the ONLY
// function in this package that plans a native-home symlink, and it draws
// exclusively from the closed, unexported allowlist var — there is no
// parameter here through which a caller can supply an arbitrary extra path
// to link, and no exported mutable variable through which another package
// could broaden what it draws from.
func PlanAllowlistedSymlinks(host, nativeHome, generationHome string) ([]SymlinkPlan, error) {
	if err := domain.ValidateHostID(host); err != nil {
		return nil, fmt.Errorf("auth: PlanAllowlistedSymlinks: %w", err)
	}
	if nativeHome == "" || generationHome == "" {
		return nil, fmt.Errorf("auth: PlanAllowlistedSymlinks: nativeHome and generationHome are both required")
	}
	var plans []SymlinkPlan
	for _, e := range allowlist {
		if e.Host != host {
			continue
		}
		if err := rejectsBroadRelPath(e.RelPath); err != nil {
			// Defense in depth: init() already validated the unexported,
			// package-internal allowlist var at load time, and nothing
			// outside this package can reach or mutate it (Allowlist()
			// above only ever returns a defensive copy) -- this branch
			// should be unreachable in practice. It stays as a second,
			// independent check rather than being removed, so a future
			// in-package edit to the literal allowlist that somehow
			// bypasses init() still fails loud instead of silently
			// planning a broad symlink.
			return nil, fmt.Errorf("auth: PlanAllowlistedSymlinks: allowlist entry (host=%s category=%s) is broad: %w", e.Host, e.Category, err)
		}
		plans = append(plans, SymlinkPlan{
			Category: e.Category,
			Target:   filepath.Join(nativeHome, e.RelPath),
			Link:     filepath.Join(generationHome, e.RelPath),
			Class:    e.Class,
		})
	}
	return plans, nil
}

// CreateAllowlistedSymlinks applies every plan from PlanAllowlistedSymlinks:
// for each, it ensures Link's parent directory exists and creates a symlink
// at Link pointing at Target (skipping a plan whose Target does not exist
// on disk — a generation-local Codex sandbox with no populated cache/ yet
// is not an error). It never symlinks anything outside what
// PlanAllowlistedSymlinks itself returned.
func CreateAllowlistedSymlinks(host, nativeHome, generationHome string) ([]SymlinkPlan, error) {
	plans, err := PlanAllowlistedSymlinks(host, nativeHome, generationHome)
	if err != nil {
		return nil, err
	}
	applied := make([]SymlinkPlan, 0, len(plans))
	for _, p := range plans {
		if _, statErr := os.Lstat(p.Target); statErr != nil {
			if os.IsNotExist(statErr) {
				continue // nothing native to share yet; not an error.
			}
			// A real failure (permission denied, a transient IO error, ...)
			// is not the same thing as "nothing to share yet" and must not
			// be swallowed the same way -- doing so would silently produce
			// an incomplete symlink set with no trace of why a plan was
			// skipped.
			return applied, fmt.Errorf("auth: CreateAllowlistedSymlinks: stat %q: %w", p.Target, statErr)
		}
		if err := os.MkdirAll(filepath.Dir(p.Link), 0o755); err != nil {
			return applied, fmt.Errorf("auth: CreateAllowlistedSymlinks: mkdir parent of %q: %w", p.Link, err)
		}
		if err := os.Symlink(p.Target, p.Link); err != nil {
			return applied, fmt.Errorf("auth: CreateAllowlistedSymlinks: symlink %q -> %q: %w", p.Link, p.Target, err)
		}
		applied = append(applied, p)
	}
	return applied, nil
}
