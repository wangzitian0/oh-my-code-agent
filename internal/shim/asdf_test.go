package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeASDFShim writes a synthetic asdf shim script at
// <asdfDataDir>/shims/<name>, containing one "# asdf-plugin: <plugin>
// <version>" comment line per entry in pluginVersions -- exactly the
// metadata format a real `asdf reshim` writes (see asdf.go's doc comment;
// this repo's own agent verified the format read-only against a real,
// installed asdf 0.18.0 during issue #69's investigation, but this fixture
// never touches that real installation -- it is built fresh under
// t.TempDir() for every test). The body execs nothing real; ResolveASDFShimTarget
// never runs shim scripts, only reads them as plain text, so the body
// content does not matter for this package's own tests (cmd/omca's
// end-to-end tests separately prove the resolved *target* binary, not this
// shim script, is what actually gets exec'd).
func writeASDFShim(t *testing.T, asdfDataDir, name string, pluginVersions [][2]string) string {
	t.Helper()
	shimsDir := filepath.Join(asdfDataDir, "shims")
	if err := os.MkdirAll(shimsDir, 0o755); err != nil {
		t.Fatalf("writeASDFShim: MkdirAll: %v", err)
	}
	body := "#!/usr/bin/env bash\n"
	for _, pv := range pluginVersions {
		body += "# asdf-plugin: " + pv[0] + " " + pv[1] + "\n"
	}
	body += "exec asdf exec \"" + name + "\" \"$@\"\n"
	path := filepath.Join(shimsDir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writeASDFShim: WriteFile: %v", err)
	}
	return path
}

// writeASDFInstalledBinary writes a fake, executable real binary at
// <asdfDataDir>/installs/<plugin>/<version>/bin/<name> -- the path
// ResolveASDFShimTarget must construct and confirm executable for a
// single-plugin-line shim to resolve successfully.
func writeASDFInstalledBinary(t *testing.T, asdfDataDir, plugin, version, name string) string {
	t.Helper()
	binDir := filepath.Join(asdfDataDir, "installs", plugin, version, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("writeASDFInstalledBinary: MkdirAll: %v", err)
	}
	return writeFakeExecutable(t, binDir, name)
}

// TestIsASDFShim_DefaultLayout proves the "<home>/.asdf/shims/<name>"
// layout issue #69's own reproduction hit is recognized.
func TestIsASDFShim_DefaultLayout(t *testing.T) {
	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	path := writeASDFShim(t, asdfDataDir, "codex", [][2]string{{"nodejs", "20.19.0"}})
	if !IsASDFShim(path) {
		t.Errorf("IsASDFShim(%q) = false, want true", path)
	}
}

// TestIsASDFShim_OrdinaryBinary_NotAShim proves a plain binary living
// somewhere unrelated to any ".asdf/shims" layout is never misidentified.
func TestIsASDFShim_OrdinaryBinary_NotAShim(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeExecutable(t, dir, "codex")
	if IsASDFShim(path) {
		t.Errorf("IsASDFShim(%q) = true, want false (not under a .asdf/shims layout)", path)
	}
}

// TestIsASDFShim_SimilarButDifferentDirNames proves the check requires both
// "shims" and ".asdf" specifically -- a merely similarly-shaped directory
// tree (e.g. some other tool's own "<x>/shims/<name>" convention) is not
// treated as an asdf shim.
func TestIsASDFShim_SimilarButDifferentDirNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-asdf", "shims")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeFakeExecutable(t, dir, "codex")
	if IsASDFShim(path) {
		t.Errorf("IsASDFShim(%q) = true, want false (grandparent is not named .asdf)", path)
	}
}

// TestIsASDFShim_EmptyPath proves the empty-string edge case is a clean
// false, not a panic (filepath.Dir("") == "." would otherwise need careful
// handling).
func TestIsASDFShim_EmptyPath(t *testing.T) {
	if IsASDFShim("") {
		t.Error("IsASDFShim(\"\") = true, want false")
	}
}

// TestIsASDFShim_CustomASDFDataDir_DetectedViaMetadataFallback is a
// regression test (Copilot review finding on this PR): a shim under a
// non-default ASDF_DATA_DIR (asdf's own env var override, e.g.
// "<home>/asdf-data/shims/<name>" instead of "<home>/.asdf/shims/<name>")
// must still be recognized -- via the file's own "# asdf-plugin:" metadata
// comment, since the directory-name heuristic alone cannot see it -- or
// isolated mode silently falls back to exec'ing the shim directly and hits
// the original exit-126 failure this whole fix exists to prevent.
func TestIsASDFShim_CustomASDFDataDir_DetectedViaMetadataFallback(t *testing.T) {
	customDataDir := filepath.Join(t.TempDir(), "asdf-data") // deliberately not ".asdf"
	path := writeASDFShim(t, customDataDir, "codex", [][2]string{{"nodejs", "20.19.0"}})
	if !IsASDFShim(path) {
		t.Errorf("IsASDFShim(%q) = false, want true (custom ASDF_DATA_DIR, detected via metadata comment fallback)", path)
	}
}

