package main

import (
	stdcontext "context"
	"fmt"
	"io"
	"strings"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
)

// runKnowledge dispatches `omca knowledge poll [--json]` (detection only)
// and `omca knowledge propose <host> [...]` (issue #33/PR-29: candidate PR
// automation) to their own thin handlers.
func runKnowledge(stdout, stderr io.Writer, args []string) int {
	const usage = "usage: omca knowledge poll [--json] | omca knowledge propose <host> [--repo-dir <dir>] [--base <branch>] [--remote <name>] [--json]"
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "poll":
		return runKnowledgePoll(stdout, stderr, args[1:])
	case "propose":
		return runKnowledgePropose(stdout, stderr, args[1:])
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
}

// runKnowledgePoll implements `omca knowledge poll [--json]`: for every host
// this build knows how to detect (hostcontext.DetectedHostIDs), it polls
// every allowlisted official source (internal/knowledge.OfficialSourcesForHost)
// via a real HTTPFetcher, comparing each against the digest the currently
// loaded Knowledge Pack repository recorded for it
// (internal/knowledge.PollHost), and prints one domain.KnowledgeCandidate
// per host that has a detected change.
//
// This is the update workflow's first step (docs/knowledge/README.md §8:
// "poll allowlisted official sources -> detect ... change -> create
// KnowledgeCandidate"); it stops there -- no repository pull request is
// opened here (`omca knowledge propose`, below, is issue #33/PR-29's job).
//
// This function itself is intentionally thin: it wires the one real,
// impure Fetcher (knowledge.HTTPFetcher, which makes real outbound HTTP
// requests) and the real on-disk Knowledge repository into
// pollAllHostsAndRender, which holds all the actual logic and is what this
// package's own tests exercise with a fake Fetcher instead -- so no
// automated test in this repository ever reaches a real HTTPFetcher call
// through this command (see internal/knowledge/poll_test.go's identical
// discipline for HTTPFetcher itself).
func runKnowledgePoll(stdout, stderr io.Writer, args []string) int {
	jsonOut, extra, err := parseJSONOnlyFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "omca: knowledge poll: %v\n", err)
		return 2
	}
	if len(extra) > 0 {
		fmt.Fprintf(stderr, "omca: knowledge poll: unrecognized argument %q\n", extra[0])
		return 2
	}

	repo, err := knowledge.Default()
	if err != nil {
		fmt.Fprintf(stderr, "omca: knowledge poll: loading Knowledge Pack repository: %v\n", err)
		return 1
	}

	return pollAllHostsAndRender(stdout, stderr, hostcontext.DetectedHostIDs, knowledge.HTTPFetcher{}, repo, jsonOut, time.Now())
}

// pollHostTimeout bounds how long pollAllHostsAndRender waits for one
// host's entire poll pass (every allowlisted source for that host). A real
// upstream source that hangs mid-response (or never completes a TCP
// handshake) would otherwise block `omca knowledge poll` indefinitely,
// since neither context.Background() nor http.DefaultClient impose any
// deadline of their own -- a real Copilot review finding on this PR. Scoped
// per host, not for the whole command, so one unresponsive host cannot
// also starve every other host's own poll from ever running or being
// reported.
const pollHostTimeout = 30 * time.Second

// pollAllHostsAndRender polls every host in hosts via fetcher against repo's
// currently loaded Packs, then renders every detected KnowledgeCandidate to
// stdout (JSON or human text per jsonOut). Separated from runKnowledge so a
// test can supply a fake, in-memory fetcher (never a real HTTPFetcher) and
// a small fixture repository instead of this machine's real Knowledge Pack
// repository and the real network.
func pollAllHostsAndRender(stdout, stderr io.Writer, hosts []string, fetcher knowledge.Fetcher, repo knowledge.Repository, jsonOut bool, now time.Time) int {
	var candidates []domain.KnowledgeCandidate
	for _, host := range hosts {
		var pack *knowledge.Pack
		if p, ok := knowledge.PackForHost(repo, host); ok {
			pack = &p
		}
		ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), pollHostTimeout)
		_, candidate, has, err := knowledge.PollHost(ctx, fetcher, host, pack, "omca knowledge poll", now)
		cancel()
		if err != nil {
			fmt.Fprintf(stderr, "omca: knowledge poll: host %q: %v\n", host, err)
			return 1
		}
		if has {
			candidates = append(candidates, candidate)
		}
	}

	if jsonOut {
		return writeJSON(stdout, stderr, candidates)
	}
	if len(candidates) == 0 {
		fmt.Fprintln(stdout, "omca: knowledge poll: no changes detected in any allowlisted official source")
		return 0
	}
	for _, c := range candidates {
		fmt.Fprintf(stdout, "Knowledge Candidate %s (host=%s surface=%s collectedAt=%s)\n", c.Metadata.ID, c.Metadata.Host, c.Metadata.Surface, c.Metadata.CollectedAt)
		for _, cs := range c.Spec.ChangedSources {
			fmt.Fprintf(stdout, "  changed source: %s (%s)\n    old digest: %s\n    new digest: %s\n", cs.SourceID, cs.URL, cs.OldDigest, cs.NewDigest)
		}
		fmt.Fprintf(stdout, "  version range: %s -> %s\n", c.Spec.VersionRange.Old, orPlaceholder(c.Spec.VersionRange.New, "(not yet determined; requires maintainer review)"))
		for _, w := range c.Spec.WriteCapabilityImpacts {
			fmt.Fprintf(stdout, "  write capability %s: %s (%s)\n", w.Concept, w.Change, w.Reason)
		}
		for _, u := range c.Spec.NewKnownUnknowns {
			fmt.Fprintf(stdout, "  known unknown: %s\n", u)
		}
	}
	return 0
}

