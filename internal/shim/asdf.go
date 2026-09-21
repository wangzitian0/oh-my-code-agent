package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// asdfPluginCommentRE matches one "# asdf-plugin: <plugin> <version>" line.
// asdf itself writes exactly this comment into every shim script it
// generates via `asdf reshim` -- one line per installed plugin version that
// can provide the shimmed command name. This is asdf's own long-standing
// shim-metadata convention (present in both its original bash
// implementation and its Go rewrite; verified read-only, during this fix's
// investigation, against a real, installed asdf 0.18.0's ~/.asdf/shims/*,
// see issue #69), not something this package infers or reimplements.
var asdfPluginCommentRE = regexp.MustCompile(`^# asdf-plugin: (\S+) (\S+)\s*$`)

// IsASDFShim reports whether path names an asdf-managed shim script, by
// location alone: asdf's own default layout always places shims at
// "<ASDF_DATA_DIR>/shims/<name>", and every asdf installation this project
// has observed (including the one issue #69's own reproduction hit) uses
// the default ASDF_DATA_DIR of "$HOME/.asdf" -- so a shim's grandparent
// directory is named ".asdf". This is a location heuristic only, cheap
// enough to run on every resolved binary path before deciding whether the
// more expensive, content-based ResolveASDFShimTarget is worth attempting;
// ResolveASDFShimTarget independently confirms the file actually contains
// asdf's own shim metadata before anything acts on the result, so a
// same-shaped-but-unrelated directory (e.g. some other tool's own
// ".asdf/shims" convention, if one existed) can never be silently treated
// as resolvable -- it would just fail ResolveASDFShimTarget's own check and
// fall through to this project's actionable-error path.
func IsASDFShim(path string) bool {
	if path == "" {
		return false
	}
	shimsDir := filepath.Dir(path)
	if filepath.Base(shimsDir) != "shims" {
		return false
	}
	if filepath.Base(filepath.Dir(shimsDir)) == ".asdf" {
		return true
	}
	// asdf's own ASDF_DATA_DIR env var can relocate its data directory away
	// from the default "$HOME/.asdf" (asdf's own documented override), so a
	// shim's grandparent directory need not literally be named ".asdf" --
	// asdf's "<dataDir>/shims/<name>" layout is otherwise unchanged. Fall
	// back to confirming the file itself carries asdf's own shim-metadata
	// comment (the same signal ResolveASDFShimTarget independently
	// re-confirms before ever acting on anything) rather than silently
	// missing every non-default-ASDF_DATA_DIR installation (a real Copilot
	// review finding on this PR).
	return hasASDFPluginComment(path)
}

// hasASDFPluginComment reports whether path's content contains at least one
// "# asdf-plugin: <plugin> <version>" line -- IsASDFShim's fallback
// confirmation for a non-default ASDF_DATA_DIR layout. A read error (path
// does not exist, is a directory, permission denied, ...) is treated as
// "not a recognizable asdf shim" rather than propagated: IsASDFShim is a
// cheap yes/no gate callers use to decide whether the more expensive,
// error-returning ResolveASDFShimTarget is worth attempting at all, so it
// has no error to report here -- ResolveASDFShimTarget re-reads the file
// itself and surfaces any real I/O error through its own return value.
func hasASDFPluginComment(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if asdfPluginCommentRE.MatchString(line) {
			return true
		}
	}
	return false
}

