package context

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFakeBinary writes a hermetic, harmless POSIX shell script standing in
// for a real host binary: it never runs the actual codex/claude, so this
// package's test suite never depends on either being installed (issue #11's
// "structure host-detection code so its unit tests don't depend on real
// binaries being present," the same discipline
// internal/qualify/invoke_test.go's TestRunInvocationRunsIsolatedFakeBinary
// established for PR-06).
func writeFakeBinary(t *testing.T, dir, name, versionOutput string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a POSIX shell script")
	}
	if strings.Contains(versionOutput, "'") {
		t.Fatalf("writeFakeBinary: versionOutput %q must not contain a single quote (kept the fake script's quoting trivial)", versionOutput)
	}
	path := filepath.Join(dir, name)
	// The subprocess this script runs in gets a minimal, synthetic PATH
	// (DetectHost's whole point is never to depend on ambient PATH
	// contents), so the script body must use only shell builtins — never an
	// external program like cat, which would fail to resolve and make the
	// fake binary itself the thing under test. printf and [ (test) are
	// POSIX shell builtins. The content is passed as printf's own argument
	// (not interpolated into the format string), so it is never
	// reinterpreted for % or backslash sequences. A caller-supplied
	// trailing newline (version strings the way a real host would print
	// them) is trimmed first since the %s\n format already supplies one.
	trimmed := strings.TrimSuffix(versionOutput, "\n")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"printf '%s\\n' '" + trimmed + "'\n" +
		"fi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDetectHost_UnknownHostID(t *testing.T) {
	env := Environment{Vars: []string{"HOME=" + t.TempDir()}}
	if _, err := DetectHost(context.Background(), env, "not-a-real-host"); err == nil {
		t.Fatal("DetectHost(not-a-real-host): want error, got nil")
	}
}

func TestDetectHost_KnownButUnimplementedHostID(t *testing.T) {
	// "opencode" is a canonical host ID (domain.KnownHostIDs) but this
	// package only implements detection for codex and claude-code — a
	// distinct failure mode from an unknown ID entirely.
	env := Environment{Vars: []string{"HOME=" + t.TempDir()}}
	_, err := DetectHost(context.Background(), env, "opencode")
	if err == nil {
		t.Fatal("DetectHost(opencode): want error, got nil")
	}
	if !strings.Contains(err.Error(), "does not implement detection") {
		t.Errorf("error = %q, want it to explain detection is unimplemented for this known host", err.Error())
	}
}

func TestDetectHost_MissingHome(t *testing.T) {
	env := Environment{Vars: []string{"PATH=/usr/bin"}}
	if _, err := DetectHost(context.Background(), env, "codex"); err == nil {
		t.Fatal("DetectHost with no HOME: want error, got nil")
	}
}

func TestDetectHost_HomeNotAbsolute(t *testing.T) {
	env := Environment{Vars: []string{"HOME=relative/path", "PATH=/usr/bin"}}
	_, err := DetectHost(context.Background(), env, "codex")
	if err == nil {
		t.Fatal("DetectHost with a non-absolute HOME: want error, got nil")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error = %q, want it to explain HOME must be absolute", err.Error())
	}
}

func TestDetectHost_NotInstalled(t *testing.T) {
	emptyBinDir := t.TempDir()
	env := Environment{Vars: []string{
		"HOME=" + t.TempDir(),
		"PATH=" + emptyBinDir,
	}}
	det, err := DetectHost(context.Background(), env, "codex")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}
	if det.Installed {
		t.Error("Installed = true, want false when the binary is not on PATH")
	}
	if det.Version != "" {
		t.Errorf("Version = %q, want empty when not installed", det.Version)
	}
	if det.Error != "" {
		t.Errorf("Error = %q, want empty (not-installed is not a probe error)", det.Error)
	}
	// Native homes are still reported even when the binary itself is absent
	// — they describe where OMCA would look, independent of installation.
	if len(det.NativeHomes) == 0 {
		t.Error("NativeHomes is empty, want native home locations reported regardless of Installed")
	}
}

