package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
)

// fakeKnowledgeFetcher is an in-memory knowledge.Fetcher: it never touches
// the network. Every test in this file that exercises real polling logic
// uses this instead of knowledge.HTTPFetcher, so `go test` never makes a
// real HTTP request to a real allowlisted domain through this command --
// only runKnowledge's own arg-parsing error paths (which return before ever
// constructing a Fetcher) are exercised against the real entry point.
type fakeKnowledgeFetcher struct {
	content map[string][]byte
}

func (f fakeKnowledgeFetcher) Fetch(_ context.Context, s knowledge.Source) ([]byte, error) {
	if raw, ok := f.content[s.SourceID]; ok {
		return raw, nil
	}
	return nil, fmt.Errorf("fakeKnowledgeFetcher: no content stubbed for %q", s.SourceID)
}

func allSourcesFetcher(hosts ...string) fakeKnowledgeFetcher {
	content := map[string][]byte{}
	for _, h := range hosts {
		for _, s := range knowledge.OfficialSourcesForHost(h) {
			content[s.SourceID] = []byte("content-for-" + s.SourceID)
		}
	}
	return fakeKnowledgeFetcher{content: content}
}

// --- runKnowledge: usage/arg-parsing (no network reachable on these paths) -

func TestRunKnowledge_RequiresPollSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runKnowledge(&stdout, &stderr, nil); code != 2 {
		t.Fatalf("runKnowledge(nil) = %d, want 2", code)
	}
	if code := runKnowledge(&stdout, &stderr, []string{"bogus"}); code != 2 {
		t.Fatalf("runKnowledge(bogus) = %d, want 2", code)
	}
}

func TestRunKnowledge_RejectsUnrecognizedArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runKnowledge(&stdout, &stderr, []string{"poll", "--bogus"}); code != 2 {
		t.Fatalf("runKnowledge(poll --bogus) = %d, want 2; stderr=%s", code, stderr.String())
	}
}

// --- pollAllHostsAndRender: real logic, fake Fetcher only ------------------

func TestPollAllHostsAndRender_NoChanges_HumanOutput(t *testing.T) {
	fetcher := allSourcesFetcher("codex", "claude-code")
	pack := packWithMatchingEvidence(t, "codex", fetcher)
	repo := repoOf(t, pack)

	var stdout, stderr bytes.Buffer
	code := pollAllHostsAndRender(&stdout, &stderr, []string{"codex"}, fetcher, repo, false, time.Now())
	if code != 0 {
		t.Fatalf("pollAllHostsAndRender = %d; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no changes detected") {
		t.Errorf("stdout = %q, want a 'no changes detected' message", stdout.String())
	}
}

func TestPollAllHostsAndRender_NoBaselinePack_NoCandidate_JSONOutput(t *testing.T) {
	fetcher := allSourcesFetcher("codex")
	repo := knowledge.Repository{} // no Pack at all -- every source has no baseline

	var stdout, stderr bytes.Buffer
	code := pollAllHostsAndRender(&stdout, &stderr, []string{"codex"}, fetcher, repo, true, time.Now())
	if code != 0 {
		t.Fatalf("pollAllHostsAndRender = %d; stderr=%s", code, stderr.String())
	}
	var candidates []domain.KnowledgeCandidate
	if err := json.Unmarshal(stdout.Bytes(), &candidates); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput:\n%s", err, stdout.String())
	}
	// No Pack at all means every source has no baseline digest -- nothing
	// to diff, so no candidate (matches internal/knowledge/poll_test.go's
	// TestPollHost_NilPack_ChangedSourcesStillDetected expectation).
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want empty for a repository with no Pack at all", candidates)
	}
}