// ResolveASDFShimTarget resolves shimPath -- an asdf shim script, per
// IsASDFShim -- to the concrete, per-version real binary asdf itself would
// have dispatched it to, without ever invoking the asdf binary, running the
// shim script, or depending on any particular HOME to do so. This is what
// lets `omca run --mode isolated`/the PATH shim exec directly past an
// asdf-shimmed host installation: the shim script's own dispatch (`exec
// asdf exec "<name>" "$@"` in every asdf version this project has observed)
// needs a real, resolvable HOME to find asdf's own ~/.tool-versions-derived
// state (docs/architecture/runtime.md §7.1, issue #69) and fails outright
// (exit 126) under isolated mode's virtualized HOME -- but the version asdf
// would have picked is already recorded, by asdf's own `asdf reshim` step,
// in the shim script's "# asdf-plugin: <plugin> <version>" comment line(s),
// so this function never needs to ask asdf anything at runtime.
//
// When exactly one such comment line is present -- the common case, and the
// one issue #69's own reproduction hit (a single "nodejs 20.19.0" line for
// `codex`, an npm-global-installed CLI reshimmed under a single Node
// version) -- the choice is already unambiguous. Two or more lines mean two
// or more installed plugin versions can provide this command name, and only
// asdf's own .tool-versions precedence (ASDF_<PLUGIN>_VERSION env var, then
// .tool-versions walking up from cwd, then a global .tool-versions) can
// disambiguate which one is "current" for a given invocation -- replicating
// that selection algorithm here would mean depending on undocumented,
// version-dependent asdf internals this project has no contract with
// (docs/architecture/runtime.md §7.1's own documented isolation-tradeoff
// discussion), so this function deliberately refuses to guess and returns
// an error instead. A caller that gets this error should fall back to a
// clear, actionable message rather than attempting anything silently -- see
// cmd/omca/run.go's runIsolated and this package's plan.go Build for the
// two call sites that do exactly that.
//
// The candidate real binary is
// "<asdfDataDir>/installs/<plugin>/<version>/bin/<name>", where asdfDataDir
// is shimPath's own grandparent directory (shimPath is already
// "<asdfDataDir>/shims/<name>" by IsASDFShim's own contract, so this needs
// no HOME lookup either -- it works identically regardless of what HOME
// this process's own caller happens to have) and name is shimPath's
// basename. The candidate must exist and be executable, or this function
// errors rather than handing back a path a later exec would just fail
// against with the same unhelpful "cannot execute" this whole fix exists to
// replace with something actionable.
func ResolveASDFShimTarget(shimPath string) (string, error) {
	data, err := os.ReadFile(shimPath)
	if err != nil {
		return "", fmt.Errorf("shim: ResolveASDFShimTarget: reading %s: %w", shimPath, err)
	}

	// Distinct (plugin, version) pairs, not raw matching-line count: asdf
	// has been observed to write more than one identical "# asdf-plugin:
	// <plugin> <version>" line for the same pair into a single shim script
	// (a real Copilot review finding on this PR) -- counting raw lines
	// would incorrectly report that as "2 different plugin versions" and
	// refuse to resolve a shim that is, in fact, completely unambiguous.
	type pluginVersion struct{ plugin, version string }
	seen := map[pluginVersion]bool{}
	var plugin, version string
	for _, line := range strings.Split(string(data), "\n") {
		m := asdfPluginCommentRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pv := pluginVersion{plugin: m[1], version: m[2]}
		if !seen[pv] {
			seen[pv] = true
			plugin, version = pv.plugin, pv.version
		}
	}
	matches := len(seen)
	if matches == 0 {
		return "", fmt.Errorf("shim: ResolveASDFShimTarget: %s has no \"# asdf-plugin: <plugin> <version>\" metadata line; not a resolvable asdf shim (unrecognized shim format)", shimPath)
	}
	if matches > 1 {
		return "", fmt.Errorf("shim: ResolveASDFShimTarget: %s names %d different plugin versions that could provide this command; disambiguating requires asdf's own .tool-versions resolution, which this project will not replicate (see internal/shim/asdf.go's doc comment)", shimPath, matches)
	}

	asdfDataDir := filepath.Dir(filepath.Dir(shimPath)) // shimPath is <asdfDataDir>/shims/<name>
	name := filepath.Base(shimPath)
	candidate := filepath.Join(asdfDataDir, "installs", plugin, version, "bin", name)
	if !isExecutableFile(candidate) {
		return "", fmt.Errorf("shim: ResolveASDFShimTarget: resolved target %s (asdf plugin %s %s) does not exist or is not executable", candidate, plugin, version)
	}
	return candidate, nil
}