func TestDetectHost_CodexInstalledDefaultHome(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "codex", "codex-cli 9.9.9\n")
	home := t.TempDir()
	env := Environment{Vars: []string{"HOME=" + home, "PATH=" + binDir}}

	det, err := DetectHost(context.Background(), env, "codex")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}
	if !det.Installed {
		t.Fatal("Installed = false, want true")
	}
	if det.Version != "9.9.9" {
		t.Errorf("Version = %q, want %q", det.Version, "9.9.9")
	}
	if det.BinaryPath != filepath.Join(binDir, "codex") {
		t.Errorf("BinaryPath = %q, want %q", det.BinaryPath, filepath.Join(binDir, "codex"))
	}
	if det.Surface != "cli" {
		t.Errorf("Surface = %q, want %q", det.Surface, "cli")
	}
	if det.Error != "" {
		t.Errorf("Error = %q, want empty", det.Error)
	}

	wantCodexHome := filepath.Join(home, ".codex")
	found := false
	for _, nh := range det.NativeHomes {
		if nh.Name == "CODEX_HOME" {
			found = true
			if nh.Path != wantCodexHome {
				t.Errorf("CODEX_HOME native home Path = %q, want %q", nh.Path, wantCodexHome)
			}
			if nh.FromEnvVar != "" {
				t.Errorf("CODEX_HOME native home FromEnvVar = %q, want empty (CODEX_HOME unset, default in use)", nh.FromEnvVar)
			}
		}
	}
	if !found {
		t.Error("no CODEX_HOME entry in NativeHomes")
	}
}

func TestDetectHost_CodexHomeOverride(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "codex", "codex-cli 0.144.5\n")
	home := t.TempDir()
	override := t.TempDir()
	env := Environment{Vars: []string{
		"HOME=" + home,
		"PATH=" + binDir,
		"CODEX_HOME=" + override,
	}}

	det, err := DetectHost(context.Background(), env, "codex")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}
	for _, nh := range det.NativeHomes {
		if nh.Name == "CODEX_HOME" {
			if nh.Path != override {
				t.Errorf("CODEX_HOME Path = %q, want override %q", nh.Path, override)
			}
			if nh.FromEnvVar != "CODEX_HOME" {
				t.Errorf("CODEX_HOME FromEnvVar = %q, want %q", nh.FromEnvVar, "CODEX_HOME")
			}
		}
	}
}

func TestDetectHost_CodexSharedAgentsSkillsHome(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "codex", "codex-cli 0.144.5\n")
	home := t.TempDir()
	env := Environment{Vars: []string{"HOME=" + home, "PATH=" + binDir}}

	det, err := DetectHost(context.Background(), env, "codex")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}
	want := filepath.Join(home, ".agents", "skills")
	found := false
	for _, nh := range det.NativeHomes {
		if nh.Name == "HOME/.agents/skills" {
			found = true
			if nh.Path != want {
				t.Errorf("HOME/.agents/skills Path = %q, want %q", nh.Path, want)
			}
		}
	}
	if !found {
		t.Error("no HOME/.agents/skills entry in NativeHomes for codex")
	}
}

func TestDetectHost_ClaudeInstalledDefaultHome(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "claude", "2.1.211 (Claude Code)\n")
	home := t.TempDir()
	env := Environment{Vars: []string{"HOME=" + home, "PATH=" + binDir}}

	det, err := DetectHost(context.Background(), env, "claude-code")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}
	if !det.Installed {
		t.Fatal("Installed = false, want true")
	}
	if det.Version != "2.1.211" {
		t.Errorf("Version = %q, want %q", det.Version, "2.1.211")
	}

	wantConfigDir := filepath.Join(home, ".claude")
	found := false
	for _, nh := range det.NativeHomes {
		if nh.Name == "CLAUDE_CONFIG_DIR" {
			found = true
			if nh.Path != wantConfigDir {
				t.Errorf("CLAUDE_CONFIG_DIR Path = %q, want %q", nh.Path, wantConfigDir)
			}
			if nh.FromEnvVar != "" {
				t.Errorf("CLAUDE_CONFIG_DIR FromEnvVar = %q, want empty (unset, default in use)", nh.FromEnvVar)
			}
		}
	}
	if !found {
		t.Error("no CLAUDE_CONFIG_DIR entry in NativeHomes")
	}
}