func TestPollAllHostsAndRender_RealDigestMismatch_ProducesCandidate(t *testing.T) {
	sources := knowledge.OfficialSourcesForHost("codex")
	fetcher := allSourcesFetcher("codex")

	evidence := make([]domain.KnowledgeEvidenceRef, 0, len(sources))
	for _, s := range sources {
		// Every recorded evidence digest is of DIFFERENT content than what
		// fetcher actually returns for this source, so every source is a
		// real, digest-computed mismatch -- not a hand-typed placeholder
		// string.
		evidence = append(evidence, domain.KnowledgeEvidenceRef{ID: s.SourceID, Digest: mustDigestBytes([]byte("stale-content-for-" + s.SourceID))})
	}
	pack := domain.HostKnowledge{
		APIVersion: domain.SupportedAPIVersion, Kind: "HostKnowledge",
		Metadata: domain.HostKnowledgeMetadata{ID: "codex:cli:test", Host: "codex", Surface: "cli", VersionRange: ">=1.0.0 <2.0.0", Status: domain.KnowledgeFresh},
		Evidence: evidence, Capabilities: map[string]domain.CapabilityOps{"skill": {Discover: domain.CapabilityExact}},
	}
	repo := repoOf(t, pack)

	var stdout, stderr bytes.Buffer
	code := pollAllHostsAndRender(&stdout, &stderr, []string{"codex"}, fetcher, repo, false, time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC))
	if code != 0 {
		t.Fatalf("pollAllHostsAndRender = %d; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Knowledge Candidate") {
		t.Errorf("stdout = %q, want a rendered Knowledge Candidate", out)
	}
	if !strings.Contains(out, "write capability") {
		t.Errorf("stdout = %q, want a rendered write capability impact line", out)
	}
}

func TestPollAllHostsAndRender_FetchError_ReturnsNonZero(t *testing.T) {
	fetcher := fakeKnowledgeFetcher{content: map[string][]byte{}} // nothing stubbed -> every fetch errors
	var stdout, stderr bytes.Buffer
	code := pollAllHostsAndRender(&stdout, &stderr, []string{"codex"}, fetcher, knowledge.Repository{}, false, time.Now())
	if code == 0 {
		t.Fatal("pollAllHostsAndRender: want a non-zero exit code when a fetch fails")
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty, want an error message naming the failing host")
	}
}

// --- test helpers -----------------------------------------------------

// packWithMatchingEvidence builds a domain.HostKnowledge for host recording,
// for every allowlisted source fetcher has content stubbed for, the exact
// digest of that stubbed content -- i.e. a Pack that is already fully
// up to date with what fetcher would return.
func packWithMatchingEvidence(t *testing.T, host string, fetcher fakeKnowledgeFetcher) domain.HostKnowledge {
	t.Helper()
	var evidence []domain.KnowledgeEvidenceRef
	for _, s := range knowledge.OfficialSourcesForHost(host) {
		content, ok := fetcher.content[s.SourceID]
		if !ok {
			t.Fatalf("packWithMatchingEvidence: fetcher has no content stubbed for %q", s.SourceID)
		}
		evidence = append(evidence, domain.KnowledgeEvidenceRef{ID: s.SourceID, Digest: mustDigestBytes(content)})
	}
	return domain.HostKnowledge{
		APIVersion: domain.SupportedAPIVersion, Kind: "HostKnowledge",
		Metadata: domain.HostKnowledgeMetadata{ID: host + ":cli:test", Host: host, Surface: "cli", VersionRange: ">=1.0.0 <2.0.0", Status: domain.KnowledgeFresh},
		Evidence: evidence, Capabilities: map[string]domain.CapabilityOps{"skill": {Discover: domain.CapabilityExact}},
	}
}

// mustDigestBytes reproduces internal/knowledge's own (unexported)
// digestBytes formula exactly -- "sha256:<hex>" of the raw content, no JSON
// canonicalization -- so a fixture Pack's recorded evidence digest actually
// matches what PollSource computes for the same content.
func mustDigestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// --- omca knowledge propose: dispatch, arg parsing, and real logic with
// every high-blast-radius seam (fetch, affected-fixtures runner, git
// publish, PR open) replaced by a fake -- no automated test in this file
// ever reaches a real HTTPFetcher, a real `go test`/`go list` subprocess, a
// real git remote, or a real `gh` CLI invocation (see
// internal/knowledge/pr_test.go for the same discipline one layer down, and
// internal/knowledge/fixtures_test.go for AffectedPackages/
// RunAffectedFixtures's own real-but-local-only proof).

// writeTinyPassingModule builds a minimal, valid Go module with exactly one
// trivial passing test -- just enough for RunAffectedFixtures' own real
// `go test ./...` fallback to have something quick and unambiguous to run
// against, for TestBuildRunFixtures_AffectedPackagesFails_FallsBackToFullSuite.
func writeTinyPassingModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("go.mod", "module omca-buildrunfixtures-test\n\ngo 1.22\n")
	write("onlypkg/onlypkg_test.go", "package onlypkg\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n")
	return root
}

