package observe

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// supportedHosts are the canonical host IDs this package implements
// observation for — the two first-party hosts named in issue #12's scope
// (codex, claude-code). This mirrors internal/context/host.go's
// binaryNames/DetectedHostIDs split: domain.KnownHostIDs is the wider,
// closed vocabulary of every host this project's ontology knows about,
// while this map is the much smaller set this package actually implements
// physical-mapping knowledge for.
var supportedHosts = map[string]bool{
	"codex":       true,
	"claude-code": true,
}

// Request specifies exactly what one Observe call should inventory.
// Locations come from internal/context, never re-derived here: Detection is
// normally internal/context.DetectHost's (or Detect's, per-host) direct
// result, and WorktreeRoot is normally internal/context.Worktree.Root from
// DetectWorktree — see doc.go's pipeline-position example. Observe never
// reads an environment variable, the real process environment, or any
// ambient state itself; every path it walks comes from this struct.
type Request struct {
	// Detection is one host's already-performed detection: its canonical
	// ID, version (copied into every resulting Observation's
	// spec.host.version), surface, and — the part this package actually
	// walks — NativeHomes. Detection.Installed/BinaryPath/Error are not
	// read: Observe inventories whatever exists on disk at the reported
	// native-home locations regardless of whether the host binary itself
	// is currently installed (a legitimate use: inspecting a copied-over
	// or backed-up home directory).
	Detection hostcontext.HostDetection

	// WorktreeRoot is the resolved, absolute root of the Git worktree whose
	// repository-scoped sources should be inventoried (typically
	// internal/context.Worktree.Root). Empty skips repository-scoped
	// observation entirely — not an error: observing only user-global
	// sources, or running outside any worktree, is a legitimate call.
	WorktreeRoot string
}

// Observe inventories Instructions, Skills, and MCP server registrations for
// req.Detection.Host across its native homes and (if req.WorktreeRoot is
// set) its repository root, per docs/ontology/README.md §6.1/§6.2's
// physical mappings. It performs no writes and no subprocess execution (see
// doc.go and zerowrite_test.go) and returns a stably-sorted, deterministic
// slice: running Observe twice against an unchanged filesystem tree
// produces byte-identical output (determinism_test.go).
//
// Every returned domain.Observation is already domain.ValidateObservation
// -clean; buildObservation enforces this per-record, so a caller does not
// need to re-validate. Every record's content is exactly what was read from
// disk, unredacted — a caller persisting or reporting the result MUST pass
// it through internal/domain/redact first (see doc.go's safety-properties
// point 4).
//
// A non-nil error means a genuine, unexpected failure (an unsupported host
// ID, a non-absolute path in req, or a filesystem error other than "does
// not exist" — e.g. a directory listing this package cannot even
// enumerate). A native home or the worktree root simply not existing on
// disk is not an error: it is silently skipped, the same "not found is a
// valid, non-fatal detection outcome" stance internal/context/host.go's
// DetectHost takes for "binary not installed."
func Observe(req Request) ([]domain.Observation, error) {
	host := req.Detection.Host
	if err := domain.ValidateHostID(host); err != nil {
		return nil, fmt.Errorf("observe: Observe: %w", err)
	}
	if !supportedHosts[host] {
		return nil, fmt.Errorf("observe: Observe: host %q is a known canonical host ID but this package does not implement observation for it (only codex, claude-code)", host)
	}

	surface := req.Detection.Surface
	if surface == "" {
		surface = "cli"
	}
	hostVersion := req.Detection.Version

	var all []domain.Observation

	for _, home := range req.Detection.NativeHomes {
		if home.Path == "" {
			continue
		}
		if !filepath.IsAbs(home.Path) {
			return nil, fmt.Errorf("observe: Observe: native home %q path %q is not absolute", home.Name, home.Path)
		}

		var rules []sourceRule
		switch host {
		case "codex":
			rules = codexUserRules(home.Name)
		case "claude-code":
			rules = claudeUserRules(home.Name)
		}
		if len(rules) == 0 {
			continue
		}

		info, err := os.Stat(home.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("observe: Observe: stat native home %s (%s): %w", home.Name, home.Path, err)
		}
		if !info.IsDir() {
			continue
		}

		recs, err := observeRoot(host, hostVersion, surface, "user", home.Path, rules)
		if err != nil {
			return nil, err
		}
		all = append(all, recs...)
	}

	if req.WorktreeRoot != "" {
		if !filepath.IsAbs(req.WorktreeRoot) {
			return nil, fmt.Errorf("observe: Observe: WorktreeRoot %q is not absolute", req.WorktreeRoot)
		}

		info, err := os.Stat(req.WorktreeRoot)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("observe: Observe: stat worktree root %s: %w", req.WorktreeRoot, err)
			}
		} else if info.IsDir() {
			var rules []sourceRule
			switch host {
			case "codex":
				rules = codexWorkspaceRules()
			case "claude-code":
				rules = claudeWorkspaceRules()
			}
			recs, err := observeRoot(host, hostVersion, surface, "workspace", req.WorktreeRoot, rules)
			if err != nil {
				return nil, err
			}
			all = append(all, recs...)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Metadata.ID < all[j].Metadata.ID
	})
	return all, nil
}