// ResolveShebangInterpreter resolves the interpreter that a
// "#!/usr/bin/env <name>" script defers to, so a caller can exec that
// interpreter directly instead of letting the OS resolve <name> through PATH
// at exec time -- which, under the virtualized HOME isolated mode always
// runs with, is exactly where an asdf-managed interpreter dies.
//
// Returns ("", nil) for the two soft cases: realPath is not an env-indirect
// script at all, or <name> is not on PATH outside shimDir. Both leave the
// caller doing what it did before -- exec realPath directly -- rather than
// turning "we could not pre-resolve this" into a hard failure.
//
// Returns an error only for the case that is NOT safe to fall through: the
// interpreter resolves to an asdf shim that cannot be resolved to a concrete
// binary. Exec'ing that shim under a virtualized HOME produces a bare exit
// 126 with no output whatsoever, strictly inside the shim script and
// strictly after the caller's own process image is gone, so nothing the
// caller does afterwards can explain it. Failing closed here is the only
// way that stays diagnosable.
//
// One extracted copy, not two: cmd/omca/run.go and plan.go's Build both need
// this, and when it lived twice the second copy still had the silent
// fallback after the first was fixed.
// virtualizingHome says whether the caller is about to override HOME for
// this exec. It is the entire reason any of this is needed: asdf's dispatch
// only breaks because HOME stops resolving. A caller that leaves HOME real
// -- a tier-2 host under ADR 0006, or `--mode native` -- must pass false,
// and then an unresolvable asdf interpreter is not an error at all: the OS
// resolves it at exec time exactly as it does outside OMCA, and failing
// closed would break a host that works (Copilot review finding).
func ResolveShebangInterpreter(realPath, pathEnv, shimDir string, virtualizingHome bool) (string, error) {
	name, isEnvIndirect := ShebangEnvIndirectInterpreter(realPath)
	if !isEnvIndirect {
		return "", nil
	}
	if !virtualizingHome {
		// Nothing to pre-resolve: HOME stays real, so the OS's own shebang
		// handling reaches the same interpreter it would unmanaged.
		return "", nil
	}

	// Prefer the interpreter inside the SAME asdf install as realPath.
	//
	// When realPath is <dataDir>/installs/<plugin>/<version>/bin/<x>, the
	// interpreter belonging to it is <same dir>/<name>: a Node CLI installed
	// under nodejs 20.19.0 runs on that install's node. This reads the
	// answer off the path asdf already resolved rather than guessing, and it
	// is the only branch that works when the machine has two node versions
	// installed -- their shared asdf `node` shim names both plugin versions,
	// which ResolveASDFShimTarget correctly refuses to choose between.
	//
	// Scoped to an asdf install directory, not applied to every shebang
	// script: outside that layout "a file with the interpreter's name next
	// to the script" carries none of the same meaning, and preferring it
	// would silently shadow the interpreter PATH would have chosen
	// (Copilot review finding).
	if isASDFInstallBinDir(filepath.Dir(realPath)) {
		if sibling := filepath.Join(filepath.Dir(realPath), name); isExecutableFile(sibling) {
			return sibling, nil
		}
	}

	candidate, err := ResolveReal(name, pathEnv, shimDir)
	if err != nil {
		return "", nil // soft case: not on PATH outside the shim dir
	}
	if !IsASDFShim(candidate) {
		return candidate, nil
	}
	resolved, asdfErr := ResolveASDFShimTarget(candidate)
	if asdfErr != nil {
		return "", fmt.Errorf("interpreter %q resolves to the asdf shim %s, which names more than one installed version (or no resolvable one): %w -- this launch virtualizes HOME, under which that shim's own dispatch fails as a bare exit 126 with no output. Fix it by removing the %s versions you do not use (`asdf uninstall`) so the shim names exactly one, by installing %s outside asdf, or by using `omca run --mode native`", name, candidate, asdfErr, name, name)
	}
	return resolved, nil
}

// isASDFInstallBinDir reports whether dir has asdf's own
// "<dataDir>/installs/<plugin>/<version>/bin" shape. Only inside that layout
// does a file sitting beside a script carry the meaning "the interpreter
// this package was installed against."
func isASDFInstallBinDir(dir string) bool {
	if filepath.Base(dir) != "bin" {
		return false
	}
	version := filepath.Dir(dir)      // <dataDir>/installs/<plugin>/<version>
	plugin := filepath.Dir(version)   // <dataDir>/installs/<plugin>
	installs := filepath.Dir(plugin)  // <dataDir>/installs
	return filepath.Base(installs) == "installs" && filepath.Base(plugin) != "" && filepath.Base(version) != ""
}