func TestDetectHost_ClaudeConfigDirOverride(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "claude", "2.1.211 (Claude Code)\n")
	home := t.TempDir()
	override := t.TempDir()
	env := Environment{Vars: []string{
		"HOME=" + home,
		"PATH=" + binDir,
		"CLAUDE_CONFIG_DIR=" + override,
	}}

	det, err := DetectHost(context.Background(), env, "claude-code")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}
	for _, nh := range det.NativeHomes {
		if nh.Name == "CLAUDE_CONFIG_DIR" {
			if nh.Path != override {
				t.Errorf("CLAUDE_CONFIG_DIR Path = %q, want override %q", nh.Path, override)
			}
			if nh.FromEnvVar != "CLAUDE_CONFIG_DIR" {
				t.Errorf("CLAUDE_CONFIG_DIR FromEnvVar = %q, want %q", nh.FromEnvVar, "CLAUDE_CONFIG_DIR")
			}
		}
	}
}

// TestDetectHost_ClaudeHomeClaudeJSON_DefaultHome is the regression test for
// the real bug this package's own commit history root-caused live against a
// machine with real MCP servers configured: `.claude.json` resolves to bare
// $HOME/.claude.json when CLAUDE_CONFIG_DIR is unset — a SIBLING of the
// default $HOME/.claude asset directory — never nested inside it. A
// previous version of claudeNativeHomes only reported a "CLAUDE_CONFIG_DIR"
// entry defaulted to $HOME/.claude, which internal/observe/rules.go then
// (wrongly) used to look for .claude.json at $HOME/.claude/.claude.json —
// a path that never exists on a real machine, so claude-code always found 0
// mcp_server candidates from this source. This asserts the new
// "HOME/.claude.json" entry's default Path is bare $HOME, not $HOME/.claude.
func TestDetectHost_ClaudeHomeClaudeJSON_DefaultHome(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "claude", "2.1.211 (Claude Code)\n")
	home := t.TempDir()
	env := Environment{Vars: []string{"HOME=" + home, "PATH=" + binDir}}

	det, err := DetectHost(context.Background(), env, "claude-code")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}

	found := false
	for _, nh := range det.NativeHomes {
		if nh.Name == "HOME/.claude.json" {
			found = true
			if nh.Path != home {
				t.Errorf("HOME/.claude.json Path = %q, want bare HOME %q", nh.Path, home)
			}
			// The old, wrong assumption: nested under the default asset
			// directory. Assert this is NOT what gets reported anymore.
			wrongNestedPath := filepath.Join(home, ".claude")
			if nh.Path == wrongNestedPath {
				t.Errorf("HOME/.claude.json Path = %q, matches the OLD wrong nested-under-.claude assumption; want bare HOME", nh.Path)
			}
			if nh.FromEnvVar != "" {
				t.Errorf("HOME/.claude.json FromEnvVar = %q, want empty (CLAUDE_CONFIG_DIR unset, default in use)", nh.FromEnvVar)
			}
		}
	}
	if !found {
		t.Fatal("no HOME/.claude.json entry in NativeHomes")
	}
}

// TestDetectHost_ClaudeHomeClaudeJSON_ConfigDirOverride proves that when
// CLAUDE_CONFIG_DIR IS explicitly set, "HOME/.claude.json"'s Path collapses
// to the identical directory as "CLAUDE_CONFIG_DIR"'s own Path — matching
// real Claude Code's own behavior of relocating .claude.json right along
// with the asset directory in that case (read-only `strings`-extraction
// evidence against the installed binary, see claudeNativeHomes' doc
// comment). This is the one case this bug fix does NOT change: it already
// worked correctly before this fix, and must keep working identically.
func TestDetectHost_ClaudeHomeClaudeJSON_ConfigDirOverride(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "claude", "2.1.211 (Claude Code)\n")
	home := t.TempDir()
	override := t.TempDir()
	env := Environment{Vars: []string{
		"HOME=" + home,
		"PATH=" + binDir,
		"CLAUDE_CONFIG_DIR=" + override,
	}}

	det, err := DetectHost(context.Background(), env, "claude-code")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}
	found := false
	for _, nh := range det.NativeHomes {
		if nh.Name == "HOME/.claude.json" {
			found = true
			if nh.Path != override {
				t.Errorf("HOME/.claude.json Path = %q, want override %q", nh.Path, override)
			}
			if nh.FromEnvVar != "CLAUDE_CONFIG_DIR" {
				t.Errorf("HOME/.claude.json FromEnvVar = %q, want %q", nh.FromEnvVar, "CLAUDE_CONFIG_DIR")
			}
		}
	}
	if !found {
		t.Fatal("no HOME/.claude.json entry in NativeHomes")
	}
}