func orPlaceholder(v, placeholder string) string {
	if v == "" {
		return placeholder
	}
	return v
}

// --- omca knowledge propose (issue #33/PR-29) -----------------------------

// proposeArgs is `omca knowledge propose`'s own parsed argument set.
// RepoDir doubles as the Go module root internal/knowledge.AffectedPackages
// and internal/knowledge.RunAffectedFixtures introspect: this repository's
// own go.mod lives at its repository root, the same directory a real
// `omca knowledge propose` invocation's git operations target.
type proposeArgs struct {
	Host        string
	RepoDir     string
	Base        string
	Remote      string
	AuthorName  string
	AuthorEmail string
	JSONOut     bool
}

// defaultProposeAuthorName/Email identify the automation's own git commits
// on a candidate's proposal branch -- distinct from whatever GitHub account
// the invoking human's own authenticated `gh` CLI session actually opens the
// pull request as (GHCLIPullRequestOpener never reads or overrides that;
// see internal/knowledge/pr.go's doc comment).
const (
	defaultProposeAuthorName  = "omca-bot"
	defaultProposeAuthorEmail = "omca-bot@users.noreply.github.com"
)

func parseProposeArgs(args []string) (proposeArgs, error) {
	out := proposeArgs{RepoDir: ".", Base: "main", Remote: "origin", AuthorName: defaultProposeAuthorName, AuthorEmail: defaultProposeAuthorEmail}
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func(flag string) (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", flag)
			}
			i++
			return args[i], nil
		}
		switch {
		case a == "--json":
			out.JSONOut = true
		case a == "--repo-dir":
			v, err := next(a)
			if err != nil {
				return proposeArgs{}, err
			}
			out.RepoDir = v
		case strings.HasPrefix(a, "--repo-dir="):
			out.RepoDir = strings.TrimPrefix(a, "--repo-dir=")
		case a == "--base":
			v, err := next(a)
			if err != nil {
				return proposeArgs{}, err
			}
			out.Base = v
		case strings.HasPrefix(a, "--base="):
			out.Base = strings.TrimPrefix(a, "--base=")
		case a == "--remote":
			v, err := next(a)
			if err != nil {
				return proposeArgs{}, err
			}
			out.Remote = v
		case strings.HasPrefix(a, "--remote="):
			out.Remote = strings.TrimPrefix(a, "--remote=")
		case out.Host == "" && !strings.HasPrefix(a, "-"):
			out.Host = a
		default:
			return proposeArgs{}, fmt.Errorf("unrecognized argument %q", a)
		}
	}
	if out.Host == "" {
		return proposeArgs{}, fmt.Errorf("a host is required: omca knowledge propose <host> [--repo-dir <dir>] [--base <branch>] [--remote <name>] [--json]")
	}
	return out, nil
}

// affectedFixturesFunc runs the qualification suite "affected" by one host's
// Knowledge change and returns its per-package results -- the seam
// runKnowledgePropose's real wiring (internal/knowledge.AffectedPackages +
// internal/knowledge.RunAffectedFixtures, a real `go list`/`go test`
// subprocess pair) and this package's own tests (a fast, fully in-memory
// fake) both implement, mirroring pollAllHostsAndRender's Fetcher seam.
type affectedFixturesFunc func(ctx stdcontext.Context, host string) ([]domain.FixtureResult, error)

// knowledgeProposeFixturesTimeout bounds how long runFixtures may run for
// one host before proposeCandidatePRForHost gives up -- affected fixtures
// are real `go test` runs (fixtures.go's runAffectedFixturesTimeout already
// bounds the `go test` subprocess itself; this is the same bound applied at
// this command's own call site so a caller-supplied fake or a future
// wiring mistake can't silently hang forever either).
const knowledgeProposeFixturesTimeout = 6 * time.Minute

// knowledgeProposePRTimeout bounds the git-publish-plus-PR-open step.
const knowledgeProposePRTimeout = 90 * time.Second

