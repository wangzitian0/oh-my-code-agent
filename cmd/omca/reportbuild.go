package main

import (
	stdcontext "context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/profiles"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// buildArtifactForCLI is the shared detect-observe-compose-Build pipeline
// every one of this PR's six new read commands (report, drift, explain,
// matrix, compare, diff) uses — one implementation of "gather this
// worktree's real state and hand it to report.Build," matching
// cmd/omca/env.go's runEnv/cmd/omca/activate.go's composeFreshCompileRequest
// own detect-then-observe pattern rather than each command re-deriving it.
//
// Every degradable failure (Knowledge Pack repository load, desired-state
// composition) is reported to stderr and the pipeline continues with an
// honest empty value, the same "degrade honestly, do not go silent on a
// working part of the pipeline" stance cmd/omca/context.go documents for its
// own Knowledge Pack resolution failure — a read-only reporting command
// should show as much real state as it can rather than hard-failing because
// one optional input (e.g. no Profiles configured yet) is unavailable. A
// worktree detection or host-observation failure is NOT degraded this way:
// those are the report's own primary subject matter, so a failure there
// fails the whole command closed.
func buildArtifactForCLI(stderr io.Writer, now time.Time) (report.Artifact, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return report.Artifact{}, err
	}
	wt, err := hostcontext.DetectWorktree(cwd)
	if err != nil {
		return report.Artifact{}, err
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		return report.Artifact{}, err
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)

	realEnv := hostcontext.RealEnvironment()
	detectEnv := envWithFilteredPath(realEnv, shimDir)

	hosts := make([]report.HostInput, 0, len(hostcontext.DetectedHostIDs))
	for _, host := range hostcontext.DetectedHostIDs {
		hd, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, host)
		if err != nil {
			return report.Artifact{}, fmt.Errorf("detecting %s: %w", host, err)
		}
		input := report.HostInput{Detection: hd}
		if hd.Installed {
			obs, err := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: wt.Root})
			if err != nil {
				return report.Artifact{}, fmt.Errorf("observing %s: %w", host, err)
			}
			input.Observations = obs
		}
		hosts = append(hosts, input)
	}

	repo, repoErr := knowledge.Default()
	if repoErr != nil {
		fmt.Fprintf(stderr, "omca: warning: Knowledge Pack repository failed to load, continuing without pack resolution: %v\n", repoErr)
	}

	req := report.BuildRequest{
		Worktree:         wt,
		WorktreeStateDir: worktreeStateDir,
		Hosts:            hosts,
		Repository:       repo,
		Now:              now,
	}
	if composed, ok := composeDesiredStateForCLI(stderr, wt, worktreeStateDir, now); ok {
		req.Profiles = composed.Profiles
		req.Activation = composed.Activation
		req.Exceptions = composed.Exceptions
	}

	return report.Build(req)
}

// composeDesiredStateForCLI runs internal/profiles.Compose over this
// machine's real config layout (mirroring cmd/omca/activate.go's
// composeFreshCompileRequest), returning ok == false — with a warning on
// stderr — for any error, including an unresolved identity ambiguity,
// rather than failing the whole report. A read-only report command has no
// way to prompt for an identity selection the way a future interactive flow
// would, so "no Desired Graph data this run" is the honest degrade.
func composeDesiredStateForCLI(stderr io.Writer, wt hostcontext.Worktree, worktreeStateDir string, now time.Time) (profiles.CompositionResult, bool) {
	configRoot, err := realConfigRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: warning: resolving config root, continuing without Desired Graph data: %v\n", err)
		return profiles.CompositionResult{}, false
	}
	profileDirs, bindingDirs, exceptionDirs := compositionDirsFor(configRoot, wt.Root)
	composed, err := profiles.Compose(profiles.CompositionInput{
		Repository:       wt.Root,
		ProfileDirs:      profileDirs,
		BindingDirs:      bindingDirs,
		ExceptionDirs:    exceptionDirs,
		ActivationPath:   filepath.Join(worktreeStateDir, "desired", "activation.yaml"),
		WorktreeStateDir: worktreeStateDir,
		Now:              now,
	})
	if err != nil {
		fmt.Fprintf(stderr, "omca: warning: composing desired state, continuing without Desired Graph data: %v\n", err)
		return profiles.CompositionResult{}, false
	}
	return composed, true
}
