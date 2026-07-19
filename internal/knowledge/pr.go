package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// This file implements issue #33/PR-29's "automation opens a repository PR
// per candidate" and "automation cannot merge or promote capability levels"
// acceptance criteria.
//
// # Safety discipline this file follows
//
// Opening a real pull request against a real GitHub repository is the
// highest-blast-radius action this project's automation has ever built: it
// writes to a real, shared, third-party system, visible to every
// collaborator, and cannot be silently un-done the way a failed HTTP GET
// (internal/knowledge.Fetcher's own risk) can be. The mechanism is therefore
// split into two small interfaces, mirroring Fetcher's own
// real-implementation-vs-fake split (fixtures.go/poll.go), so every
// automated test in this module can substitute a fake or a purely local git
// remote instead of ever reaching a real GitHub repository:
//
//   - GitPublisher commits candidate files to a new branch and pushes it to
//     a remote. Its real implementation, CLIGitPublisher, shells out to the
//     real `git` binary -- but every test in pr_test.go points RemoteName
//     at a `git init --bare` directory the test itself created in
//     t.TempDir(), so the real git push mechanics are genuinely exercised
//     without ever touching a real network or a real GitHub host.
//   - PullRequestOpener turns an already-pushed branch into a real pull
//     request. Its real implementation, GHCLIPullRequestOpener, shells out
//     to `gh pr create` -- the invoking human's own already-authenticated
//     `gh` CLI session, never a token or credential this code itself
//     handles. No automated test in this module ever invokes
//     GHCLIPullRequestOpener.OpenPullRequest; every test substitutes
//     fakePullRequestOpener instead (see pr_test.go). Proving this real
//     implementation genuinely works end-to-end against real GitHub is left
//     to a human maintainer -- see the PR-29 pull request description's
//     follow-up issue.
//
// ProposeCandidatePR is the orchestration these two interfaces compose
// into: it writes the candidate's own JSON report to
// knowledge/candidates/<host>/<id>.json (NEVER knowledge/hosts/**, the
// actual published, immutable Pack location ADR-0004 reserves for
// maintainer-reviewed publication only -- see
// TestCandidateArtifactPath_NeverWritesUnderKnowledgeHosts and
// TestProposeCandidatePR_PushedBranchNeverTouchesKnowledgeHosts), pushes
// that one file on a new branch, and opens a PR. Nothing in this file ever
// calls a merge operation -- see
// TestNoExecInvocationInKnowledgePackageEverPassesAMergeArgument and
// TestGHCLIPRCreateArgs_AlwaysPRCreate_NeverMerge in pr_test.go, which prove
// this mechanically rather than merely by inspection.

// --- PullRequestOpener -------------------------------------------------

// PullRequestRequest is everything one candidate pull request needs: which
// already-pushed branch to open against which base, and its title/body.
type PullRequestRequest struct {
	Branch     string
	BaseBranch string
	Title      string
	Body       string
}

// PullRequestResult is what opening a pull request produced.
type PullRequestResult struct {
	URL string
}

// PullRequestOpener opens one repository pull request for an
// already-pushed branch. See this file's doc comment for the real-vs-fake
// split every caller and every test must respect.
type PullRequestOpener interface {
	OpenPullRequest(ctx context.Context, req PullRequestRequest) (PullRequestResult, error)
}

// ghCLITimeout bounds one `gh pr create` invocation -- mirrors
// affectedPackagesTimeout's and internal/auth.invokeTimeout's identical "a
// hang can never be mistaken for success" discipline.
const ghCLITimeout = 30 * time.Second

// GHCLIPullRequestOpener is the real, production PullRequestOpener: it
// shells out to the real `gh` CLI's `pr create` subcommand in RepoDir,
// relying entirely on the invoking human's own already-authenticated `gh`
// session (this code never reads, stores, or transmits a GitHub token or
// credential itself). `gh pr create` is scoped to opening a pull request --
// it cannot merge, delete, or administer a repository; merging requires the
// entirely distinct `gh pr merge` subcommand, which no code path in this
// package ever constructs (see ghPRCreateArgs below and pr_test.go's
// TestGHCLIPRCreateArgs_AlwaysPRCreate_NeverMerge and
// TestNoExecInvocationInKnowledgePackageEverPassesAMergeArgument).
//
// No automated test in this module invokes OpenPullRequest: doing so would
// require either a real, authenticated `gh` CLI session or a real GitHub
// repository, exactly what this project's safety discipline for this PR
// forbids in any test. A human maintainer verifying this real path against
// a real (non-production, disposable) repository is the follow-up this PR's
// description tracks.
type GHCLIPullRequestOpener struct {
	// RepoDir is the local git working copy `gh` runs in -- it must be a
	// clone/checkout of the real target repository, with req.Branch already
	// pushed to its remote (GitPublisher's job, run first by
	// ProposeCandidatePR).
	RepoDir string
	// Env, if non-nil, is the exact environment `gh` runs with. Nil means
	// inherit the calling process's environment, which is what lets `gh`
	// find the invoking human's own existing authenticated session
	// (GH_TOKEN, gh's own config file, ...) -- there is no separate
	// automation identity to isolate here, unlike internal/auth.Invoke's
	// login-flow concern.
	Env []string
}