func TestDetectHost_VersionProbeUnparseableOutput(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "codex", "no version number in here\n")
	home := t.TempDir()
	env := Environment{Vars: []string{"HOME=" + home, "PATH=" + binDir}}

	det, err := DetectHost(context.Background(), env, "codex")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}
	if !det.Installed {
		t.Fatal("Installed = false, want true (the binary was found and ran)")
	}
	if det.Version != "" {
		t.Errorf("Version = %q, want empty when output is unparseable", det.Version)
	}
	if det.Error == "" {
		t.Error("Error is empty, want a non-fatal probe error recorded")
	}
}

// TestProbeVersion_ZeroValueEnvironmentDoesNotInheritRealEnvironment guards
// against a specific os/exec pitfall: exec.Cmd.Env == nil means "inherit the
// calling process's entire real environment," the opposite of what a
// zero-value Environment{} (Vars == nil) should ever produce here. This
// calls probeVersion directly, bypassing DetectHost's own HOME check, to
// prove the protection lives in probeVersion itself and does not depend on
// that caller-side guard remaining in place.
func TestProbeVersion_ZeroValueEnvironmentDoesNotInheritRealEnvironment(t *testing.T) {
	t.Setenv("OMCA_CONTEXT_LEAK_CANARY", "leaked")

	binDir := t.TempDir()
	path := filepath.Join(binDir, "probe")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" != \"--version\" ]; then exit 1; fi\n" +
		"if [ -n \"$OMCA_CONTEXT_LEAK_CANARY\" ]; then printf '9.9.9-LEAKED\\n'; else printf '1.0.0\\n'; fi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := probeVersion(context.Background(), path, Environment{}, "codex")
	if err != nil {
		t.Fatalf("probeVersion: %v", err)
	}
	if strings.Contains(got, "LEAKED") {
		t.Fatalf("probeVersion(..., Environment{}, ...) = %q: the real process environment leaked into a zero-value Environment's subprocess", got)
	}
	if got != "1.0.0" {
		t.Errorf("probeVersion(..., Environment{}, ...) = %q, want %q", got, "1.0.0")
	}
}

func TestDetectHost_NeverPassesUnexpectedArgs(t *testing.T) {
	// A fake binary that only recognizes exactly "--version" and errors on
	// anything else proves DetectHost never passes a different or
	// additional argument.
	binDir := t.TempDir()
	path := filepath.Join(binDir, "codex")
	script := "#!/bin/sh\nif [ \"$#\" -eq 1 ] && [ \"$1\" = \"--version\" ]; then echo 'codex-cli 1.2.3'; else echo 'unexpected args' >&2; exit 1; fi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	env := Environment{Vars: []string{"HOME=" + home, "PATH=" + binDir}}

	det, err := DetectHost(context.Background(), env, "codex")
	if err != nil {
		t.Fatalf("DetectHost: %v", err)
	}
	if det.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q; Error=%q", det.Version, "1.2.3", det.Error)
	}
}

