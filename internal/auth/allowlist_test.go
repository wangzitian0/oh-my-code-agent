package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestProductionAllowlistIsValid is a golden self-check: the real,
// production Allowlist var must always pass its own validation (already
// enforced by this package's init(), which panics otherwise — this test
// exists so a broken Allowlist entry fails a normal `go test` assertion
// too, with a clean failure message, in addition to a package-init panic).
func TestProductionAllowlistIsValid(t *testing.T) {
	if err := ValidateAllowlist(allowlist); err != nil {
		t.Fatalf("the production allowlist is invalid: %v", err)
	}
	if len(allowlist) == 0 {
		t.Fatal("allowlist is empty -- issue #27's AC requires at least one concrete, fixture-backed shared class")
	}
}

// TestAllowlist_ReturnsDefensiveCopy proves the exported Allowlist()
// accessor cannot be used to mutate the package-internal allowlist var:
// mutating the returned slice must never be visible through a second call.
func TestAllowlist_ReturnsDefensiveCopy(t *testing.T) {
	got := Allowlist()
	if len(got) == 0 {
		t.Fatal("Allowlist() returned no entries")
	}
	got[0].RelPath = "tampered"
	if allowlist[0].RelPath == "tampered" {
		t.Fatal("mutating Allowlist()'s returned slice mutated the package-internal allowlist -- not a defensive copy")
	}
}

// TestValidateAllowlist_RejectsBroadEntry is the direct proof of issue #27's
// AC: "a broad symlink to the native home is rejected by test." Every
// RelPath shape here would, if permitted, amount to symlinking the entire
// native home (or escaping it) rather than one narrow sub-path.
func TestValidateAllowlist_RejectsBroadEntry(t *testing.T) {
	broadRelPaths := []string{"", ".", "/", "..", "../elsewhere", "cache/../.."}
	for _, relPath := range broadRelPaths {
		t.Run("relpath="+relPath, func(t *testing.T) {
			entries := []AllowlistedShare{
				{Host: "codex", Category: "test-broad", RelPath: relPath, Class: domain.MutableStateWorktreeShared},
			}
			if err := ValidateAllowlist(entries); err == nil {
				t.Errorf("ValidateAllowlist(RelPath=%q) = nil, want a rejection error", relPath)
			}
		})
	}
}

// TestValidateAllowlist_RejectsNonSharingClass proves an allowlist entry
// naming a non-sharing MutableStateClass (generation-local, host-global
// external, prohibited import) is refused: an allowlist entry only makes
// sense for a class that is actually meant to cross generation boundaries.
func TestValidateAllowlist_RejectsNonSharingClass(t *testing.T) {
	nonSharing := []domain.MutableStateClass{
		domain.MutableStateGenerationLocal, domain.MutableStateHostGlobalExternal, domain.MutableStateProhibitedImport,
	}
	for _, class := range nonSharing {
		entries := []AllowlistedShare{{Host: "codex", Category: "test", RelPath: "cache", Class: class}}
		if err := ValidateAllowlist(entries); err == nil {
			t.Errorf("ValidateAllowlist(Class=%q) = nil, want a rejection error", class)
		}
	}
}

// TestPlanAllowlistedSymlinks_RejectsBroadNativeHomeLink proves the planning
// function itself (not only ValidateAllowlist) refuses a broad share — the
// second, independent layer of the "structurally impossible to broaden"
// guarantee doc.go describes. This simulates a corrupted/tampered Allowlist
// by calling the same internal guard PlanAllowlistedSymlinks uses directly,
// proving rejectsBroadRelPath itself (the single choke point both
// ValidateAllowlist and PlanAllowlistedSymlinks call) actually rejects every
// broad shape rather than just documenting that it should.
func TestPlanAllowlistedSymlinks_RejectsBroadNativeHomeLink(t *testing.T) {
	broad := []string{"", ".", "/", "..", filepath.Join("cache", "..", "..")}
	for _, relPath := range broad {
		if err := rejectsBroadRelPath(relPath); err == nil {
			t.Errorf("rejectsBroadRelPath(%q) = nil, want a rejection error", relPath)
		}
	}
	// And a legitimate narrow path must NOT be rejected.
	if err := rejectsBroadRelPath("cache"); err != nil {
		t.Errorf("rejectsBroadRelPath(\"cache\") = %v, want nil", err)
	}
	if err := rejectsBroadRelPath(filepath.Join("cache", "models")); err != nil {
		t.Errorf("rejectsBroadRelPath(cache/models) = %v, want nil", err)
	}
}

// TestPlanAllowlistedSymlinks_OnlyReturnsAllowlistedEntries proves
// PlanAllowlistedSymlinks never invents an entry beyond the production
// Allowlist — every returned SymlinkPlan's RelPath-equivalent (Target minus
// nativeHome) must correspond to some real Allowlist entry for that host.
func TestPlanAllowlistedSymlinks_OnlyReturnsAllowlistedEntries(t *testing.T) {
	nativeHome := "/native/codex-home"
	generationHome := "/generation/codex-home"
	plans, err := PlanAllowlistedSymlinks("codex", nativeHome, generationHome)
	if err != nil {
		t.Fatalf("PlanAllowlistedSymlinks: %v", err)
	}
	if len(plans) == 0 {
		t.Fatal("plans is empty, want at least the cache allowlist entry")
	}
	allowedRelPaths := map[string]bool{}
	for _, e := range allowlist {
		if e.Host == "codex" {
			allowedRelPaths[e.RelPath] = true
		}
	}
	for _, p := range plans {
		rel, err := filepath.Rel(nativeHome, p.Target)
		if err != nil {
			t.Fatalf("filepath.Rel: %v", err)
		}
		if !allowedRelPaths[rel] {
			t.Errorf("plan Target %q (rel %q) does not correspond to any codex Allowlist entry", p.Target, rel)
		}
		if !p.Class.SharesAcrossGenerations() {
			t.Errorf("plan Class %q is not a sharing class", p.Class)
		}
	}
}