// TestResolveASDFShimTarget_SingleVersion_Resolves is this fix's core
// success-path proof: a shim naming exactly one plugin version (issue #69's
// own reproduction shape) resolves to the concrete installed binary,
// without ever running the shim script or any real asdf binary.
func TestResolveASDFShimTarget_SingleVersion_Resolves(t *testing.T) {
	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	shimPath := writeASDFShim(t, asdfDataDir, "codex", [][2]string{{"nodejs", "20.19.0"}})
	want := writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "20.19.0", "codex")

	got, err := ResolveASDFShimTarget(shimPath)
	if err != nil {
		t.Fatalf("ResolveASDFShimTarget: %v", err)
	}
	if got != want {
		t.Errorf("ResolveASDFShimTarget(%q) = %q, want %q", shimPath, got, want)
	}
}

// TestResolveASDFShimTarget_AmbiguousVersions_Errors proves a shim naming
// two or more plugin versions is refused rather than guessed at -- asdf's
// own .tool-versions precedence would be needed to disambiguate, and this
// project deliberately does not replicate it (asdf.go's doc comment).
func TestResolveASDFShimTarget_AmbiguousVersions_Errors(t *testing.T) {
	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	shimPath := writeASDFShim(t, asdfDataDir, "codex", [][2]string{
		{"nodejs", "20.19.0"},
		{"nodejs", "18.20.0"},
	})
	writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "20.19.0", "codex")
	writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "18.20.0", "codex")

	if _, err := ResolveASDFShimTarget(shimPath); err == nil {
		t.Fatal("ResolveASDFShimTarget with two plugin-version lines: want error, got nil")
	}
}

// TestResolveASDFShimTarget_DuplicateIdenticalPluginLine_NotAmbiguous is a
// regression test (Copilot review finding on this PR): asdf has been
// observed to write the same "# asdf-plugin: <plugin> <version>" line more
// than once into a single shim script for the same (plugin, version) pair.
// Counting raw matching lines (rather than distinct pairs) would
// incorrectly refuse this as "2 different plugin versions" even though
// there is exactly one real candidate and nothing to disambiguate.
func TestResolveASDFShimTarget_DuplicateIdenticalPluginLine_NotAmbiguous(t *testing.T) {
	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	shimPath := writeASDFShim(t, asdfDataDir, "codex", [][2]string{
		{"nodejs", "20.19.0"},
		{"nodejs", "20.19.0"},
	})
	want := writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "20.19.0", "codex")

	got, err := ResolveASDFShimTarget(shimPath)
	if err != nil {
		t.Fatalf("ResolveASDFShimTarget with a duplicated identical plugin-version line: want success, got error: %v", err)
	}
	if got != want {
		t.Errorf("ResolveASDFShimTarget(%q) = %q, want %q", shimPath, got, want)
	}
}

// TestResolveASDFShimTarget_NoMetadataLine_Errors proves a file that merely
// lives in the right location but carries none of asdf's own shim metadata
// is refused, not guessed at.
func TestResolveASDFShimTarget_NoMetadataLine_Errors(t *testing.T) {
	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	shimsDir := filepath.Join(asdfDataDir, "shims")
	if err := os.MkdirAll(shimsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(shimsDir, "codex")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\necho not an asdf shim\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveASDFShimTarget(path); err == nil {
		t.Fatal("ResolveASDFShimTarget with no asdf-plugin metadata line: want error, got nil")
	}
}

// TestResolveASDFShimTarget_ResolvedTargetMissing_Errors proves that a
// single, unambiguous plugin-version line whose corresponding installed
// binary does not actually exist on disk (e.g. it was uninstalled after the
// shim was generated, or the plugin's install layout does not match the
// installs/<plugin>/<version>/bin/<name> convention this function assumes)
// is a clear error, never a path a later exec would just fail against.
func TestResolveASDFShimTarget_ResolvedTargetMissing_Errors(t *testing.T) {
	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	shimPath := writeASDFShim(t, asdfDataDir, "codex", [][2]string{{"nodejs", "20.19.0"}})
	// Deliberately never create installs/nodejs/20.19.0/bin/codex.

	if _, err := ResolveASDFShimTarget(shimPath); err == nil {
		t.Fatal("ResolveASDFShimTarget with a missing resolved target: want error, got nil")
	}
}

// TestResolveASDFShimTarget_UnreadableShim_Errors proves a nonexistent
// shimPath itself (as opposed to a resolved target that is missing) is
// reported as a clean error too.
func TestResolveASDFShimTarget_UnreadableShim_Errors(t *testing.T) {
	if _, err := ResolveASDFShimTarget(filepath.Join(t.TempDir(), ".asdf", "shims", "does-not-exist")); err == nil {
		t.Fatal("ResolveASDFShimTarget on a nonexistent shim path: want error, got nil")
	}
}

// writeShebangScript writes an executable "#!/usr/bin/env <interp>" script,
// the shape an npm-installed Node CLI has.
func writeShebangScript(t *testing.T, dir, name, interp string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeShebangScript: MkdirAll: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/usr/bin/env "+interp+"\n"), 0o755); err != nil {
		t.Fatalf("writeShebangScript: %v", err)
	}
	return p
}