// ghPRCreateArgs builds the exact argv `gh` receives. Deliberately a small,
// pure, separately-testable function: args[0] and args[1] are always the
// literal constants "pr" and "create" below, never derived from req or any
// other input this package accepts from a candidate or a caller -- proven
// for adversarial req values (a Branch/Title/BaseBranch literally containing
// the word "merge") by
// TestGHCLIPRCreateArgs_AlwaysPRCreate_NeverMerge.
func ghPRCreateArgs(req PullRequestRequest) []string {
	return []string{
		"pr", "create",
		"--head", req.Branch,
		"--base", req.BaseBranch,
		"--title", req.Title,
		"--body", req.Body,
	}
}

// OpenPullRequest implements PullRequestOpener for real, via `gh pr create`.
func (g GHCLIPullRequestOpener) OpenPullRequest(ctx context.Context, req PullRequestRequest) (PullRequestResult, error) {
	if err := validatePullRequestRequest(req); err != nil {
		return PullRequestResult{}, fmt.Errorf("knowledge: GHCLIPullRequestOpener: %w", err)
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return PullRequestResult{}, fmt.Errorf("knowledge: GHCLIPullRequestOpener: `gh` CLI not found on PATH: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, ghCLITimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, ghPath, ghPRCreateArgs(req)...)
	cmd.Dir = g.RepoDir
	if g.Env != nil {
		cmd.Env = g.Env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return PullRequestResult{}, fmt.Errorf("knowledge: GHCLIPullRequestOpener: `gh pr create` in %s: %w (stderr: %s)", g.RepoDir, err, stderr.String())
	}
	// `gh pr create` prints the new PR's URL to stdout on success.
	return PullRequestResult{URL: strings.TrimSpace(stdout.String())}, nil
}

func validatePullRequestRequest(req PullRequestRequest) error {
	if strings.TrimSpace(req.Branch) == "" {
		return fmt.Errorf("PullRequestRequest.Branch is empty")
	}
	if strings.TrimSpace(req.BaseBranch) == "" {
		return fmt.Errorf("PullRequestRequest.BaseBranch is empty")
	}
	if strings.TrimSpace(req.Title) == "" {
		return fmt.Errorf("PullRequestRequest.Title is empty")
	}
	return nil
}

// --- GitPublisher -------------------------------------------------------

// BranchPublishRequest is one set of file writes to commit to a new branch
// and push to a remote.
type BranchPublishRequest struct {
	// RepoDir is an existing local git working copy (a real repository
	// checkout in production; a repository the test itself created with
	// `git init` in every automated test).
	RepoDir string
	// RemoteName is the git remote to push to, e.g. "origin" in production.
	// Every test in this package instead names a remote pointing at a local
	// `git init --bare` directory the test created.
	RemoteName string
	// BaseRef is the branch (or other committish) Branch is created from.
	BaseRef string
	// Branch is the new branch name to create and push.
	Branch string
	// Files maps a repo-relative slash path to its full new content.
	Files map[string]string
	// CommitMessage is the commit message for the one commit this publishes.
	CommitMessage string
	// AuthorName/AuthorEmail identify the commit author -- required so a
	// real push never silently falls back to whatever ambient git identity
	// (or lack of one) the calling process/environment happens to have.
	AuthorName  string
	AuthorEmail string
}

// BranchPublishResult is what publishing a branch produced.
type BranchPublishResult struct {
	Branch string
	Remote string
}

// GitPublisher creates a new branch off BaseRef, writes Files, commits, and
// pushes Branch to RemoteName. See this file's doc comment for the
// real-vs-local-test split.
type GitPublisher interface {
	PublishBranch(ctx context.Context, req BranchPublishRequest) (BranchPublishResult, error)
}

// gitOpTimeout bounds every individual `git` subprocess CLIGitPublisher
// runs. A push is allowed a little more time than a purely local operation
// since it may cross a real network in production.
const gitOpTimeout = 60 * time.Second

// CLIGitPublisher is the real, production GitPublisher: it shells out to
// the real `git` binary. It never touches the caller's actual checked-out
// branch -- it uses `git worktree add` to check out Branch into a fresh,
// separate temporary directory, so a real invocation running inside a
// human's own working checkout never changes what branch that checkout has
// open or disturbs any uncommitted change there.
type CLIGitPublisher struct {
	// Env, if non-nil, is the exact environment every `git` subprocess
	// receives. Nil means inherit the calling process's environment (the
	// production default: git needs the invoking human's own SSH/HTTPS
	// credential helpers to push for real).
	Env []string
}

func validateBranchPublishRequest(req BranchPublishRequest) error {
	switch {
	case strings.TrimSpace(req.RepoDir) == "":
		return fmt.Errorf("BranchPublishRequest.RepoDir is empty")
	case strings.TrimSpace(req.RemoteName) == "":
		return fmt.Errorf("BranchPublishRequest.RemoteName is empty")
	case strings.TrimSpace(req.BaseRef) == "":
		return fmt.Errorf("BranchPublishRequest.BaseRef is empty")
	case strings.TrimSpace(req.Branch) == "":
		return fmt.Errorf("BranchPublishRequest.Branch is empty")
	case len(req.Files) == 0:
		return fmt.Errorf("BranchPublishRequest.Files is empty -- refusing to open a pull request with no changes")
	case strings.TrimSpace(req.AuthorName) == "":
		return fmt.Errorf("BranchPublishRequest.AuthorName is empty")
	case strings.TrimSpace(req.AuthorEmail) == "":
		return fmt.Errorf("BranchPublishRequest.AuthorEmail is empty")
	}
	for p := range req.Files {
		if err := validateSafeRepoRelativePath(p); err != nil {
			return fmt.Errorf("BranchPublishRequest.Files key %q: %w", p, err)
		}
		if strings.HasPrefix(path.Clean(p), "knowledge/hosts/") || path.Clean(p) == "knowledge/hosts" {
			// Structural, load-bearing guard, not just documentation: even
			// if a future caller of this package ever tried to write a file
			// under the actual published-Pack location, PublishBranch
			// itself refuses rather than silently letting automation write
			// where only a maintainer-reviewed publish may (ADR-0004
			// decision 4). Proven end-to-end (not merely by this check
			// existing) in
			// TestProposeCandidatePR_PushedBranchNeverTouchesKnowledgeHosts.
			return fmt.Errorf("refusing to publish a file under knowledge/hosts/ (%q): automation may propose a candidate, it may never write the published Pack location directly (ADR-0004 decision 4)", p)
		}
	}
	return nil
}

// validateSafeRepoRelativePath rejects any BranchPublishRequest.Files key
// that is not a plain, repo-relative path fully contained within the
// worktree PublishBranch writes into: an absolute path, an empty path, or
// any path whose cleaned form is ".." or starts with "../" would otherwise
// reach filepath.Join(worktreeDir, filepath.FromSlash(p)) unchecked and
// resolve OUTSIDE that temporary worktree -- a real path-traversal
// vulnerability (a Copilot review finding on this PR): the prior check only
// rejected the specific "knowledge/hosts/" prefix, not the general "does
// this path even stay inside the directory PublishBranch owns" question.
func validateSafeRepoRelativePath(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("path is empty")
	}
	if path.IsAbs(p) || filepath.IsAbs(p) {
		return fmt.Errorf("path must be repo-relative, not absolute")
	}
	clean := path.Clean(filepath.ToSlash(p))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path escapes the repository root via '..'")
	}
	return nil
}

