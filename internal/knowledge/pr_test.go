package knowledge

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// This file is this project's own "never touch a real GitHub repository in
// a test" discipline, applied to PullRequestOpener/GitPublisher, PLUS the
// mechanical proof issue #33/PR-29's own "Automation cannot merge or
// promote capability levels (permission test)" acceptance criterion
// demands:
//
//   - CLIGitPublisher (the real GitPublisher implementation) IS exercised
//     for real in this file -- but only ever against a `git init --bare`
//     directory this file itself creates in t.TempDir()
//     (setupLocalGitRepoWithBareRemote), never a real network remote.
//   - GHCLIPullRequestOpener.OpenPullRequest (the real PullRequestOpener
//     implementation, which shells out to `gh pr create`) is NEVER called
//     by any test in this file or this package. Every test that needs a
//     PullRequestOpener uses fakePullRequestOpener instead. Its pure,
//     input-only argument-building helper (ghPRCreateArgs) IS tested
//     directly, without ever invoking `gh` itself.
//   - TestNoExecInvocationInKnowledgePackageEverPassesAMergeArgument proves
//     mechanically, via Go AST inspection (not a fragile text grep, which
//     would false-positive on this package's own doc comments explaining
//     why merging is forbidden), that no exec.Command/CommandContext call
//     anywhere in this package's production code ever passes a literal
//     "merge" argument.
//   - TestCandidateArtifactPath_NeverWritesUnderKnowledgeHosts and
//     TestProposeCandidatePR_EndToEnd_PushesOnlyTheCandidateArtifact prove,
//     respectively by construction and by inspecting a real (local) pushed
//     git commit, that automation only ever writes under
//     knowledge/candidates/, never knowledge/hosts/ (the actual published,
//     immutable Pack location).

// --- test doubles ---------------------------------------------------------

type fakePullRequestOpener struct {
	calls  []PullRequestRequest
	result PullRequestResult
	err    error
}

func (f *fakePullRequestOpener) OpenPullRequest(_ context.Context, req PullRequestRequest) (PullRequestResult, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return PullRequestResult{}, f.err
	}
	if f.result == (PullRequestResult{}) {
		return PullRequestResult{URL: "https://example.invalid/fake/pull/1"}, nil
	}
	return f.result, nil
}

// --- local git test plumbing ----------------------------------------------

func requireRealGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; skipping a test that needs a real (local-only) git repository")
	}
}

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out.String())
	}
	return out.String()
}

// setupLocalGitRepoWithBareRemote builds, entirely inside t.TempDir(): a
// normal git working repository (repoDir) with one commit on branch "main",
// and a `git init --bare` directory (bareDir) added to repoDir as remote
// remoteName. Every git operation here is 100% local filesystem -- no
// network, no real GitHub host, ever.
func setupLocalGitRepoWithBareRemote(t *testing.T) (repoDir, remoteName, baseRef, bareDir string) {
	t.Helper()
	requireRealGit(t)

	root := t.TempDir()
	repoDir = filepath.Join(root, "repo")
	bareDir = filepath.Join(root, "remote.git")
	remoteName = "test-origin"
	baseRef = "main"

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repoDir: %v", err)
	}
	runTestGit(t, root, "init", "--bare", bareDir)
	runTestGit(t, repoDir, "init", "-b", baseRef)
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runTestGit(t, repoDir, "add", "README.md")
	runTestGit(t, repoDir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runTestGit(t, repoDir, "remote", "add", remoteName, bareDir)
	runTestGit(t, repoDir, "push", remoteName, baseRef)

	return repoDir, remoteName, baseRef, bareDir
}

// filesChangedOnPushedBranch fetches remoteName into repoDir and returns the
// list of repo-relative paths that differ between baseRef and the pushed
// branch -- i.e. exactly what that one candidate-PR commit actually wrote,
// verified by inspecting the real (local) pushed git history rather than
// trusting the production code's own claim of what it wrote.
func filesChangedOnPushedBranch(t *testing.T, repoDir, remoteName, baseRef, branch string) []string {
	t.Helper()
	runTestGit(t, repoDir, "fetch", remoteName)
	remoteRef := "refs/remotes/" + remoteName + "/" + branch
	out := runTestGit(t, repoDir, "diff", "--name-only", baseRef, remoteRef)
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	sort.Strings(files)
	return files
}

// --- CLIGitPublisher: real git mechanics, local-only remote ---------------