// TestBuildRunFixtures_AffectedPackagesFails_FallsBackToFullSuite is a
// regression test (Copilot review finding on this PR): buildRunFixtures
// previously returned knowledge.AffectedPackages' own error immediately,
// aborting `omca knowledge propose` entirely, instead of falling back to
// RunAffectedFixtures' documented nil-packages "./..." behavior as both
// this file's own doc comment and the PR description already promised.
// knowledge.AffectedPackages errors on an empty host (a real, already-proven
// error condition -- TestAffectedPackages_EmptyHostOrModuleDir_Errors in
// internal/knowledge), which lets this test force exactly that failure
// against an otherwise perfectly valid module, proving the fallback
// actually runs (and succeeds) rather than the whole call failing.
func TestBuildRunFixtures_AffectedPackagesFails_FallsBackToFullSuite(t *testing.T) {
	moduleDir := writeTinyPassingModule(t)
	var stderr bytes.Buffer
	runFixtures := buildRunFixtures(&stderr, moduleDir)

	results, err := runFixtures(context.Background(), "" /* empty host forces AffectedPackages to error */)
	if err != nil {
		t.Fatalf("runFixtures with a forced AffectedPackages failure: want the full-suite fallback to succeed, got error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("runFixtures returned zero FixtureResults -- want at least the one package the fallback './...' run should have exercised")
	}
	if !strings.Contains(stderr.String(), "falling back to the full suite") {
		t.Errorf("stderr = %q, want it to explain the AffectedPackages failure and the fallback", stderr.String())
	}
}

// TestRunKnowledge_Propose_MissingHost_UsageError is a regression test
// (Copilot review finding on this PR): this test's name previously said
// "UnknownSubcommand," but "propose" IS a real, known subcommand -- what
// this actually exercises is that subcommand's own required-host argument
// validation, a materially different failure mode a future maintainer
// debugging a real unknown-subcommand report would be misled by.
func TestRunKnowledge_Propose_MissingHost_UsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runKnowledge(&stdout, &stderr, []string{"propose"}); code != 2 {
		t.Fatalf("runKnowledge(propose, no host) = %d, want 2; stderr=%s", code, stderr.String())
	}
}

func TestParseProposeArgs_RequiresHost(t *testing.T) {
	if _, err := parseProposeArgs(nil); err == nil {
		t.Fatal("parseProposeArgs(nil): want an error, host is required")
	}
}

func TestParseProposeArgs_DefaultsAndOverrides(t *testing.T) {
	out, err := parseProposeArgs([]string{"codex"})
	if err != nil {
		t.Fatalf("parseProposeArgs: %v", err)
	}
	if out.Host != "codex" || out.RepoDir != "." || out.Base != "main" || out.Remote != "origin" {
		t.Fatalf("parseProposeArgs(codex) = %+v, want the documented defaults", out)
	}

	out2, err := parseProposeArgs([]string{"claude-code", "--repo-dir=/tmp/repo", "--base", "release", "--remote=upstream", "--json"})
	if err != nil {
		t.Fatalf("parseProposeArgs: %v", err)
	}
	if out2.Host != "claude-code" || out2.RepoDir != "/tmp/repo" || out2.Base != "release" || out2.Remote != "upstream" || !out2.JSONOut {
		t.Fatalf("parseProposeArgs with overrides = %+v", out2)
	}
}

func TestParseProposeArgs_UnrecognizedArgument(t *testing.T) {
	if _, err := parseProposeArgs([]string{"codex", "--bogus"}); err == nil {
		t.Fatal("parseProposeArgs: want an error for an unrecognized flag")
	}
}

func TestParseProposeArgs_DanglingFlagValue(t *testing.T) {
	if _, err := parseProposeArgs([]string{"codex", "--base"}); err == nil {
		t.Fatal("parseProposeArgs: want an error when --base has no value")
	}
}

// fakeGitPublisher records every BranchPublishRequest it receives and never
// touches a real filesystem or git binary.
type fakeGitPublisher struct {
	calls []knowledge.BranchPublishRequest
	err   error
}

func (f *fakeGitPublisher) PublishBranch(_ context.Context, req knowledge.BranchPublishRequest) (knowledge.BranchPublishResult, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return knowledge.BranchPublishResult{}, f.err
	}
	return knowledge.BranchPublishResult{Branch: req.Branch, Remote: req.RemoteName}, nil
}

// fakePullRequestOpenerCLI records every PullRequestRequest it receives and
// never shells out to `gh` -- the CLI-layer equivalent of
// internal/knowledge/pr_test.go's own fakePullRequestOpener.
type fakePullRequestOpenerCLI struct {
	calls []knowledge.PullRequestRequest
	err   error
}

