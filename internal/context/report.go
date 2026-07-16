package context

import (
	"context"
	"fmt"
)

// Report is the stable, JSON-serializable result of one full context
// detection pass: worktree identity plus every first-party host's
// detection, in DetectedHostIDs order (never map iteration order) so two
// Detect calls against identical inputs marshal to byte-identical JSON.
type Report struct {
	Worktree Worktree        `json:"worktree"`
	Hosts    []HostDetection `json:"hosts"`
}

// Detect resolves the worktree containing startDir and detects every host in
// DetectedHostIDs using env. It is the single entry point cmd/omca (and any
// future MCP/report projection) should call rather than composing
// DetectWorktree and DetectHost separately, so the two stay in lockstep.
func Detect(ctx context.Context, startDir string, env Environment) (Report, error) {
	wt, err := DetectWorktree(startDir)
	if err != nil {
		return Report{}, fmt.Errorf("context: Detect: %w", err)
	}

	hosts := make([]HostDetection, 0, len(DetectedHostIDs))
	for _, h := range DetectedHostIDs {
		hd, err := DetectHost(ctx, env, h)
		if err != nil {
			return Report{}, fmt.Errorf("context: Detect: %w", err)
		}
		hosts = append(hosts, hd)
	}

	return Report{Worktree: wt, Hosts: hosts}, nil
}