func TestCLIGitPublisher_PublishBranch_PushesToLocalBareRepo(t *testing.T) {
	repoDir, remoteName, baseRef, _ := setupLocalGitRepoWithBareRemote(t)

	origBranch := strings.TrimSpace(runTestGit(t, repoDir, "symbolic-ref", "--short", "HEAD"))

	pub := CLIGitPublisher{}
	req := BranchPublishRequest{
		RepoDir:       repoDir,
		RemoteName:    remoteName,
		BaseRef:       baseRef,
		Branch:        "knowledge-candidate/codex-test",
		Files:         map[string]string{"knowledge/candidates/codex/c1.json": "{\"ok\":true}\n"},
		CommitMessage: "knowledge candidate: codex/cli (collected t, automation test)",
		AuthorName:    "OMCA Bot",
		AuthorEmail:   "omca-bot@example.invalid",
	}
	res, err := pub.PublishBranch(context.Background(), req)
	if err != nil {
		t.Fatalf("PublishBranch: %v", err)
	}
	if res.Branch != req.Branch || res.Remote != remoteName {
		t.Errorf("PublishBranch result = %+v, want branch=%q remote=%q", res, req.Branch, remoteName)
	}

	changed := filesChangedOnPushedBranch(t, repoDir, remoteName, baseRef, req.Branch)
	if len(changed) != 1 || changed[0] != "knowledge/candidates/codex/c1.json" {
		t.Fatalf("pushed branch changed files = %v, want exactly [knowledge/candidates/codex/c1.json]", changed)
	}

	// The caller's own checked-out branch in repoDir must be left exactly
	// as it was -- PublishBranch operates via a separate `git worktree add`,
	// never `git checkout` in repoDir itself.
	nowBranch := strings.TrimSpace(runTestGit(t, repoDir, "symbolic-ref", "--short", "HEAD"))
	if nowBranch != origBranch {
		t.Errorf("repoDir's checked-out branch changed from %q to %q -- PublishBranch must never disturb the caller's own working copy", origBranch, nowBranch)
	}
	if status := runTestGit(t, repoDir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Errorf("repoDir has uncommitted changes after PublishBranch: %q", status)
	}

	// The commit's author must be exactly what the request specified, not
	// whatever ambient git identity the test machine happens to have.
	author := runTestGit(t, repoDir, "log", "-1", "--format=%an <%ae>", "refs/remotes/"+remoteName+"/"+req.Branch)
	if want := "OMCA Bot <omca-bot@example.invalid>"; strings.TrimSpace(author) != want {
		t.Errorf("commit author = %q, want %q", strings.TrimSpace(author), want)
	}
}

func TestCLIGitPublisher_PublishBranch_RejectsFilesUnderKnowledgeHosts(t *testing.T) {
	repoDir, remoteName, baseRef, _ := setupLocalGitRepoWithBareRemote(t)

	pub := CLIGitPublisher{}
	req := BranchPublishRequest{
		RepoDir:       repoDir,
		RemoteName:    remoteName,
		BaseRef:       baseRef,
		Branch:        "should-never-be-pushed",
		Files:         map[string]string{"knowledge/hosts/codex/cli/9.9/manifest.json": "{}"},
		CommitMessage: "must not happen",
		AuthorName:    "OMCA Bot",
		AuthorEmail:   "omca-bot@example.invalid",
	}
	if _, err := pub.PublishBranch(context.Background(), req); err == nil {
		t.Fatal("PublishBranch: want an error when a Files path is under knowledge/hosts/, got nil")
	}

	// Prove this structurally, not just via the returned error: no such
	// branch was ever created or pushed to the (local, throwaway) remote.
	out := runTestGit(t, repoDir, "ls-remote", remoteName)
	if strings.Contains(out, req.Branch) {
		t.Fatalf("remote branches = %q, want branch %q to never have been pushed", out, req.Branch)
	}
}