// TestResolveShebangInterpreter_AmbiguousASDFInterpreter_UsesSiblingInstall
// is the machine state that silently broke `codex` in every managed shell.
//
// Two installed node versions means their shared asdf `node` shim names both
// plugin versions, and ResolveASDFShimTarget correctly refuses to choose --
// picking one would mean replicating asdf's .tool-versions precedence. The
// old code then kept the unresolved shim and exec'd it anyway, which under
// the virtualized HOME isolated mode always uses dies as a bare exit 126
// with no output at all.
//
// The ambiguity is avoidable rather than merely reportable: the host binary
// was resolved to <dataDir>/installs/nodejs/20.19.0/bin/codex, so the node
// that belongs to it is the one beside it. No guess, no .tool-versions.
func TestResolveShebangInterpreter_AmbiguousASDFInterpreter_UsesSiblingInstall(t *testing.T) {
	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	// Both versions installed; the shared node shim therefore names both.
	writeASDFShim(t, asdfDataDir, "node", [][2]string{{"nodejs", "22.19.0"}, {"nodejs", "20.19.0"}})
	writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "22.19.0", "node")
	wantNode := writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "20.19.0", "node")

	// The host binary as asdf installed it: a Node entry point under 20.19.0.
	hostBin := writeShebangScript(t, filepath.Join(asdfDataDir, "installs", "nodejs", "20.19.0", "bin"), "codex", "node")

	got, err := ResolveShebangInterpreter(hostBin, filepath.Join(asdfDataDir, "shims"), "")
	if err != nil {
		t.Fatalf("ResolveShebangInterpreter: %v", err)
	}
	if got != wantNode {
		t.Errorf("ResolveShebangInterpreter = %q, want the node beside the host binary (%q); anything else either guesses a version or falls back to the asdf shim that dies as a silent exit 126", got, wantNode)
	}
}

// TestResolveShebangInterpreter_UnresolvableASDFInterpreter_FailsClosed
// covers the case with no sibling to fall back on: the error must surface
// here, where it can still be explained, rather than being exec'd into a
// bare 126 that nothing downstream can attribute.
func TestResolveShebangInterpreter_UnresolvableASDFInterpreter_FailsClosed(t *testing.T) {
	asdfDataDir := filepath.Join(t.TempDir(), ".asdf")
	writeASDFShim(t, asdfDataDir, "node", [][2]string{{"nodejs", "22.19.0"}, {"nodejs", "20.19.0"}})
	writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "22.19.0", "node")
	writeASDFInstalledBinary(t, asdfDataDir, "nodejs", "20.19.0", "node")

	// Host binary lives outside any asdf install, so there is no sibling node.
	hostBin := writeShebangScript(t, t.TempDir(), "codex", "node")

	_, err := ResolveShebangInterpreter(hostBin, filepath.Join(asdfDataDir, "shims"), "")
	if err == nil {
		t.Fatal("want an error for an interpreter that resolves to an unresolvable asdf shim; returning it silently makes the caller exec a path known to die as exit 126 with no output")
	}
	if !strings.Contains(err.Error(), "126") {
		t.Errorf("the error should name the failure mode the caller would otherwise hit: %v", err)
	}
}

// TestResolveShebangInterpreter_SoftCases must stay soft: neither is a
// reason to fail a launch that would otherwise work.
func TestResolveShebangInterpreter_SoftCases(t *testing.T) {
	plain := writeFakeExecutable(t, t.TempDir(), "codex") // not a shebang script
	got, err := ResolveShebangInterpreter(plain, t.TempDir(), "")
	if err != nil || got != "" {
		t.Errorf("a non-shebang binary = (%q, %v), want (\"\", nil)", got, err)
	}

	orphan := writeShebangScript(t, t.TempDir(), "codex", "no-such-interpreter-anywhere")
	got, err = ResolveShebangInterpreter(orphan, t.TempDir(), "")
	if err != nil || got != "" {
		t.Errorf("an interpreter absent from PATH = (%q, %v), want (\"\", nil) so the caller falls back to exec'ing the script directly", got, err)
	}
}
