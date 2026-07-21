package context

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// DetectedHostIDs are the canonical host IDs this package can detect, in a
// fixed, deliberate order — codex first, matching this project's own stated
// qualification order (docs/architecture/runtime.md §7: "Codex leads
// qualification... Claude Code follows inside the same milestones";
// docs/project/roadmap.md: "Codex leads inside each milestone") — so a
// Report's Hosts slice always has the same shape regardless of any map
// iteration internally.
var DetectedHostIDs = []string{"codex", "claude-code"}

// binaryNames maps a canonical host ID this package knows how to detect to
// the executable name resolved on PATH.
var binaryNames = map[string]string{
	"codex":       "codex",
	"claude-code": "claude",
}

// detectTimeout hard-bounds every host binary invocation this package makes,
// regardless of what a caller's context allows, mirroring
// internal/qualify.invokeTimeout's rationale: a hang must never be mistaken
// for a slow but passing detection.
const detectTimeout = 15 * time.Second

// versionArgs is the only argument list this package will ever pass to a
// host binary. There is no per-call variability to guard against (unlike
// internal/qualify, which runs a fixture-declared argument list and so needs
// a runtime allowlist check), because host detection only ever needs one
// fact: the installed version. codex --version and claude --version both
// print a version string and exit; neither starts an interactive session,
// calls a model, or touches the network (verified against `codex --help` /
// `claude --help`, see fixtures/README.md).
var versionArgs = []string{"--version"}

// NativeHome is one named native-home location a host adapter must
// inventory (docs/architecture/runtime.md §7.1 Codex / §7.2 Claude Code).
// Host detection reports the location itself, not its contents — walking
// what is inside it is internal/observe's job (see doc.go).
type NativeHome struct {
	// Name identifies which native home this is, e.g. "CODEX_HOME" or
	// "HOME/.agents/skills".
	Name string `json:"name"`
	// Path is the resolved absolute path.
	Path string `json:"path"`
	// FromEnvVar names the environment variable that supplied Path when an
	// override was set, or "" when Path is the unset-env-var default
	// (e.g. $HOME/.codex when CODEX_HOME is not set).
	FromEnvVar string `json:"fromEnvVar,omitempty"`
}

// HostDetection is what DetectHost observed for one host: whether its
// binary is installed, its resolved path and exact version if so, its
// surface, and its native home locations. Installed=false is an expected,
// non-error outcome — "not installed" is itself a detection result, not a
// detection failure.
type HostDetection struct {
	Host        string       `json:"host"`
	Surface     string       `json:"surface"`
	Platform    string       `json:"platform"`
	Installed   bool         `json:"installed"`
	BinaryPath  string       `json:"binaryPath,omitempty"`
	Version     string       `json:"version,omitempty"`
	NativeHomes []NativeHome `json:"nativeHomes,omitempty"`
	// Error carries a non-fatal detection problem for an installed binary
	// (e.g. `--version` produced output DetectHost could not parse). It is
	// deliberately a string, not a returned error: a probe failure for one
	// host degrades that host's own result rather than failing the whole
	// detection pass (see DetectHost's doc comment).
	Error string `json:"error,omitempty"`
}

// codexNativeHomes computes Codex's native home locations from env, per
// docs/architecture/runtime.md §7.1 ("isolated Codex home", "user Skills can
// also be discovered from $HOME/.agents/skills") and
// internal/qualify/realhome.go's RealHomePaths (the analogous, already
// merged list of real paths PR-06's harness watches for zero-write proof).
func codexNativeHomes(env Environment) []NativeHome {
	home := env.Get("HOME")
	codexHome := env.Get("CODEX_HOME")
	homes := []NativeHome{}
	if codexHome != "" {
		homes = append(homes, NativeHome{Name: "CODEX_HOME", Path: codexHome, FromEnvVar: "CODEX_HOME"})
	} else {
		homes = append(homes, NativeHome{Name: "CODEX_HOME", Path: filepath.Join(home, ".codex")})
	}
	homes = append(homes, NativeHome{Name: "HOME/.agents/skills", Path: filepath.Join(home, ".agents", "skills")})
	return homes
}