// PublishBranch implements GitPublisher for real, via `git worktree add` +
// commit + push.
func (g CLIGitPublisher) PublishBranch(ctx context.Context, req BranchPublishRequest) (BranchPublishResult, error) {
	if err := validateBranchPublishRequest(req); err != nil {
		return BranchPublishResult{}, fmt.Errorf("knowledge: CLIGitPublisher: %w", err)
	}

	worktreeDir, err := os.MkdirTemp("", "omca-knowledge-candidate-")
	if err != nil {
		return BranchPublishResult{}, fmt.Errorf("knowledge: CLIGitPublisher: creating temp worktree dir: %w", err)
	}
	defer os.RemoveAll(worktreeDir)

	if _, _, err := g.runGit(ctx, req.RepoDir, "worktree", "add", "-b", req.Branch, worktreeDir, req.BaseRef); err != nil {
		return BranchPublishResult{}, fmt.Errorf("knowledge: CLIGitPublisher: `git worktree add`: %w", err)
	}
	defer func() {
		_, _, _ = g.runGit(context.Background(), req.RepoDir, "worktree", "remove", "--force", worktreeDir)
	}()

	paths := make([]string, 0, len(req.Files))
	for p := range req.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		full := filepath.Join(worktreeDir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return BranchPublishResult{}, fmt.Errorf("knowledge: CLIGitPublisher: creating directory for %s: %w", p, err)
		}
		if err := os.WriteFile(full, []byte(req.Files[p]), 0o644); err != nil {
			return BranchPublishResult{}, fmt.Errorf("knowledge: CLIGitPublisher: writing %s: %w", p, err)
		}
		if _, _, err := g.runGit(ctx, worktreeDir, "add", "--", p); err != nil {
			return BranchPublishResult{}, fmt.Errorf("knowledge: CLIGitPublisher: `git add` %s: %w", p, err)
		}
	}

	// The commit author/committer identity is set via GIT_AUTHOR_*/
	// GIT_COMMITTER_* environment variables, not just `-c user.name=`/
	// `-c user.email=`: git environment variables take precedence over `-c`
	// config on the command line, so an ambient GIT_AUTHOR_EMAIL already set
	// in the calling process's environment (a real, observed case on the
	// developer's own machine during this PR's own testing) would otherwise
	// silently leak the invoking human's own identity into an
	// automation-authored commit -- exactly the kind of "which identity
	// actually made this write" ambiguity internal/auth.Invoke's own
	// explicit-environment discipline exists to prevent elsewhere in this
	// project, applied here to git instead of a host CLI.
	commitEnv := gitCommitEnv(g.effectiveEnv(), req.AuthorName, req.AuthorEmail)
	if _, _, err := g.runGitWithEnv(ctx, worktreeDir, commitEnv, "commit", "-m", req.CommitMessage); err != nil {
		return BranchPublishResult{}, fmt.Errorf("knowledge: CLIGitPublisher: `git commit`: %w", err)
	}

	if _, _, err := g.runGit(ctx, worktreeDir, "push", req.RemoteName, req.Branch); err != nil {
		return BranchPublishResult{}, fmt.Errorf("knowledge: CLIGitPublisher: `git push`: %w", err)
	}

	return BranchPublishResult{Branch: req.Branch, Remote: req.RemoteName}, nil
}