func (f *fakePullRequestOpenerCLI) OpenPullRequest(_ context.Context, req knowledge.PullRequestRequest) (knowledge.PullRequestResult, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return knowledge.PullRequestResult{}, f.err
	}
	return knowledge.PullRequestResult{URL: "https://example.invalid/fake/pull/42"}, nil
}

// digestMismatchRepoAndFetcher builds a fetcher stubbed with real content for
// every allowlisted source of host, plus a one-Pack repository whose
// recorded evidence digest for host's sources is of DIFFERENT content --
// mirrors TestPollAllHostsAndRender_RealDigestMismatch_ProducesCandidate's
// own construction, factored out so proposeCandidatePRForHost's tests can
// reuse it to force PollHost to report a real, digest-computed change.
func digestMismatchRepoAndFetcher(t *testing.T, host string) (knowledge.Repository, knowledge.Fetcher) {
	t.Helper()
	sources := knowledge.OfficialSourcesForHost(host)
	fetcher := allSourcesFetcher(host)
	evidence := make([]domain.KnowledgeEvidenceRef, 0, len(sources))
	for _, s := range sources {
		evidence = append(evidence, domain.KnowledgeEvidenceRef{ID: s.SourceID, Digest: mustDigestBytes([]byte("stale-content-for-" + s.SourceID))})
	}
	pack := domain.HostKnowledge{
		APIVersion: domain.SupportedAPIVersion, Kind: "HostKnowledge",
		Metadata: domain.HostKnowledgeMetadata{ID: host + ":cli:test", Host: host, Surface: "cli", VersionRange: ">=1.0.0 <2.0.0", Status: domain.KnowledgeFresh},
		Evidence: evidence, Capabilities: map[string]domain.CapabilityOps{"skill": {Discover: domain.CapabilityExact}},
	}
	return repoOf(t, pack), fetcher
}

func TestProposeCandidatePRForHost_NoChangeDetected_NeverTouchesGitOrPR(t *testing.T) {
	fetcher := allSourcesFetcher("codex")
	pack := packWithMatchingEvidence(t, "codex", fetcher)
	repo := repoOf(t, pack)

	fixturesCalled := false
	runFixtures := func(context.Context, string) ([]domain.FixtureResult, error) {
		fixturesCalled = true
		return nil, nil
	}
	git := &fakeGitPublisher{}
	pr := &fakePullRequestOpenerCLI{}

	var stdout, stderr bytes.Buffer
	code := proposeCandidatePRForHost(&stdout, &stderr, "codex", fetcher, repo, knowledge.ProposeConfig{}, runFixtures, git, pr, time.Now(), false)
	if code != 0 {
		t.Fatalf("proposeCandidatePRForHost = %d; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nothing to propose") {
		t.Errorf("stdout = %q, want a 'nothing to propose' message", stdout.String())
	}
	if fixturesCalled {
		t.Error("runFixtures was called even though no candidate was detected")
	}
	if len(git.calls) != 0 || len(pr.calls) != 0 {
		t.Fatalf("git.calls=%d pr.calls=%d, want 0 for both when no change was detected", len(git.calls), len(pr.calls))
	}
}

func TestProposeCandidatePRForHost_ChangeDetected_RunsFixturesAndOpensPR(t *testing.T) {
	repo, fetcher := digestMismatchRepoAndFetcher(t, "codex")

	var gotHost string
	runFixtures := func(_ context.Context, host string) ([]domain.FixtureResult, error) {
		gotHost = host
		return []domain.FixtureResult{{ID: "internal/knowledge", Status: domain.FixtureResultPass}}, nil
	}
	git := &fakeGitPublisher{}
	pr := &fakePullRequestOpenerCLI{}

	var stdout, stderr bytes.Buffer
	code := proposeCandidatePRForHost(&stdout, &stderr, "codex", fetcher, repo,
		knowledge.ProposeConfig{RepoDir: "/repo", RemoteName: "origin", BaseRef: "main", AuthorName: "n", AuthorEmail: "e@example.com"},
		runFixtures, git, pr, time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC), false)
	if code != 0 {
		t.Fatalf("proposeCandidatePRForHost = %d; stderr=%s", code, stderr.String())
	}
	if gotHost != "codex" {
		t.Errorf("runFixtures was called with host %q, want %q", gotHost, "codex")
	}
	if len(git.calls) != 1 {
		t.Fatalf("GitPublisher.PublishBranch calls = %d, want 1", len(git.calls))
	}
	if len(pr.calls) != 1 {
		t.Fatalf("PullRequestOpener.OpenPullRequest calls = %d, want 1", len(pr.calls))
	}
	if !strings.Contains(stdout.String(), "https://example.invalid/fake/pull/42") {
		t.Errorf("stdout = %q, want the opened pull request URL", stdout.String())
	}
	if !strings.Contains(stdout.String(), "internal/knowledge: PASS") {
		t.Errorf("stdout = %q, want the fixture result rendered", stdout.String())
	}
}

