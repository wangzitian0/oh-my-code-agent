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
	return filepath.Base(filepath.Dir(shimsDir)) == ".asdf"
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

	var plugin, version string
	matches := 0
	for _, line := range strings.Split(string(data), "\n") {
		m := asdfPluginCommentRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		matches++
		plugin, version = m[1], m[2]
	}
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