func (g CLIGitPublisher) runGit(ctx context.Context, dir string, args ...string) (stdout, stderr string, err error) {
	return g.runGitWithEnv(ctx, dir, g.Env, args...)
}

// effectiveEnv returns the environment every non-commit git subprocess runs
// with: g.Env if the caller set one, otherwise nil (os/exec's own "inherit
// the calling process's environment" default) -- the production default,
// since git needs the invoking human's own SSH/HTTPS credential helpers to
// push for real.
func (g CLIGitPublisher) effectiveEnv() []string {
	return g.Env
}

// gitCommitEnv returns a copy of base with any existing GIT_AUTHOR_*/
// GIT_COMMITTER_* entries removed and authoritative ones for authorName/
// authorEmail appended -- see PublishBranch's own comment on why this must
// be an environment override, not just `-c user.name=`/`-c user.email=`.
// base may be nil (meaning "inherit the calling process's real
// environment"); this function always returns a concrete, fully-specified
// slice so the returned env is never accidentally "inherit everything,
// including whatever ambient identity happens to be set."
func gitCommitEnv(base []string, authorName, authorEmail string) []string {
	src := base
	if src == nil {
		src = os.Environ()
	}
	out := make([]string, 0, len(src)+4)
	for _, kv := range src {
		switch {
		case strings.HasPrefix(kv, "GIT_AUTHOR_NAME="),
			strings.HasPrefix(kv, "GIT_AUTHOR_EMAIL="),
			strings.HasPrefix(kv, "GIT_COMMITTER_NAME="),
			strings.HasPrefix(kv, "GIT_COMMITTER_EMAIL="):
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		"GIT_AUTHOR_NAME="+authorName, "GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME="+authorName, "GIT_COMMITTER_EMAIL="+authorEmail,
	)
	return out
}

func (g CLIGitPublisher) runGitWithEnv(ctx context.Context, dir string, env []string, args ...string) (stdout, stderr string, err error) {
	runCtx, cancel := context.WithTimeout(ctx, gitOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	if runErr != nil {
		return outBuf.String(), errBuf.String(), fmt.Errorf("`git %s` in %s: %w (stderr: %s)", strings.Join(args, " "), dir, runErr, errBuf.String())
	}
	return outBuf.String(), errBuf.String(), nil
}

// --- Orchestration -------------------------------------------------------

// ProposeConfig configures ProposeCandidatePR's git mechanics. Every field
// must be chosen explicitly by the caller -- there is no default that could
// silently point at a real repository in a test.
type ProposeConfig struct {
	RepoDir     string
	RemoteName  string
	BaseRef     string
	AuthorName  string
	AuthorEmail string
}

// candidateArtifactPath returns the repo-relative slash path a Knowledge
// Candidate's own JSON report is written to on its proposal branch --
// always under knowledge/candidates/, NEVER under knowledge/hosts/ (see
// validateBranchPublishRequest's structural guard above and
// TestCandidateArtifactPath_NeverWritesUnderKnowledgeHosts).
func candidateArtifactPath(c domain.KnowledgeCandidate) string {
	safeID := candidateSlug(c.Metadata.ID)
	return path.Join("knowledge", "candidates", c.Metadata.Host, safeID+".json")
}

// candidateBranchName returns the branch name ProposeCandidatePR proposes
// one candidate's PR from.
func candidateBranchName(c domain.KnowledgeCandidate) string {
	return "knowledge-candidate/" + candidateSlug(c.Metadata.ID)
}

// candidateSlug turns a KnowledgeCandidate's own metadata.id (e.g.
// "candidate:codex:cli:2026-07-19T00:00:00Z") into a string safe to use as
// both a file name component and a git branch name component: git branch
// names and typical filesystems both reject ':' (and git additionally
// rejects a few other characters branch names commonly do not need), so
// every non-alphanumeric run collapses to a single '-'.
func candidateSlug(id string) string {
	var b strings.Builder
	lastWasDash := false
	for _, r := range id {
		safe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if safe {
			b.WriteRune(r)
			lastWasDash = false
			continue
		}
		if !lastWasDash {
			b.WriteByte('-')
			lastWasDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		// ValidateKnowledgeCandidate only enforces that Metadata.ID is
		// non-empty, not that it contains any alphanumeric character --
		// an id built entirely from symbols would otherwise slug down to
		// "", producing an ambiguous branch name
		// ("knowledge-candidate/") and a file path ending in "/.json" (a
		// real Copilot review finding on this PR). This fallback keeps
		// every branch/path this package builds non-empty and
		// unambiguous even for a pathological id.
		return "candidate"
	}
	return slug
}

// candidatePRTitle names automation and collection time in the pull
// request's own title, per docs/knowledge/README.md §12: "Generated
// candidates identify automation and collection time."
func candidatePRTitle(c domain.KnowledgeCandidate) string {
	return fmt.Sprintf("Knowledge candidate: %s/%s (collected %s)", c.Metadata.Host, c.Metadata.Surface, c.Metadata.CollectedAt)
}

// candidatePRBody renders the pull request description: automation
// identity + collection time (AC1), the changed sources, and the fixture
// results this PR's own RunAffectedFixtures populated (AC2) -- plus an
// explicit, unambiguous statement that this PR was opened by automation and
// requires maintainer review before any Pack publish or capability
// promotion (AC3 / ADR-0004 decision 4), so the statement is not only true
// of the code but visible to the human who reviews it.
func candidatePRBody(c domain.KnowledgeCandidate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Automated Knowledge Candidate opened by `%s`, collected at `%s`.\n\n", c.Metadata.Automation, c.Metadata.CollectedAt)
	b.WriteString("This pull request was opened by automation. It does not merge itself and it does not promote any capability level -- a maintainer must review the fixture results below (and the full candidate report attached to this PR) before publishing a new Knowledge Pack or changing any capability relation (docs/knowledge/README.md §8, §12; ADR-0004 decision 4).\n\n")
	fmt.Fprintf(&b, "Host: `%s`  Surface: `%s`\n\n", c.Metadata.Host, c.Metadata.Surface)

	b.WriteString("## Changed sources\n\n")
	for _, cs := range c.Spec.ChangedSources {
		fmt.Fprintf(&b, "- `%s` (%s): %s -> %s\n", cs.SourceID, cs.URL, orDash(cs.OldDigest), cs.NewDigest)
	}

	b.WriteString("\n## Fixture results\n\n")
	if len(c.Spec.FixtureResults) == 0 {
		b.WriteString("(none recorded)\n")
	}
	for _, fr := range c.Spec.FixtureResults {
		fmt.Fprintf(&b, "- `%s`: **%s**", fr.ID, fr.Status)
		if fr.Detail != "" {
			fmt.Fprintf(&b, " -- %s", fr.Detail)
		}
		b.WriteString("\n")
	}

	if len(c.Spec.WriteCapabilityImpacts) > 0 {
		b.WriteString("\n## Write capability impacts\n\n")
		for _, w := range c.Spec.WriteCapabilityImpacts {
			fmt.Fprintf(&b, "- `%s`: %s (%s)\n", w.Concept, w.Change, w.Reason)
		}
	}

	if len(c.Spec.NewKnownUnknowns) > 0 {
		b.WriteString("\n## New known unknowns\n\n")
		for _, u := range c.Spec.NewKnownUnknowns {
			fmt.Fprintf(&b, "- %s\n", u)
		}
	}

	fmt.Fprintf(&b, "\nFull candidate report: `%s`\n", candidateArtifactPath(c))
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "(none recorded)"
	}
	return s
}

// ProposeCandidatePR turns one already-built domain.KnowledgeCandidate
// (with FixtureResults already populated by RunAffectedFixtures) into a
// real branch push (via publisher) and a real pull request (via opener) --
// the "open repository pull request" step of docs/knowledge/README.md §8's
// update workflow. It never merges anything (see this file's doc comment)
// and never writes anywhere under knowledge/hosts/**
// (validateBranchPublishRequest's structural guard, proven end-to-end by
// TestProposeCandidatePR_PushedBranchNeverTouchesKnowledgeHosts).
func ProposeCandidatePR(ctx context.Context, candidate domain.KnowledgeCandidate, cfg ProposeConfig, publisher GitPublisher, opener PullRequestOpener) (PullRequestResult, error) {
	if err := domain.ValidateKnowledgeCandidate(candidate); err != nil {
		return PullRequestResult{}, fmt.Errorf("knowledge: ProposeCandidatePR: invalid candidate: %w", err)
	}
	if publisher == nil {
		return PullRequestResult{}, fmt.Errorf("knowledge: ProposeCandidatePR: publisher is nil")
	}
	if opener == nil {
		return PullRequestResult{}, fmt.Errorf("knowledge: ProposeCandidatePR: opener is nil")
	}

	raw, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return PullRequestResult{}, fmt.Errorf("knowledge: ProposeCandidatePR: marshaling candidate: %w", err)
	}
	artifactPath := candidateArtifactPath(candidate)
	branch := candidateBranchName(candidate)

	pubReq := BranchPublishRequest{
		RepoDir:       cfg.RepoDir,
		RemoteName:    cfg.RemoteName,
		BaseRef:       cfg.BaseRef,
		Branch:        branch,
		Files:         map[string]string{artifactPath: string(raw) + "\n"},
		CommitMessage: fmt.Sprintf("knowledge candidate: %s/%s (collected %s, automation %s)", candidate.Metadata.Host, candidate.Metadata.Surface, candidate.Metadata.CollectedAt, candidate.Metadata.Automation),
		AuthorName:    cfg.AuthorName,
		AuthorEmail:   cfg.AuthorEmail,
	}
	if _, err := publisher.PublishBranch(ctx, pubReq); err != nil {
		return PullRequestResult{}, fmt.Errorf("knowledge: ProposeCandidatePR: publishing branch: %w", err)
	}

	prReq := PullRequestRequest{
		Branch:     branch,
		BaseBranch: cfg.BaseRef,
		Title:      candidatePRTitle(candidate),
		Body:       candidatePRBody(candidate),
	}
	result, err := opener.OpenPullRequest(ctx, prReq)
	if err != nil {
		return PullRequestResult{}, fmt.Errorf("knowledge: ProposeCandidatePR: opening pull request: %w", err)
	}
	return result, nil
}
