package main

import (
	stdcontext "context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
)

// contextReport is `omca context`'s stable JSON shape: worktree identity
// plus every first-party host's detection.
type contextReport struct {
	Worktree hostcontext.Worktree `json:"worktree"`
	Hosts    []contextHostReport  `json:"hosts"`
}

// contextHostReport promotes hostcontext.HostDetection's fields to the top
// level (Go's encoding/json flattens an anonymous embedded struct) and adds
// knowledgePack whenever resolution actually ran for that host — installed,
// a parseable version, and a loaded repository. knowledgePack.qualified is
// then either true (with packId/digest) or false with an explanatory
// reason (docs/knowledge/README.md §11: "No matching Pack means
// observation-only behavior for unresolved operations"); an unqualified
// result is still reported, not hidden, because the reason is itself
// useful diagnostic output for this command. knowledgePack is omitted
// entirely only when resolution could not run at all (not installed, no
// parseable version, or the Knowledge Pack repository itself failed to
// load) — that is the one case with nothing meaningful to report.
type contextHostReport struct {
	hostcontext.HostDetection
	KnowledgePack *knowledge.Resolution `json:"knowledgePack,omitempty"`
}

// runContext implements `omca context`: detect the worktree and every
// first-party host, then resolve each installed host's Knowledge Pack. A
// Knowledge Pack repository load failure does not fail the whole command —
// it is reported on stderr and the command still emits everything host
// detection itself determined, the same "degrade honestly, do not go silent
// on a working part of the pipeline" stance the rest of this package takes.
func runContext(stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "omca: context: %v\n", err)
		return 1
	}

	report, err := hostcontext.Detect(stdcontext.Background(), cwd, hostcontext.RealEnvironment())
	if err != nil {
		fmt.Fprintf(stderr, "omca: context: %v\n", err)
		return 1
	}

	repo, repoErr := knowledge.Default()
	if repoErr != nil {
		fmt.Fprintf(stderr, "omca: context: warning: Knowledge Pack repository failed to load, continuing without pack resolution: %v\n", repoErr)
	}

	out := contextReport{Worktree: report.Worktree, Hosts: make([]contextHostReport, 0, len(report.Hosts))}
	for _, hd := range report.Hosts {
		hr := contextHostReport{HostDetection: hd}
		if repoErr == nil && hd.Installed && hd.Version != "" {
			res := repo.Resolve(hd.Host, hd.Surface, hd.Version)
			hr.KnowledgePack = &res
		}
		out.Hosts = append(out.Hosts, hr)
	}

	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(stderr, "omca: context: %v\n", err)
		return 1
	}
	return 0
}
