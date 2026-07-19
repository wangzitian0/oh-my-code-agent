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