// runKnowledgePropose implements `omca knowledge propose <host> [...]`: it
// wires the one real Fetcher (knowledge.HTTPFetcher), the real on-disk
// Knowledge repository, the real affected-fixtures runner
// (internal/knowledge.AffectedPackages + RunAffectedFixtures against
// opts.RepoDir), and the two real, high-blast-radius implementations
// (knowledge.CLIGitPublisher, knowledge.GHCLIPullRequestOpener) into
// proposeCandidatePRForHost, which holds the actual logic and is what this
// package's own tests exercise with fakes for every one of those seams
// instead -- so no automated test in this repository ever pushes a branch
// to, or opens a pull request against, a real GitHub repository through
// this command (see internal/knowledge/pr_test.go's identical discipline
// for CLIGitPublisher/GHCLIPullRequestOpener themselves).
func runKnowledgePropose(stdout, stderr io.Writer, args []string) int {
	opts, err := parseProposeArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "omca: knowledge propose: %v\n", err)
		return 2
	}

	repo, err := knowledge.Default()
	if err != nil {
		fmt.Fprintf(stderr, "omca: knowledge propose: loading Knowledge Pack repository: %v\n", err)
		return 1
	}

	runFixtures := func(ctx stdcontext.Context, host string) ([]domain.FixtureResult, error) {
		packages, err := knowledge.AffectedPackages(ctx, opts.RepoDir, host)
		if err != nil {
			return nil, fmt.Errorf("determining affected packages: %w", err)
		}
		return knowledge.RunAffectedFixtures(ctx, opts.RepoDir, packages)
	}

	cfg := knowledge.ProposeConfig{
		RepoDir: opts.RepoDir, RemoteName: opts.Remote, BaseRef: opts.Base,
		AuthorName: opts.AuthorName, AuthorEmail: opts.AuthorEmail,
	}
	publisher := knowledge.CLIGitPublisher{}
	opener := knowledge.GHCLIPullRequestOpener{RepoDir: opts.RepoDir}

	return proposeCandidatePRForHost(stdout, stderr, opts.Host, knowledge.HTTPFetcher{}, repo, cfg, runFixtures, publisher, opener, time.Now(), opts.JSONOut)
}

// proposeCandidatePRResult is `omca knowledge propose --json`'s output
// shape: the full candidate report (with real FixtureResults populated) and
// the pull request that was opened for it.
type proposeCandidatePRResult struct {
	Candidate   domain.KnowledgeCandidate   `json:"candidate"`
	PullRequest knowledge.PullRequestResult `json:"pullRequest"`
}

// proposeCandidatePRForHost is `omca knowledge propose`'s real logic: poll
// host (knowledge.PollHost, exactly as `omca knowledge poll` does), and if a
// change was detected, run the affected qualification fixtures
// (runFixtures) to populate the candidate's own FixtureResults (superseding
// PollHost's NOT_RUN placeholders -- issue #33/PR-29's own job), then open
// a real pull request for it (knowledge.ProposeCandidatePR). Every
// dependency that could reach a real network, a real git remote, or a real
// GitHub repository is a parameter here, so this function's own tests can
// substitute a fake for every one of them.
func proposeCandidatePRForHost(stdout, stderr io.Writer, host string, fetcher knowledge.Fetcher, repo knowledge.Repository, cfg knowledge.ProposeConfig, runFixtures affectedFixturesFunc, publisher knowledge.GitPublisher, opener knowledge.PullRequestOpener, now time.Time, jsonOut bool) int {
	var pack *knowledge.Pack
	if p, ok := knowledge.PackForHost(repo, host); ok {
		pack = &p
	}

	pollCtx, cancel := stdcontext.WithTimeout(stdcontext.Background(), pollHostTimeout)
	_, candidate, has, err := knowledge.PollHost(pollCtx, fetcher, host, pack, "omca knowledge propose", now)
	cancel()
	if err != nil {
		fmt.Fprintf(stderr, "omca: knowledge propose: host %q: %v\n", host, err)
		return 1
	}
	if !has {
		fmt.Fprintf(stdout, "omca: knowledge propose: no changes detected for host %q; nothing to propose\n", host)
		return 0
	}

	fixturesCtx, cancelFixtures := stdcontext.WithTimeout(stdcontext.Background(), knowledgeProposeFixturesTimeout)
	results, err := runFixtures(fixturesCtx, host)
	cancelFixtures()
	if err != nil {
		fmt.Fprintf(stderr, "omca: knowledge propose: running affected fixtures for host %q: %v\n", host, err)
		return 1
	}
	candidate = knowledge.WithFixtureResults(candidate, results)

	prCtx, cancelPR := stdcontext.WithTimeout(stdcontext.Background(), knowledgeProposePRTimeout)
	result, err := knowledge.ProposeCandidatePR(prCtx, candidate, cfg, publisher, opener)
	cancelPR()
	if err != nil {
		fmt.Fprintf(stderr, "omca: knowledge propose: opening pull request for host %q: %v\n", host, err)
		return 1
	}

	if jsonOut {
		return writeJSON(stdout, stderr, proposeCandidatePRResult{Candidate: candidate, PullRequest: result})
	}
	fmt.Fprintf(stdout, "omca: knowledge propose: opened pull request %s for host %q\n", result.URL, host)
	for _, r := range results {
		fmt.Fprintf(stdout, "  fixture %s: %s\n", r.ID, r.Status)
	}
	return 0
}