// claudeNativeHomes computes Claude Code's native home locations from env,
// per docs/architecture/runtime.md §7.2 ("a relocated configuration
// directory per generation") and fixtures/README.md's static-inspection
// finding that the relocation variable is CLAUDE_CONFIG_DIR (undocumented in
// `claude --help`; discovered by read-only inspection of the installed
// binary — see fixtures/README.md for the full evidentiary trail, including
// that this was never behaviorally confirmed by launching Claude Code with
// the variable set).
//
// This returns THREE entries, not two, because ~/.claude.json (Claude
// Code's user/local MCP-registry + trust/OAuth state file) resolves via a
// formula with a DIFFERENT unset-default fallback than every other file
// under the CLAUDE_CONFIG_DIR entry: read-only `strings` extraction against
// the real installed binary (fixtures/README.md's own static-inspection
// method, reproduced here against a second, newer install — see
// internal/observe/rules.go's claudeUserRules doc comment for the exact
// evidence and dated correction note) shows Claude Code computes it as
// `path.join(process.env.CLAUDE_CONFIG_DIR || os.homedir(), ".claude.json")`
// — i.e. CLAUDE_CONFIG_DIR, when SET, relocates .claude.json right along
// with the asset directory (both land directly under it, so the
// "CLAUDE_CONFIG_DIR" and "HOME/.claude.json" entries below deliberately
// collapse to the identical Path in that case), but when CLAUDE_CONFIG_DIR
// is UNSET, .claude.json sits at bare $HOME/.claude.json — a SIBLING of the
// default $HOME/.claude asset directory, never nested inside it, unlike
// this same fallback rule for every other Claude Code source file. A single
// "CLAUDE_CONFIG_DIR" NativeHome cannot represent both fallback shapes at
// once, hence the dedicated "HOME/.claude.json" entry (named after the
// resolved-shape convention "HOME/.agents/skills" below already
// established, not the runtime-level $HOME-redirection concept in
// internal/runtime/mutablehome.go, which this package has no relation to).
func claudeNativeHomes(env Environment) []NativeHome {
	home := env.Get("HOME")
	claudeConfigDir := env.Get("CLAUDE_CONFIG_DIR")
	homes := []NativeHome{}
	if claudeConfigDir != "" {
		homes = append(homes, NativeHome{Name: "CLAUDE_CONFIG_DIR", Path: claudeConfigDir, FromEnvVar: "CLAUDE_CONFIG_DIR"})
		homes = append(homes, NativeHome{Name: "HOME/.claude.json", Path: claudeConfigDir, FromEnvVar: "CLAUDE_CONFIG_DIR"})
	} else {
		homes = append(homes, NativeHome{Name: "CLAUDE_CONFIG_DIR", Path: filepath.Join(home, ".claude")})
		homes = append(homes, NativeHome{Name: "HOME/.claude.json", Path: home})
	}
	homes = append(homes, NativeHome{Name: "HOME/.agents/skills", Path: filepath.Join(home, ".agents", "skills")})
	return homes
}

// platformString reports this process's platform in the "<goos>-<goarch>"
// shape docs/knowledge/README.md §4's metadata.platforms uses (e.g.
// "darwin-arm64").
func platformString() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// BinaryName returns the executable name DetectHost resolves on PATH for a
// canonical host ID this package implements detection for (DetectedHostIDs)
// -- the read-only half of the binaryNames table above. PR-10 (issue #14,
// `omca env`/`omca run`/PATH shims) needs the exact same host-ID-to-binary-
// name correspondence DetectHost already encodes internally, both to build
// the PATH shim's two entry-point names (codex, claude) and to resolve a
// target host's real binary name for `omca run <host>`; exporting this
// tiny, static lookup avoids a second, driftable copy of binaryNames
// elsewhere in the module (contrast lookPathIn below, which stays
// unexported/duplicated on purpose because it is entangled with
// per-package sandboxing concerns, not a bare data table).
func BinaryName(host string) (string, error) {
	name, ok := binaryNames[host]
	if !ok {
		return "", fmt.Errorf("context: BinaryName: host %q is a known canonical host ID but this package does not implement detection for it (only %v)", host, DetectedHostIDs)
	}
	return name, nil
}