// TestCLIGitPublisher_PublishBranch_RejectsPathTraversal is a regression
// test (Copilot review finding on this PR): the prior check only rejected
// the specific "knowledge/hosts/" prefix, never a general unsafe path -- a
// "../"-escaping key reaches filepath.Join(worktreeDir,
// filepath.FromSlash(p)) unchecked, and the resulting path is written to
// via os.WriteFile BEFORE the subsequent `git add` step (which does reject
// a path outside the git worktree) ever runs. That means the OLD code's
// own non-nil error return does NOT actually prove nothing escaped: a
// throwaway proof-of-concept file at a real, attacker-chosen path outside
// the worktree was already written to disk by the time `git add` finally
// failed and surfaced an error -- confirmed empirically while writing this
// fix (the file genuinely appeared on disk with the reverted code, despite
// PublishBranch still returning a non-nil error). The real security
// property this test must check is therefore the absence of that escaped
// file on disk, not merely a non-nil error.
func TestCLIGitPublisher_PublishBranch_RejectsPathTraversal(t *testing.T) {
	escapeTarget := filepath.Join(t.TempDir(), "omca-path-traversal-poc")
	// filepath.Join(worktreeDir, "../../../../../../"+escapeTarget) is how
	// many "../" segments it takes to walk from a fresh os.MkdirTemp
	// worktree back up to "/" is implementation-detail-dependent, so this
	// test computes a traversal string relative to escapeTarget's own
	// absolute path instead of hardcoding a fixed number of "../" segments.
	traversal := strings.Repeat("../", 20) + strings.TrimPrefix(escapeTarget, string(filepath.Separator))

	repoDir, remoteName, baseRef, _ := setupLocalGitRepoWithBareRemote(t)

	pub := CLIGitPublisher{}
	req := BranchPublishRequest{
		RepoDir:       repoDir,
		RemoteName:    remoteName,
		BaseRef:       baseRef,
		Branch:        "should-never-be-pushed",
		Files:         map[string]string{traversal: "malicious content"},
		CommitMessage: "must not happen",
		AuthorName:    "OMCA Bot",
		AuthorEmail:   "omca-bot@example.invalid",
	}
	if _, err := pub.PublishBranch(context.Background(), req); err == nil {
		t.Fatalf("PublishBranch with Files key %q: want an error, got nil", traversal)
	}
	if _, statErr := os.Stat(escapeTarget); statErr == nil {
		t.Fatalf("%s exists -- PublishBranch wrote a file outside the worktree it owns, the actual path-traversal vulnerability (a non-nil error alone does not prove this didn't happen: the write can succeed before a later step, e.g. `git add`, fails)", escapeTarget)
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("stat %s: %v", escapeTarget, statErr)
	}
	out := runTestGit(t, repoDir, "ls-remote", remoteName)
	if strings.Contains(out, req.Branch) {
		t.Fatalf("remote branches = %q, want branch %q to never have been pushed", out, req.Branch)
	}
}

func TestCLIGitPublisher_PublishBranch_RequiresNonEmptyFields(t *testing.T) {
	valid := BranchPublishRequest{
		RepoDir: "x", RemoteName: "origin", BaseRef: "main", Branch: "b",
		Files: map[string]string{"a": "b"}, AuthorName: "n", AuthorEmail: "e@example.com",
	}
	cases := []func(BranchPublishRequest) BranchPublishRequest{
		func(r BranchPublishRequest) BranchPublishRequest { r.RepoDir = ""; return r },
		func(r BranchPublishRequest) BranchPublishRequest { r.RemoteName = ""; return r },
		func(r BranchPublishRequest) BranchPublishRequest { r.BaseRef = ""; return r },
		func(r BranchPublishRequest) BranchPublishRequest { r.Branch = ""; return r },
		func(r BranchPublishRequest) BranchPublishRequest { r.Files = nil; return r },
		func(r BranchPublishRequest) BranchPublishRequest { r.AuthorName = ""; return r },
		func(r BranchPublishRequest) BranchPublishRequest { r.AuthorEmail = ""; return r },
	}
	for i, mutate := range cases {
		if err := validateBranchPublishRequest(mutate(valid)); err == nil {
			t.Errorf("case %d: validateBranchPublishRequest: want an error", i)
		}
	}
}

func TestValidatePullRequestRequest(t *testing.T) {
	valid := PullRequestRequest{Branch: "b", BaseBranch: "main", Title: "t", Body: "d"}
	if err := validatePullRequestRequest(valid); err != nil {
		t.Errorf("validatePullRequestRequest(valid) = %v, want nil", err)
	}
	cases := []func(PullRequestRequest) PullRequestRequest{
		func(r PullRequestRequest) PullRequestRequest { r.Branch = ""; return r },
		func(r PullRequestRequest) PullRequestRequest { r.BaseBranch = ""; return r },
		func(r PullRequestRequest) PullRequestRequest { r.Title = ""; return r },
	}
	for i, mutate := range cases {
		if err := validatePullRequestRequest(mutate(valid)); err == nil {
			t.Errorf("case %d: validatePullRequestRequest: want an error", i)
		}
	}
}