func TestPlanAllowlistedSymlinks_RejectsUnknownHost(t *testing.T) {
	if _, err := PlanAllowlistedSymlinks("not-a-real-host", "/a", "/b"); err == nil {
		t.Error("PlanAllowlistedSymlinks(unknown host) error = nil, want error")
	}
}

func TestPlanAllowlistedSymlinks_RequiresBothHomes(t *testing.T) {
	if _, err := PlanAllowlistedSymlinks("codex", "", "/b"); err == nil {
		t.Error("PlanAllowlistedSymlinks(nativeHome=\"\") error = nil, want error")
	}
	if _, err := PlanAllowlistedSymlinks("codex", "/a", ""); err == nil {
		t.Error("PlanAllowlistedSymlinks(generationHome=\"\") error = nil, want error")
	}
}

// TestCreateAllowlistedSymlinks_CreatesWorkingSymlink is the fixture-backed
// functional proof issue #27's AC asks for: a real narrow symlink is
// created, and reading through it from the generation side reaches the
// exact native content -- not merely that the mechanism is "allowed" on
// paper.
func TestCreateAllowlistedSymlinks_CreatesWorkingSymlink(t *testing.T) {
	nativeHome := t.TempDir()
	generationHome := t.TempDir()

	cacheDir := filepath.Join(nativeHome, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(cacheDir, "models_cache.json")
	if err := os.WriteFile(planted, []byte(`{"planted":"cache content"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	applied, err := CreateAllowlistedSymlinks("codex", nativeHome, generationHome)
	if err != nil {
		t.Fatalf("CreateAllowlistedSymlinks: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied = %d plans, want 1 (only the cache/ allowlist entry has native content)", len(applied))
	}

	linkPath := filepath.Join(generationHome, "cache")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("Lstat(%q): %v", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%q is not a symlink", linkPath)
	}

	gotContent, err := os.ReadFile(filepath.Join(linkPath, "models_cache.json"))
	if err != nil {
		t.Fatalf("reading through the generation-side symlink: %v", err)
	}
	if string(gotContent) != `{"planted":"cache content"}` {
		t.Errorf("content read through symlink = %q, want the planted native content", gotContent)
	}
}

// TestCreateAllowlistedSymlinks_NoNativeContent_SkipsWithoutError proves an
// allowlisted-but-empty native path (a fresh install with no cache/ yet) is
// silently skipped, not an error.
func TestCreateAllowlistedSymlinks_NoNativeContent_SkipsWithoutError(t *testing.T) {
	nativeHome := t.TempDir() // no cache/ subdirectory created
	generationHome := t.TempDir()

	applied, err := CreateAllowlistedSymlinks("codex", nativeHome, generationHome)
	if err != nil {
		t.Fatalf("CreateAllowlistedSymlinks: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("applied = %d plans, want 0", len(applied))
	}
	if _, err := os.Lstat(filepath.Join(generationHome, "cache")); err == nil {
		t.Error("a symlink was created despite no native content existing")
	}
}

// TestCreateAllowlistedSymlinks_RealLstatError_IsNotSwallowed proves the
// Copilot-review fix: a real (non-IsNotExist) Lstat failure on a plan's
// Target -- here, a permission error, simulated by making nativeHome itself
// unsearchable -- must surface as an error, not be silently treated the
// same as "nothing native to share yet." Swallowing it would silently
// produce an incomplete symlink set with no trace of why a plan was
// skipped.
func TestCreateAllowlistedSymlinks_RealLstatError_IsNotSwallowed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks are bypassed when running as root")
	}
	nativeHome := t.TempDir()
	generationHome := t.TempDir()
	if err := os.Chmod(nativeHome, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(nativeHome, 0o755) })

	_, err := CreateAllowlistedSymlinks("codex", nativeHome, generationHome)
	if err == nil {
		t.Fatal("CreateAllowlistedSymlinks: want an error for a real (non-IsNotExist) Lstat failure, got nil")
	}
}

// TestCreateAllowlistedSymlinks_NeverLinksOutsideAllowlist plants a
// sensitive file directly at the native home ROOT (standing in for
// auth.json/.claude.json) and proves CreateAllowlistedSymlinks never
// symlinks it: only the cache/ allowlist entry ever gets linked, and the
// root-level file remains unreferenced from generationHome by any means
// this function creates.
func TestCreateAllowlistedSymlinks_NeverLinksOutsideAllowlist(t *testing.T) {
	nativeHome := t.TempDir()
	generationHome := t.TempDir()

	sensitive := filepath.Join(nativeHome, "auth.json")
	if err := os.WriteFile(sensitive, []byte(`{"token":"should-never-be-linked"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(nativeHome, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "x.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := CreateAllowlistedSymlinks("codex", nativeHome, generationHome); err != nil {
		t.Fatalf("CreateAllowlistedSymlinks: %v", err)
	}

	entries, err := os.ReadDir(generationHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "cache" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("generationHome contains %v, want exactly [\"cache\"]", names)
	}
	if _, err := os.Lstat(filepath.Join(generationHome, "auth.json")); err == nil {
		t.Fatal("auth.json was linked into the generation home -- broad/unlisted native content leaked through")
	}
}