// DetectHost detects one host's binary, exact version, surface, and native
// home locations using env's PATH/HOME/CODEX_HOME/CLAUDE_CONFIG_DIR — never
// read implicitly. host must be a canonical ID this package implements
// detection for (DetectedHostIDs); anything else is a caller error, returned
// as a non-nil error. A binary that is simply not installed, or whose
// --version output this package cannot parse, is not a caller error: it is
// reported inside the returned HostDetection (Installed=false, or a non-empty
// Error) so a caller detecting both hosts can always get a complete,
// stable-shaped report rather than an all-or-nothing failure.
func DetectHost(ctx context.Context, env Environment, host string) (HostDetection, error) {
	if err := domain.ValidateHostID(host); err != nil {
		return HostDetection{}, fmt.Errorf("context: DetectHost: %w", err)
	}
	binName, ok := binaryNames[host]
	if !ok {
		return HostDetection{}, fmt.Errorf("context: DetectHost: host %q is a known canonical host ID but this package does not implement detection for it (only %v)", host, DetectedHostIDs)
	}
	home := env.Get("HOME")
	if home == "" {
		return HostDetection{}, fmt.Errorf("context: DetectHost: HOME is not set in the supplied Environment")
	}
	if !filepath.IsAbs(home) {
		return HostDetection{}, fmt.Errorf("context: DetectHost: HOME %q in the supplied Environment is not an absolute path", home)
	}

	det := HostDetection{
		Host:     host,
		Surface:  "cli",
		Platform: platformString(),
	}
	switch host {
	case "codex":
		det.NativeHomes = codexNativeHomes(env)
	case "claude-code":
		det.NativeHomes = claudeNativeHomes(env)
	}

	binPath, err := lookPathIn(binName, env.Get("PATH"))
	if err != nil {
		det.Installed = false
		return det, nil
	}
	det.Installed = true
	det.BinaryPath = binPath

	version, err := probeVersion(ctx, binPath, env, host)
	if err != nil {
		det.Error = err.Error()
		return det, nil
	}
	det.Version = version
	return det, nil
}

// probeVersion runs binPath --version — the only invocation this package
// ever makes of a host binary — and extracts host's version from the
// output. The subprocess environment is env.Vars exactly as supplied by the
// caller: for RealEnvironment() this is the real process environment (so a
// shim mechanism like asdf or nvm resolves the binary's own runtime exactly
// as it would for a manually typed `codex --version`), and for an injected
// test Environment it is whatever minimal synthetic slice the test
// constructed. This differs deliberately from internal/qualify's
// RunInvocation, which always redirects HOME (and CODEX_HOME/
// CLAUDE_CONFIG_DIR) into a fresh sandbox to prove a fixture case never
// touches the real host: this package's entire purpose is to observe the
// real installation, so there is no sandbox to redirect into — the safety
// boundary here is the fixed, non-interactive ["--version"] argument list
// and the hard timeout below, not environment isolation.
func probeVersion(ctx context.Context, binPath string, env Environment, host string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binPath, versionArgs...)
	// os/exec documents a nil Cmd.Env as "inherit the current process's
	// entire environment" — the exact opposite of this package's explicit,
	// nothing-implicit design. env.Vars is nil for a zero-value Environment,
	// so this must never be a bare assignment: an empty-but-non-nil slice
	// keeps "no environment was supplied" from silently becoming "inherit
	// everything" if a future caller ever reaches probeVersion without
	// going through DetectHost's own HOME check first.
	cmd.Env = append([]string{}, env.Vars...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("context: probeVersion: %s --version: %w (stderr: %s)", binPath, err, strings.TrimSpace(stderr.String()))
	}
	return extractVersion(stdout.String(), host)
}