// --- GHCLIPullRequestOpener: pure argument-building proof, gh never run --

func TestGHCLIPRCreateArgs_AlwaysPRCreate_NeverMerge(t *testing.T) {
	adversarial := []PullRequestRequest{
		{Branch: "b", BaseBranch: "main", Title: "t", Body: "d"},
		{Branch: "merge", BaseBranch: "merge", Title: "please merge this", Body: "merge merge merge"},
		{Branch: "pr-merge-attempt", BaseBranch: "main", Title: "gh pr merge", Body: "gh pr merge --auto"},
	}
	for i, req := range adversarial {
		args := ghPRCreateArgs(req)
		if len(args) < 2 || args[0] != "pr" || args[1] != "create" {
			t.Fatalf("case %d: ghPRCreateArgs(%+v) = %v, want it to start with [\"pr\", \"create\"] regardless of input", i, req, args)
		}
	}
}

// --- Mechanical (AST-based) proof: no exec invocation anywhere in this
// package's production code ever passes a literal "merge" argument. AST
// inspection rather than a text grep: this package's own doc comments
// legitimately discuss "must never call gh pr merge" in prose, which a
// naive substring grep for "merge" would false-positive on; parsing real Go
// syntax and looking only at actual string-literal arguments to an
// exec.Command/exec.CommandContext call sidesteps that entirely.

func TestNoExecInvocationInKnowledgePackageEverPassesAMergeArgument(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}

	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		full := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, full, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", full, err)
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
				return true
			}
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(val), "merge") {
					t.Errorf("%s: found an exec.Command/CommandContext call with a literal %q argument -- automation in this package must never construct a merge subcommand", full, val)
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no production .go files were checked -- test premise broken")
	}
}

// --- candidateArtifactPath / branch naming ---------------------------------

func sampleCandidate(id, host string) domain.KnowledgeCandidate {
	return domain.KnowledgeCandidate{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "KnowledgeCandidate",
		Metadata: domain.KnowledgeCandidateMetadata{
			ID: id, Host: host, Surface: "cli", CollectedAt: "2026-07-19T00:00:00Z", Automation: "omca knowledge propose",
		},
		Spec: domain.KnowledgeCandidateSpec{
			ChangedSources: []domain.ChangedSource{{SourceID: "src1", NewDigest: "sha256:abc"}},
			AffectedCapabilities: []domain.AffectedCapability{
				{Concept: "skill", Operation: "resolve", Old: domain.CapabilityExact},
			},
			FixtureResults: []domain.FixtureResult{
				{ID: "pkg/a", Status: domain.FixtureResultNotRun},
			},
		},
	}
}

func TestCandidateArtifactPath_NeverWritesUnderKnowledgeHosts(t *testing.T) {
	candidates := []domain.KnowledgeCandidate{
		sampleCandidate("candidate:codex:cli:2026-07-19T00:00:00Z", "codex"),
		sampleCandidate("candidate:claude-code:cli:2026-01-01T00:00:00Z", "claude-code"),
	}
	for _, c := range candidates {
		p := candidateArtifactPath(c)
		if strings.HasPrefix(p, "knowledge/hosts/") || p == "knowledge/hosts" {
			t.Errorf("candidateArtifactPath(%q) = %q, must never be under knowledge/hosts/", c.Metadata.ID, p)
		}
		if !strings.HasPrefix(p, "knowledge/candidates/") {
			t.Errorf("candidateArtifactPath(%q) = %q, want it under knowledge/candidates/", c.Metadata.ID, p)
		}
	}
}

// --- ProposeCandidatePR: end-to-end, local git remote + fake opener ------

