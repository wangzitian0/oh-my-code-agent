package runtime

import (
	"fmt"
	"os"
	"path/filepath"
)

// MutableNativeHomeDir returns the writable, worktree-scoped directory a
// launch shim (internal/shim/plan.go's Build) or `omca run --mode isolated`
// (cmd/omca/run.go's runIsolated) points a host's native-home environment
// variable at (CODEX_HOME/CLAUDE_CONFIG_DIR) -- deliberately NOT the
// generation's own <generationDir>/hosts/<host>/<surface>/<homeDirName>
// directory NativeHomeDirName names, because that directory is made
// read-only by Bootstrap/Compile (readonly.go, issue #13 AC "Generated
// artifact trees are read-only on disk") while a host's own native-home
// variable is also where it stores mutable runtime state (docs/architecture/
// runtime.md §7.1: "Codex uses CODEX_HOME for config and state"; §9:
// "Config artifacts are immutable per generation, but hosts create mutable
// state ... in separately classified mutable paths"). A real launch through
// the read-only generation directory fails outright the moment the host
// tries to create its own state (e.g. Codex's CODEX_HOME/state_5.sqlite:
// "unable to open database file") -- this is the fix for that.
//
// Scoped under worktreeStateDir (OMCA_STATE_DIR), not the generation
// directory, so this state is "worktree-shared" per §9's classification: a
// host's session history, local databases, and trust decisions survive a
// generation recompile within the same worktree, exactly like a real
// $CODEX_HOME/$CLAUDE_CONFIG_DIR would survive an ordinary config edit --
// rather than "generation-local," which would silently wipe that state on
// every recompile.
func MutableNativeHomeDir(worktreeStateDir, host, surface string) (string, error) {
	homeDirName, err := NativeHomeDirName(host)
	if err != nil {
		return "", fmt.Errorf("runtime: MutableNativeHomeDir: %w", err)
	}
	return filepath.Join(worktreeStateDir, "state", "hosts", host, surface, homeDirName), nil
}

// SyncMutableNativeHome refreshes mutableDir (creating it if necessary) with
// the current generation's compiled config artifacts from
// generationNativeHomeDir -- the read-only
// <generationDir>/hosts/<host>/<surface>/<homeDirName> directory
// hostConfigFiles (compile.go) wrote config.toml/settings.json/.claude.json
// into -- so a launch always sees the config the CURRENT generation
// compiled, without ever touching host-written state (SQLite databases,
// logs, auth.json, session history) that may already live in mutableDir
// from a previous launch in this worktree.
//
// It only ever copies the flat set of regular files directly inside
// generationNativeHomeDir, never recursing into subdirectories: every
// OMCA-authored file hostConfigFiles produces today lands directly there
// with no subdirectories of its own (compile.go's hostConfigFiles /
// compileHostTree), and a non-recursive copy is what keeps this function
// from ever walking into -- and silently overwriting content inside -- a
// subdirectory a host itself creates in mutableDir for its own state (e.g.
// Codex's own `log/`).
func SyncMutableNativeHome(generationNativeHomeDir, mutableDir string) error {
	if err := os.MkdirAll(mutableDir, 0o755); err != nil {
		return fmt.Errorf("runtime: SyncMutableNativeHome: mkdir %s: %w", mutableDir, err)
	}
	entries, err := os.ReadDir(generationNativeHomeDir)
	if err != nil {
		return fmt.Errorf("runtime: SyncMutableNativeHome: read %s: %w", generationNativeHomeDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := filepath.Join(generationNativeHomeDir, entry.Name())
		content, readErr := os.ReadFile(src)
		if readErr != nil {
			return fmt.Errorf("runtime: SyncMutableNativeHome: read %s: %w", src, readErr)
		}
		dst := filepath.Join(mutableDir, entry.Name())
		if writeErr := os.WriteFile(dst, content, 0o644); writeErr != nil {
			return fmt.Errorf("runtime: SyncMutableNativeHome: write %s: %w", dst, writeErr)
		}
	}
	return nil
}
