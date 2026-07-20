package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMutableNativeHomeDir_PathConstruction(t *testing.T) {
	got, err := MutableNativeHomeDir("/state/worktree", "codex", "cli")
	if err != nil {
		t.Fatalf("MutableNativeHomeDir: %v", err)
	}
	want := filepath.Join("/state/worktree", "state", "hosts", "codex", "cli", "codex-home")
	if got != want {
		t.Errorf("MutableNativeHomeDir = %q, want %q", got, want)
	}
}

func TestMutableNativeHomeDir_UnsupportedHost(t *testing.T) {
	if _, err := MutableNativeHomeDir("/state/worktree", "not-a-host", "cli"); err == nil {
		t.Fatal("MutableNativeHomeDir: want error for unsupported host, got nil")
	}
}

// TestSyncMutableNativeHome_CopiesGeneratedConfig_PreservesHostState is this
// fix's core regression proof: SyncMutableNativeHome must refresh the
// OMCA-authored config files a generation compiled, without touching (or
// deleting) host-written state -- e.g. Codex's own state_5.sqlite -- that a
// previous launch already left in mutableDir. Losing that state on every
// relaunch would defeat the entire point of scoping mutableDir to the
// worktree rather than the generation (MutableNativeHomeDir's doc comment).
func TestSyncMutableNativeHome_CopiesGeneratedConfig_PreservesHostState(t *testing.T) {
	genDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(genDir, "config.toml"), []byte("generation-v1"), 0o444); err != nil {
		t.Fatal(err)
	}

	mutableDir := t.TempDir()
	// Simulate a previous launch: the host already wrote its own state file
	// here, and an older synced copy of config.toml is already present.
	hostState := filepath.Join(mutableDir, "state_5.sqlite")
	if err := os.WriteFile(hostState, []byte("real sqlite bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mutableDir, "config.toml"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SyncMutableNativeHome(genDir, mutableDir); err != nil {
		t.Fatalf("SyncMutableNativeHome: %v", err)
	}

	gotConfig, err := os.ReadFile(filepath.Join(mutableDir, "config.toml"))
	if err != nil {
		t.Fatalf("reading synced config.toml: %v", err)
	}
	if string(gotConfig) != "generation-v1" {
		t.Errorf("synced config.toml = %q, want %q (a new generation's config must overwrite a stale copy)", gotConfig, "generation-v1")
	}

	gotState, err := os.ReadFile(hostState)
	if err != nil {
		t.Fatalf("host state file was removed by Sync: %v", err)
	}
	if string(gotState) != "real sqlite bytes" {
		t.Errorf("host state file content changed = %q, want untouched %q", gotState, "real sqlite bytes")
	}
}

// TestSyncMutableNativeHome_CreatesMutableDirWritable proves the directory
// Sync creates is actually writable, even though the source
// generationNativeHomeDir it reads from is the read-only tree
// readonly.go produces -- the literal bug this whole file exists to fix
// (a host launched with its native-home variable pointing at a 0o555
// directory fails the moment it tries to create its own state file there).
func TestSyncMutableNativeHome_CreatesMutableDirWritable(t *testing.T) {
	genDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(genDir, "config.toml"), []byte("v1"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(genDir, readOnlyDirMode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(genDir, 0o755) })

	mutableDir := filepath.Join(t.TempDir(), "codex-home")
	if err := SyncMutableNativeHome(genDir, mutableDir); err != nil {
		t.Fatalf("SyncMutableNativeHome: %v", err)
	}

	if err := os.WriteFile(filepath.Join(mutableDir, "state_5.sqlite"), []byte("x"), 0o644); err != nil {
		t.Fatalf("mutableDir is not writable after Sync: %v", err)
	}
}

func TestSyncMutableNativeHome_MissingSourceDir_Errors(t *testing.T) {
	mutableDir := t.TempDir()
	if err := SyncMutableNativeHome(filepath.Join(t.TempDir(), "does-not-exist"), mutableDir); err == nil {
		t.Fatal("SyncMutableNativeHome: want error for missing source dir, got nil")
	}
}