func TestProposeCandidatePRForHost_JSONOutput_IncludesRealFixtureResults(t *testing.T) {
	repo, fetcher := digestMismatchRepoAndFetcher(t, "codex")
	runFixtures := func(context.Context, string) ([]domain.FixtureResult, error) {
		return []domain.FixtureResult{{ID: "internal/knowledge", Status: domain.FixtureResultPass}, {ID: "internal/report", Status: domain.FixtureResultFail, Detail: "boom"}}, nil
	}
	git := &fakeGitPublisher{}
	pr := &fakePullRequestOpenerCLI{}

	var stdout, stderr bytes.Buffer
	code := proposeCandidatePRForHost(&stdout, &stderr, "codex", fetcher, repo,
		knowledge.ProposeConfig{RepoDir: "/repo", RemoteName: "origin", BaseRef: "main", AuthorName: "n", AuthorEmail: "e@example.com"},
		runFixtures, git, pr, time.Now(), true)
	if code != 0 {
		t.Fatalf("proposeCandidatePRForHost = %d; stderr=%s", code, stderr.String())
	}
	var out proposeCandidatePRResult
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput:\n%s", err, stdout.String())
	}
	if len(out.Candidate.Spec.FixtureResults) != 2 {
		t.Fatalf("Candidate.Spec.FixtureResults = %+v, want 2 entries", out.Candidate.Spec.FixtureResults)
	}
	if out.PullRequest.URL == "" {
		t.Error("PullRequest.URL is empty")
	}
}

func TestProposeCandidatePRForHost_FixturesError_NeverOpensAPR(t *testing.T) {
	repo, fetcher := digestMismatchRepoAndFetcher(t, "codex")
	runFixtures := func(context.Context, string) ([]domain.FixtureResult, error) {
		return nil, fmt.Errorf("go test exploded")
	}
	git := &fakeGitPublisher{}
	pr := &fakePullRequestOpenerCLI{}

	var stdout, stderr bytes.Buffer
	code := proposeCandidatePRForHost(&stdout, &stderr, "codex", fetcher, repo, knowledge.ProposeConfig{}, runFixtures, git, pr, time.Now(), false)
	if code != 1 {
		t.Fatalf("proposeCandidatePRForHost = %d, want 1", code)
	}
	if len(git.calls) != 0 || len(pr.calls) != 0 {
		t.Fatalf("git.calls=%d pr.calls=%d, want 0 for both when the fixture run itself errors", len(git.calls), len(pr.calls))
	}
}

func TestProposeCandidatePRForHost_PublishError_Propagates(t *testing.T) {
	repo, fetcher := digestMismatchRepoAndFetcher(t, "codex")
	runFixtures := func(context.Context, string) ([]domain.FixtureResult, error) {
		return []domain.FixtureResult{{ID: "pkg", Status: domain.FixtureResultPass}}, nil
	}
	git := &fakeGitPublisher{err: fmt.Errorf("push rejected")}
	pr := &fakePullRequestOpenerCLI{}

	var stdout, stderr bytes.Buffer
	code := proposeCandidatePRForHost(&stdout, &stderr, "codex", fetcher, repo, knowledge.ProposeConfig{}, runFixtures, git, pr, time.Now(), false)
	if code != 1 {
		t.Fatalf("proposeCandidatePRForHost = %d, want 1", code)
	}
	if len(pr.calls) != 0 {
		t.Fatalf("PullRequestOpener.OpenPullRequest calls = %d, want 0 when publishing the branch itself failed", len(pr.calls))
	}
}

// repoOf loads a one-Pack knowledge.Repository from a temp dir -- this
// package's own equivalent of internal/knowledge/repository_test.go's
// writePackDir/LoadRepository fixture pattern.
func repoOf(t *testing.T, hk domain.HostKnowledge) knowledge.Repository {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, hk.Metadata.Host, hk.Metadata.Surface, "fixture")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture Pack dir: %v", err)
	}
	raw, err := json.Marshal(hk)
	if err != nil {
		t.Fatalf("marshal fixture Pack: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, knowledge.PackFileName), raw, 0o644); err != nil {
		t.Fatalf("writing fixture Pack manifest: %v", err)
	}
	repo, err := knowledge.LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}
	return repo
}