func TestProposeCandidatePR_EndToEnd_PushesOnlyTheCandidateArtifact(t *testing.T) {
	repoDir, remoteName, baseRef, _ := setupLocalGitRepoWithBareRemote(t)
	candidate := sampleCandidate("candidate:codex:cli:2026-07-19T00:00:00Z", "codex")

	cfg := ProposeConfig{RepoDir: repoDir, RemoteName: remoteName, BaseRef: baseRef, AuthorName: "OMCA Bot", AuthorEmail: "omca-bot@example.invalid"}
	publisher := CLIGitPublisher{}
	opener := &fakePullRequestOpener{}

	result, err := ProposeCandidatePR(context.Background(), candidate, cfg, publisher, opener)
	if err != nil {
		t.Fatalf("ProposeCandidatePR: %v", err)
	}
	if result.URL == "" {
		t.Error("ProposeCandidatePR result.URL is empty")
	}

	if len(opener.calls) != 1 {
		t.Fatalf("PullRequestOpener.OpenPullRequest calls = %d, want 1", len(opener.calls))
	}
	req := opener.calls[0]
	branch := candidateBranchName(candidate)
	if req.Branch != branch {
		t.Errorf("PullRequestRequest.Branch = %q, want %q", req.Branch, branch)
	}
	if req.BaseBranch != baseRef {
		t.Errorf("PullRequestRequest.BaseBranch = %q, want %q", req.BaseBranch, baseRef)
	}
	if !strings.Contains(req.Title, candidate.Metadata.Host) || !strings.Contains(req.Title, candidate.Metadata.CollectedAt) {
		t.Errorf("PullRequestRequest.Title = %q, want it to identify host and collection time (docs/knowledge/README.md §12)", req.Title)
	}
	if !strings.Contains(req.Body, candidate.Metadata.Automation) {
		t.Errorf("PullRequestRequest.Body = %q, want it to identify the automation (docs/knowledge/README.md §12)", req.Body)
	}
	if !strings.Contains(req.Body, "does not merge itself") {
		t.Errorf("PullRequestRequest.Body = %q, want an explicit no-merge/no-promote statement (AC3)", req.Body)
	}

	// Prove, by inspecting the real (local) pushed git commit -- not merely
	// by trusting production code's own claim -- that the ONLY file this
	// automation ever wrote is the candidate's own JSON artifact, never
	// anything under knowledge/hosts/.
	changed := filesChangedOnPushedBranch(t, repoDir, remoteName, baseRef, branch)
	want := candidateArtifactPath(candidate)
	if len(changed) != 1 || changed[0] != want {
		t.Fatalf("pushed branch changed files = %v, want exactly [%s]", changed, want)
	}
}

func TestProposeCandidatePR_InvalidCandidate_Errors(t *testing.T) {
	invalid := domain.KnowledgeCandidate{} // missing everything ValidateKnowledgeCandidate requires
	_, err := ProposeCandidatePR(context.Background(), invalid, ProposeConfig{}, CLIGitPublisher{}, &fakePullRequestOpener{})
	if err == nil {
		t.Fatal("ProposeCandidatePR: want an error for an invalid candidate, got nil")
	}
}

// TestProposeCandidatePR_NeverAltersAffectedCapabilitiesOrFixtureResultsShape
// is this issue's own "cannot ... promote capability levels" acceptance
// criterion, proven directly against ProposeCandidatePR (the code this PR
// adds): opening a pull request must never itself change what capability
// level a candidate reports -- AffectedCapabilities[].New (the field that
// would represent a promoted level) must come out exactly as it went in,
// for every entry, every time.
func TestProposeCandidatePR_NeverAltersAffectedCapabilitiesOrFixtureResultsShape(t *testing.T) {
	repoDir, remoteName, baseRef, _ := setupLocalGitRepoWithBareRemote(t)
	candidate := sampleCandidate("candidate:codex:cli:2026-07-19T00:00:00Z", "codex")
	beforeCaps := append([]domain.AffectedCapability(nil), candidate.Spec.AffectedCapabilities...)

	cfg := ProposeConfig{RepoDir: repoDir, RemoteName: remoteName, BaseRef: baseRef, AuthorName: "OMCA Bot", AuthorEmail: "omca-bot@example.invalid"}
	if _, err := ProposeCandidatePR(context.Background(), candidate, cfg, CLIGitPublisher{}, &fakePullRequestOpener{}); err != nil {
		t.Fatalf("ProposeCandidatePR: %v", err)
	}

	for i, ac := range beforeCaps {
		if ac.New != "" {
			t.Fatalf("test premise broken: fixture AffectedCapabilities[%d].New = %q, want empty", i, ac.New)
		}
	}
	for i, ac := range candidate.Spec.AffectedCapabilities {
		if ac.New != "" {
			t.Errorf("after ProposeCandidatePR, AffectedCapabilities[%d].New = %q, want still empty -- opening a pull request must never itself promote a capability level", i, ac.New)
		}
		if ac != beforeCaps[i] {
			t.Errorf("AffectedCapabilities[%d] changed from %+v to %+v", i, beforeCaps[i], ac)
		}
	}
}