// versionNumberPattern matches a MAJOR.MINOR.PATCH (optionally followed by a
// -prerelease or +build suffix) substring, used only by
// extractVersionLoosely's unanchored fallback below.
var versionNumberPattern = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)

// strictVersionLinePattern is the exact, whole-line shape each real host's
// --version prints as its entire line of output, capturing the version
// group (see fixtures/README.md): codex prints "codex-cli X.Y.Z", claude
// prints "X.Y.Z (Claude Code)". Matching a closed, anchored shape — instead
// of "any X.Y.Z-looking substring anywhere in the output, first one wins"
// — is what actually defends against a decoy version number a wrapper,
// shim, or runtime could print on an earlier line before the real one:
// codex is resolved through an asdf shim into a Node entrypoint, and a
// Node/npm deprecation warning naming an unrelated version (e.g. a Node
// runtime version) is exactly the kind of line this must not be fooled by
// (see fixtures/README.md's acquisition-method notes for both hosts).
var strictVersionLinePattern = map[string]*regexp.Regexp{
	"codex":       regexp.MustCompile(`^codex-cli\s+(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)$`),
	"claude-code": regexp.MustCompile(`^(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)\s*\(Claude Code\)$`),
}

// extractVersion first looks for host's known, exact --version line shape
// on any line of output (so a decoy line elsewhere, before or after, cannot
// be mistaken for it). If host has no known strict shape (not one of
// strictVersionLinePattern's two entries), or output never contains a line
// matching it, extractVersion falls back to extractVersionLoosely — a
// weaker, best-effort guess that does not defend against a decoy.
func extractVersion(output, host string) (string, error) {
	if strict, ok := strictVersionLinePattern[host]; ok {
		for _, line := range strings.Split(output, "\n") {
			if m := strict.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				return m[1], nil
			}
		}
	}
	return extractVersionLoosely(output)
}

// extractVersionLoosely searches only output's first non-empty line for any
// MAJOR.MINOR.PATCH-shaped substring. This is the fallback path for a host
// with no strictVersionLinePattern entry; it offers no real protection
// against a decoy sharing that first line, which is exactly why
// extractVersion prefers the anchored, whole-line match above whenever a
// strict pattern for the host is known.
func extractVersionLoosely(output string) (string, error) {
	var firstLine string
	for _, line := range strings.Split(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			firstLine = trimmed
			break
		}
	}
	m := versionNumberPattern.FindString(firstLine)
	if m == "" {
		return "", fmt.Errorf("context: extractVersionLoosely: no MAJOR.MINOR.PATCH version number found on the first non-empty line %q", firstLine)
	}
	return m, nil
}

// lookPathIn resolves name to an executable path using pathEnv (a
// PATH-shaped, os.PathListSeparator-delimited list of directories) instead
// of the calling process's actual environment — deliberately separate from
// exec.LookPath/os.LookPath, which both consult the real process
// environment's PATH. This is a small, intentional duplicate of
// internal/qualify's own unexported lookPathIn: qualify's version is
// entangled with its Sandbox type (which this package must not use — see
// probeVersion's doc comment for why), and qualify does not export it, so
// reusing it would mean either depending on an unrelated package's
// internals or widening PR-06's already-merged public API for a ~15-line
// function. A future cleanup could extract both into a shared leaf package.
func lookPathIn(name, pathEnv string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("context: lookPathIn: empty command name")
	}
	if filepath.IsAbs(name) || filepath.Base(name) != name {
		if isExecutableFile(name) {
			return name, nil
		}
		return "", fmt.Errorf("context: lookPathIn: %s: not found", name)
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("context: lookPathIn: %s: not found in supplied PATH", name)
}

// isExecutableFile checks the Unix executable permission bits, matching
// internal/qualify's own isExecutableFile. This does not detect an
// executable on Windows (file.Mode() never carries a 0111 bit there
// regardless of the file's actual runnability) — an accepted, pre-existing
// scope limit rather than one this package introduces: this project's first
// implementation slice is explicitly macOS-only
// (docs/project/roadmap.md, "First Implementation Slice"), and
// internal/qualify has run with this exact same limitation since PR-06.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