func TestExtractVersion(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		host    string
		want    string
		wantErr bool
	}{
		{"codex format", "codex-cli 0.144.5\n", "codex", "0.144.5", false},
		{"claude format", "2.1.211 (Claude Code)\n", "claude-code", "2.1.211", false},
		{"no version, known host", "not a version string\n", "codex", "", true},
		{"empty, known host", "", "codex", "", true},
		{"unknown host falls back to loose scan", "9.9.9\n", "some-other-host", "9.9.9", false},

		// The strict, whole-line, host-specific pattern must win over a
		// decoy version-shaped number on an earlier or later line — the
		// exact scenario a code-review pass on this PR flagged as an
		// unanchored-regex risk (e.g. an asdf/Node shim emitting a
		// deprecation warning naming an unrelated version before codex's
		// own output line).
		{
			"codex: decoy version on an earlier line is not picked",
			"(node:12345) DeprecationWarning: something (node 18.17.0)\ncodex-cli 0.144.5\n",
			"codex", "0.144.5", false,
		},
		{
			"claude: decoy version on an earlier line is not picked",
			"npm warn using --force Recommended protections disabled (npm 10.2.4)\n2.1.211 (Claude Code)\n",
			"claude-code", "2.1.211", false,
		},
		{
			"codex: decoy version on a later line is not picked",
			"codex-cli 0.144.5\nsome trailing diagnostic mentioning 9.9.9\n",
			"codex", "0.144.5", false,
		},
		{
			"codex: extra text on the version line itself defeats the strict match, falls back loosely",
			"codex-cli 0.144.5 (extra trailing text)\n",
			"codex", "0.144.5", false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := extractVersion(c.output, c.host)
			if (err != nil) != c.wantErr {
				t.Fatalf("extractVersion(%q, %q) error = %v, wantErr %v", c.output, c.host, err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("extractVersion(%q, %q) = %q, want %q", c.output, c.host, got, c.want)
			}
		})
	}
}

func TestLookPathIn(t *testing.T) {
	t.Run("empty name", func(t *testing.T) {
		if _, err := lookPathIn("", "/usr/bin"); err == nil {
			t.Error("lookPathIn(\"\", ...): want error")
		}
	})

	t.Run("absolute path that exists and is executable", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeBinary(t, dir, "tool", "1.0.0")
		abs := filepath.Join(dir, "tool")
		got, err := lookPathIn(abs, "/does/not/matter")
		if err != nil {
			t.Fatalf("lookPathIn(%s): %v", abs, err)
		}
		if got != abs {
			t.Errorf("lookPathIn(%s) = %q, want %q", abs, got, abs)
		}
	})

	t.Run("absolute path that does not exist", func(t *testing.T) {
		if _, err := lookPathIn(filepath.Join(t.TempDir(), "missing"), "/usr/bin"); err == nil {
			t.Error("lookPathIn(missing absolute path): want error")
		}
	})

	t.Run("relative-with-separator name that does not exist", func(t *testing.T) {
		if _, err := lookPathIn(filepath.Join("subdir", "tool"), "/usr/bin"); err == nil {
			t.Error("lookPathIn(subdir/tool): want error for a non-existent relative path containing a separator")
		}
	})

	t.Run("not found on any PATH entry", func(t *testing.T) {
		if _, err := lookPathIn("definitely-not-a-real-tool-omca-context", t.TempDir()); err == nil {
			t.Error("lookPathIn: want error when name is on no PATH entry")
		}
	})

	t.Run("skips empty PATH entries", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeBinary(t, dir, "tool", "1.0.0")
		pathEnv := "" + string(filepath.ListSeparator) + dir
		got, err := lookPathIn("tool", pathEnv)
		if err != nil {
			t.Fatalf("lookPathIn: %v", err)
		}
		if got != filepath.Join(dir, "tool") {
			t.Errorf("lookPathIn = %q, want %q", got, filepath.Join(dir, "tool"))
		}
	})
}

func TestPlatformStringShape(t *testing.T) {
	p := platformString()
	if !strings.Contains(p, "-") {
		t.Errorf("platformString() = %q, want a %q-separated goos-goarch shape", p, "-")
	}
	if !strings.HasPrefix(p, runtime.GOOS) {
		t.Errorf("platformString() = %q, want prefix %q", p, runtime.GOOS)
	}
}

func TestDetectedHostIDsOrder(t *testing.T) {
	want := []string{"codex", "claude-code"}
	if len(DetectedHostIDs) != len(want) {
		t.Fatalf("DetectedHostIDs = %v, want %v", DetectedHostIDs, want)
	}
	for i := range want {
		if DetectedHostIDs[i] != want[i] {
			t.Errorf("DetectedHostIDs[%d] = %q, want %q (codex must lead, matching this project's documented qualification order)", i, DetectedHostIDs[i], want[i])
		}
	}
}
