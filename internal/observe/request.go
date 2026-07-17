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

	// SystemRoots are this host's machine/managed-scope locations (PR-16,
	// issue #20; system.go's SystemRoot doc comment). Empty skips
	// system-scope observation entirely — not an error, the same "caller
	// simply did not supply this" stance WorktreeRoot itself takes.
	SystemRoots []SystemRoot

	// WorkingDirectory is the absolute directory (normally the real
	// invocation's cwd) whose root-to-cwd nested "directory" scope chain
	// (docs/ontology/README.md §2) should be inventoried in addition to
	// WorktreeRoot itself (PR-16, issue #20; directory.go). Empty skips
	// directory-chain observation entirely. Non-empty requires WorktreeRoot
	// to also be set and WorkingDirectory to be WorktreeRoot itself or a
	// descendant of it — anything else is a caller error (directory.go's
	// directoryChain).
	WorkingDirectory string

	// SessionInputs are already-resolved session-scoped facts (CLI flags,
	// explicit env overrides) a caller supplies for Observe to inventory
	// alongside filesystem sources (PR-16, issue #20; session.go's
	// SessionInput doc comment, which explains why Observe cannot discover
	// these itself). Empty skips session-scope observation entirely.
	SessionInputs []SessionInput
}

// Observe inventories Instructions, Skills, MCP server registrations, Hooks,
// Policy/trust state, and Plugins/Extensions for req.Detection.Host across
// its native homes (user scope), its repository root and directory-chain
// (workspace/directory/local scope, if req.WorktreeRoot/WorkingDirectory are
// set), its machine/managed locations (system scope, if req.SystemRoots is
// set), and any caller-supplied session-scoped facts (req.SessionInputs),
// per docs/ontology/README.md §6.1/§6.2's physical mappings. It performs no
// writes and no subprocess execution (see doc.go and zerowrite_test.go) and
// returns a stably-sorted, deterministic slice: running Observe twice
// against an unchanged filesystem tree produces byte-identical output
// (determinism_test.go).
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

	worktreeExists := false
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
			worktreeExists = true

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

			// `local` scope (PR-16, issue #20): Claude Code only, see
			// claudeLocalRules's doc comment for why Codex has no
			// counterpart.
			if host == "claude-code" {
				recs, err := observeRoot(host, hostVersion, surface, "local", req.WorktreeRoot, claudeLocalRules())
				if err != nil {
					return nil, err
				}
				all = append(all, recs...)
			}
		}
	}

	// `system`/`managed` scope (PR-16, issue #20).
	for _, sysRoot := range req.SystemRoots {
		if sysRoot.Path == "" {
			continue
		}
		if !filepath.IsAbs(sysRoot.Path) {
			return nil, fmt.Errorf("observe: Observe: system root %q path %q is not absolute", sysRoot.Name, sysRoot.Path)
		}

		var rules []sourceRule
		switch host {
		case "codex":
			rules = codexSystemRules()
		case "claude-code":
			rules = claudeSystemRules()
		}
		if len(rules) == 0 {
			continue
		}

		info, err := os.Stat(sysRoot.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("observe: Observe: stat system root %s (%s): %w", sysRoot.Name, sysRoot.Path, err)
		}
		if !info.IsDir() {
			continue
		}

		recs, err := observeRoot(host, hostVersion, surface, "managed", sysRoot.Path, rules)
		if err != nil {
			return nil, err
		}
		all = append(all, recs...)
	}

	// `directory` scope chain (PR-16, issue #20).
	if req.WorkingDirectory != "" {
		if !filepath.IsAbs(req.WorkingDirectory) {
			return nil, fmt.Errorf("observe: Observe: WorkingDirectory %q is not absolute", req.WorkingDirectory)
		}
		if req.WorktreeRoot == "" {
			return nil, fmt.Errorf("observe: Observe: WorkingDirectory %q was set without WorktreeRoot", req.WorkingDirectory)
		}

		chain, err := directoryChain(req.WorktreeRoot, req.WorkingDirectory)
		if err != nil {
			return nil, fmt.Errorf("observe: Observe: %w", err)
		}

		if worktreeExists {
			var rules []sourceRule
			switch host {
			case "codex":
				rules = codexDirectoryChainRules()
			case "claude-code":
				rules = claudeDirectoryChainRules()
			}

				// tainted latches once any chain segment is found to be a
				// symlink, and every deeper segment is skipped unconditionally
				// from then on — checking each segment's own os.Lstat result
				// in isolation is not enough: os.Lstat only refuses to follow
				// a symlink in the *final* path element of the path it is
				// given, so a later segment built by filepath.Join'ing past an
				// already-symlinked earlier segment (directory.go's
				// directoryChain produces exactly this — "root/a" then
				// "root/a/b") still resolves cleanly through the OS's normal
				// intermediate-component symlink-following, lands on whatever
				// real directory the earlier symlink actually points at, and
				// would otherwise pass this loop's own per-segment check even
				// though the whole path is outside WorktreeRoot. Once tainted,
				// nothing under that prefix can be trusted to stay contained,
				// so skip it and everything deeper, not just the segment that
				// was directly a symlink.
				tainted := false
				for _, dir := range chain {
					if tainted {
						continue
					}
					// os.Lstat, not os.Stat: a chain segment that is itself a
					// symlink (e.g. a repo subdirectory symlinked to somewhere
					// outside WorktreeRoot) must never be followed into
					// observeRoot. os.Stat would resolve it and hand observeRoot
					// a path whose later filepath.Join/os.Lstat calls treat the
					// symlinked segment as an ordinary directory component —
					// silently widening observation outside the declared scope
					// root, the same scope-containment boundary observeFile
					// enforces for symlinked files (see walk.go). A symlinked
					// chain segment gets the same silent, non-error, non-record
					// treatment as a chain segment that doesn't exist or isn't
					// a directory: it is scaffolding for where to look, not
					// itself an observed concept, so there is nothing to lose
					// from "lossless inventory" by not walking through it.
					info, err := os.Lstat(dir)
					if err != nil {
						if os.IsNotExist(err) {
							continue
						}
						return nil, fmt.Errorf("observe: Observe: stat directory %s: %w", dir, err)
					}
					if info.Mode()&os.ModeSymlink != 0 {
						tainted = true
						continue
					}
					if !info.IsDir() {
						continue
					}

					recs, err := observeRoot(host, hostVersion, surface, "directory", dir, rules)
					if err != nil {
						return nil, err
					}
					all = append(all, recs...)
				}
		}
	}

	// `session` scope (PR-16, issue #20): caller-supplied facts, not a
	// filesystem walk — see session.go's SessionInput doc comment.
	for _, in := range req.SessionInputs {
		obs, err := buildSessionObservation(host, hostVersion, surface, in)
		if err != nil {
			return nil, fmt.Errorf("observe: Observe: %w", err)
		}
		all = append(all, obs)
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Metadata.ID < all[j].Metadata.ID
	})
	return all, nil
}
