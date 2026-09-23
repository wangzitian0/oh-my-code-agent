package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The dev_env tool that owns the judgement. OMCA calls it and shows the
// answer; it does not recompute one. Re-deriving "is this copy 1:1" here
// would be a second implementation of a rule that lives elsewhere, and the
// two would disagree on the day it mattered (core.harness.md §5.1 puts
// "重实现 render / drift 判定" under what this repository must never do).
const skillSyncBin = "ws-skills-sync"

// How long the check waits. The tool hashes a handful of files and asks git
// one question, so seconds is generous; the point of the bound is that
// `omca doctor` must not hang because something upstream did.
const skillSyncTimeout = 20 * time.Second

// checkSkillDelivery answers "are this worktree's vendored skills still 1:1
// with their source, and can a host actually find them".
//
// Why it is worth a doctor check at all: the two failure modes are both
// silent. A copy that has drifted from the source still loads, and simply
// tells the next agent something the source no longer says. A copy at an
// address no host reads is committed, reviewed, hashed green -- and never
// consulted (dev_env#120, which is exactly how eight skills sat unread in
// four repositories for a fortnight while `--check` reported byte-identical).
//
// Absence of the tool is not a failure. OMCA runs on machines that have no
// dev_env at all, and a repository that vendors nothing has nothing to check;
// reporting FAIL for either would train people to ignore this line.
func checkSkillDelivery(worktreeRoot string) doctorFinding {
	const check = "skill-delivery"

	if _, err := os.Stat(filepath.Join(worktreeRoot, "skills")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return doctorFinding{
				Check: check, Status: statusOK,
				Detail: "this worktree vendors no skills (no skills/ directory) — nothing to keep in sync",
			}
		}
		return doctorFinding{
			Check: check, Status: statusWarn,
			Detail: fmt.Sprintf("cannot read %s: %v", filepath.Join(worktreeRoot, "skills"), err),
		}
	}

	bin, err := exec.LookPath(skillSyncBin)
	if err != nil {
		return doctorFinding{
			Check: check, Status: statusWarn,
			Detail: fmt.Sprintf(
				"this worktree vendors skills but %s is not on PATH, so whether they still match their source is unknown — not checked, which is not the same as fine",
				skillSyncBin),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), skillSyncTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--check", "--repo", worktreeRoot, "--quiet")
	out, runErr := cmd.CombinedOutput()
	detail := strings.TrimSpace(string(out))

	if ctx.Err() != nil {
		return doctorFinding{
			Check: check, Status: statusWarn,
			Detail: fmt.Sprintf("%s did not answer within %s — unknown, not clean", skillSyncBin, skillSyncTimeout),
		}
	}
	if runErr == nil {
		return doctorFinding{
			Check: check, Status: statusOK,
			Detail: "vendored skills are byte-identical to their source and reachable at the address a host reads",
		}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		switch exitErr.ExitCode() {
		case 1:
			return doctorFinding{
				Check: check, Status: statusFail,
				Detail: summariseSkillDrift(detail),
			}
		case 2:
			// The tool's "not applicable" code: not a git checkout, or no
			// skill declares `ship: repo`. Neither is this worktree's fault.
			return doctorFinding{
				Check: check, Status: statusWarn,
				Detail: fmt.Sprintf("%s reported nothing to check: %s", skillSyncBin, summariseSkillDrift(detail)),
			}
		}
	}
	return doctorFinding{
		Check: check, Status: statusWarn,
		Detail: fmt.Sprintf("could not run %s: %v (%s)", skillSyncBin, runErr, summariseSkillDrift(detail)),
	}
}

// summariseSkillDrift keeps the upstream tool's own words, bounded.
//
// Its wording is the part a reader acts on, and paraphrasing it here would be
// the same second implementation in prose. What this does is put a ceiling on
// the length so one drifting repository cannot bury every other doctor line.
func summariseSkillDrift(detail string) string {
	if detail == "" {
		return "(no output)"
	}
	lines := strings.Split(detail, "\n")
	const maxLines = 6
	if len(lines) > maxLines {
		omitted := len(lines) - maxLines
		lines = append(lines[:maxLines], fmt.Sprintf("… %d more line(s)", omitted))
	}
	return strings.Join(lines, "; ")
}
