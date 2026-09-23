package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The two ways the cross-repository rule layers reach a checkout.
//
// They are not committed and cannot be: they carry names and absolute paths
// that belong to one machine, and the consuming repositories are public. So a
// checkout receives them either by directory inheritance from an ancestor, or
// from files rendered onto that checkout -- and both depend on where the
// checkout sits, which is the part nobody was told (dev_env#128).
var (
	// Rendered onto the checkout itself. Excluded from git by design.
	renderedArtifacts = []string{"CLAUDE.local.md", "AGENTS.override.md"}
	// Carried by an ancestor directory and inherited downward.
	inheritedCarriers = []string{"CLAUDE.md", "CLAUDE.local.md", "AGENTS.md"}
)

// checkRulesReach answers whether this worktree can receive the Root and
// Workspace rule layers at all.
//
// Why a worktree can silently miss them: the repository's own rules ride along
// with git, so a fresh worktree always has those. The cross-repository layers
// do not, and `git worktree add` has no hook that renders them. Put the
// worktree somewhere the inheritance does not reach -- a session scratchpad, a
// host's own worktrees directory -- and every session there runs without the
// escalation discipline, the red lines and the collaboration model, with
// nothing saying so. Measured: two worktrees of the same commit, one under the
// workspace root and one in a scratchpad, differ on exactly this.
//
// The test is filesystem-only on purpose. Asking a model what it loaded is the
// ground truth, but a check that costs an inference call is a check nobody runs
// before starting work.
func checkRulesReach(worktreeRoot string) doctorFinding {
	const check = "rules-reach"

	for _, name := range renderedArtifacts {
		if _, err := os.Stat(filepath.Join(worktreeRoot, name)); err == nil {
			return doctorFinding{
				Check: check, Status: statusOK,
				Detail: fmt.Sprintf("rendered %s is present in this worktree", name),
			}
		}
	}

	if carrier, ok := ancestorRuleCarrier(worktreeRoot); ok {
		return doctorFinding{
			Check: check, Status: statusOK,
			Detail: fmt.Sprintf("inherited from %s", carrier),
		}
	}

	return doctorFinding{
		Check: check, Status: statusFail,
		Detail: fmt.Sprintf(
			"this worktree receives no cross-repository rules: no ancestor carries %s, and none of %s is rendered here. "+
				"The repository's own rules are committed and fine; what is missing is everything above them, silently. "+
				"Move the worktree under the directory that carries them, or render them onto this one",
			strings.Join(inheritedCarriers, "/"), strings.Join(renderedArtifacts, ", ")),
	}
}

// ancestorRuleCarrier walks up from the worktree looking for a rule file that
// is not the worktree's own.
//
// Starting one level up, not at the worktree: a repository's committed
// CLAUDE.md is its *own* layer, and counting it would make every repository
// look covered while the layers above it were absent -- the exact false green
// this check exists to prevent.
func ancestorRuleCarrier(worktreeRoot string) (string, bool) {
	dir := filepath.Dir(filepath.Clean(worktreeRoot))
	for {
		for _, name := range inheritedCarriers {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
