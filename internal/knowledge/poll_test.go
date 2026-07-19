package knowledge

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Every test in this file either uses fakeFetcher (a pure in-memory stub) or
// an httptest.Server -- no test in this package ever makes a real request
// to a real external domain (see the safety note in doc.go and this
// package's PR description: "no automated test... may make a real HTTP
// request to a real allowlisted URL during `go test`").

// fakeFetcher is an in-memory Fetcher: it never touches the network at
// all, returning fixed content or an error per Source.SourceID.
type fakeFetcher struct {
	content map[string][]byte // keyed by SourceID
	err     map[string]error
	calls   []Source
}

func (f *fakeFetcher) Fetch(_ context.Context, s Source) ([]byte, error) {
	f.calls = append(f.calls, s)
	if err, ok := f.err[s.SourceID]; ok {
		return nil, err
	}
	if raw, ok := f.content[s.SourceID]; ok {
		return raw, nil
	}
	return nil, fmt.Errorf("fakeFetcher: no content stubbed for source %q", s.SourceID)
}

func testSource(host, sourceID string) Source {
	for _, s := range officialSources {
		if s.Host == host && s.SourceID == sourceID {
			return s
		}
	}
	panic(fmt.Sprintf("testSource: no allowlisted source %s/%s", host, sourceID))
}

func packWithEvidence(host, surface, versionRange string, evidence ...domain.KnowledgeEvidenceRef) *Pack {
	return &Pack{
		Knowledge: domain.HostKnowledge{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       "HostKnowledge",
			Metadata: domain.HostKnowledgeMetadata{
				ID: host + ":" + surface + ":test", Host: host, Surface: surface,
				VersionRange: versionRange, Status: domain.KnowledgeFresh,
			},
			Evidence:     evidence,
			Capabilities: map[string]domain.CapabilityOps{"skill": {Discover: domain.CapabilityExact}},
		},
	}
}

// --- PollSource --------------------------------------------------------

func TestPollSource_NoBaselineDigest_NotChanged(t *testing.T) {
	s := testSource("codex", "codex-cli-doc")
	fetcher := &fakeFetcher{content: map[string][]byte{s.SourceID: []byte("hello world")}}
	pack := packWithEvidence("codex", "cli", ">=0.144.0 <0.145.0", domain.KnowledgeEvidenceRef{ID: s.SourceID, Kind: "official-doc"}) // no Digest

	res, err := PollSource(context.Background(), fetcher, s, pack)
	if err != nil {
		t.Fatalf("PollSource: %v", err)
	}
	if res.Changed {
		t.Error("Changed = true, want false when the Pack recorded no baseline digest")
	}
	if res.NewDigest == "" {
		t.Error("NewDigest is empty, want a computed digest of the fetched content")
	}
}

func TestPollSource_MissingEvidenceEntry_NotChanged(t *testing.T) {
	s := testSource("codex", "codex-cli-doc")
	fetcher := &fakeFetcher{content: map[string][]byte{s.SourceID: []byte("hello world")}}
	pack := packWithEvidence("codex", "cli", ">=0.144.0 <0.145.0") // no evidence at all

	res, err := PollSource(context.Background(), fetcher, s, pack)
	if err != nil {
		t.Fatalf("PollSource: %v", err)
	}
	if res.Changed {
		t.Error("Changed = true, want false when the Pack records no evidence entry for this source at all")
	}
}

func TestPollSource_NilPack_NotChanged(t *testing.T) {
	s := testSource("codex", "codex-cli-doc")
	fetcher := &fakeFetcher{content: map[string][]byte{s.SourceID: []byte("hello world")}}

	res, err := PollSource(context.Background(), fetcher, s, nil)
	if err != nil {
		t.Fatalf("PollSource: %v", err)
	}
	if res.Changed {
		t.Error("Changed = true, want false for a nil Pack (nothing published yet)")
	}
}

func TestPollSource_DigestMatches_NotChanged(t *testing.T) {
	s := testSource("codex", "codex-cli-doc")
	content := []byte("stable content")
	fetcher := &fakeFetcher{content: map[string][]byte{s.SourceID: content}}
	pack := packWithEvidence("codex", "cli", ">=0.144.0 <0.145.0", domain.KnowledgeEvidenceRef{ID: s.SourceID, Digest: digestBytes(content)})

	res, err := PollSource(context.Background(), fetcher, s, pack)
	if err != nil {
		t.Fatalf("PollSource: %v", err)
	}
	if res.Changed {
		t.Error("Changed = true, want false when the fresh digest matches the Pack's recorded digest")
	}
	if res.OldDigest != res.NewDigest {
		t.Errorf("OldDigest = %q, NewDigest = %q, want equal", res.OldDigest, res.NewDigest)
	}
}

func TestPollSource_DigestDiffers_Changed(t *testing.T) {
	s := testSource("codex", "codex-cli-doc")
	fetcher := &fakeFetcher{content: map[string][]byte{s.SourceID: []byte("brand new content")}}
	pack := packWithEvidence("codex", "cli", ">=0.144.0 <0.145.0", domain.KnowledgeEvidenceRef{ID: s.SourceID, Digest: digestBytes([]byte("old content"))})

	res, err := PollSource(context.Background(), fetcher, s, pack)
	if err != nil {
		t.Fatalf("PollSource: %v", err)
	}
	if !res.Changed {
		t.Fatal("Changed = false, want true when the fresh digest differs from the Pack's recorded digest")
	}
	if res.OldDigest == res.NewDigest {
		t.Error("OldDigest == NewDigest, want them to differ")
	}
}

func TestPollSource_RejectsNonAllowlistedSource(t *testing.T) {
	bad := Source{Host: "codex", SourceID: "not-real", Kind: "official-doc", URL: "https://attacker.example/x"}
	fetcher := &fakeFetcher{}
	if _, err := PollSource(context.Background(), fetcher, bad, nil); err == nil {
		t.Fatal("PollSource: want an error for a non-allowlisted Source, got nil")
	}
	if len(fetcher.calls) != 0 {
		t.Error("PollSource called the Fetcher for a non-allowlisted Source -- want the allowlist gate to reject before ever fetching")
	}
}

func TestPollSource_FetcherError_Propagates(t *testing.T) {
	s := testSource("codex", "codex-cli-doc")
	fetcher := &fakeFetcher{err: map[string]error{s.SourceID: fmt.Errorf("boom")}}
	if _, err := PollSource(context.Background(), fetcher, s, nil); err == nil {
		t.Fatal("PollSource: want the Fetcher's error to propagate, got nil")
	}
}

// --- PollHost ------------------------------------------------------------

func TestPollHost_NothingChanged_NoCandidate(t *testing.T) {
	fetcher := &fakeFetcher{content: map[string][]byte{}}
	pack := packWithEvidence("codex", "cli", ">=0.144.0 <0.145.0")
	for _, s := range OfficialSourcesForHost("codex") {
		content := []byte("content-for-" + s.SourceID)
		fetcher.content[s.SourceID] = content
		pack.Knowledge.Evidence = append(pack.Knowledge.Evidence, domain.KnowledgeEvidenceRef{ID: s.SourceID, Digest: digestBytes(content)})
	}

	results, candidate, has, err := PollHost(context.Background(), fetcher, "codex", pack, "omca knowledge poll", time.Now())
	if err != nil {
		t.Fatalf("PollHost: %v", err)
	}
	if has {
		t.Fatalf("hasCandidate = true, want false when every source's digest matches; candidate = %+v", candidate)
	}
	if len(results) != len(OfficialSourcesForHost("codex")) {
		t.Errorf("len(results) = %d, want %d", len(results), len(OfficialSourcesForHost("codex")))
	}
}

func TestPollHost_OneSourceChanged_BuildsValidCandidate(t *testing.T) {
	fetcher := &fakeFetcher{content: map[string][]byte{}}
	pack := packWithEvidence("codex", "cli", ">=0.144.0 <0.145.0")
	sources := OfficialSourcesForHost("codex")
	if len(sources) < 2 {
		t.Fatal("test premise: codex needs at least 2 allowlisted sources")
	}
	for i, s := range sources {
		content := []byte("content-for-" + s.SourceID)
		fetcher.content[s.SourceID] = content
		digest := digestBytes(content)
		if i == 0 {
			digest = digestBytes([]byte("stale content")) // force a mismatch on the first source only
		}
		pack.Knowledge.Evidence = append(pack.Knowledge.Evidence, domain.KnowledgeEvidenceRef{ID: s.SourceID, Digest: digest})
	}

	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	results, candidate, has, err := PollHost(context.Background(), fetcher, "codex", pack, "omca knowledge poll", now)
	if err != nil {
		t.Fatalf("PollHost: %v", err)
	}
	if !has {
		t.Fatal("hasCandidate = false, want true when one source's digest changed")
	}
	if len(results) != len(sources) {
		t.Errorf("len(results) = %d, want %d", len(results), len(sources))
	}
	if err := domain.ValidateKnowledgeCandidate(candidate); err != nil {
		t.Fatalf("PollHost built an invalid KnowledgeCandidate: %v", err)
	}
	if len(candidate.Spec.ChangedSources) != 1 {
		t.Fatalf("len(ChangedSources) = %d, want 1", len(candidate.Spec.ChangedSources))
	}
	if candidate.Spec.ChangedSources[0].SourceID != sources[0].SourceID {
		t.Errorf("ChangedSources[0].SourceID = %q, want %q", candidate.Spec.ChangedSources[0].SourceID, sources[0].SourceID)
	}
	if candidate.Spec.VersionRange.Old != ">=0.144.0 <0.145.0" {
		t.Errorf("VersionRange.Old = %q", candidate.Spec.VersionRange.Old)
	}
	if candidate.Metadata.Automation != "omca knowledge poll" {
		t.Errorf("Metadata.Automation = %q", candidate.Metadata.Automation)
	}
	if len(candidate.Spec.WriteCapabilityImpacts) == 0 {
		t.Error("WriteCapabilityImpacts is empty, want at least one BLOCKED entry for the affected concept")
	}
	for _, wc := range candidate.Spec.WriteCapabilityImpacts {
		if wc.Change != domain.WriteCapabilityBlocked {
			t.Errorf("WriteCapabilityImpacts[%s].Change = %q, want BLOCKED (STALE Pack never expands write capability)", wc.Concept, wc.Change)
		}
	}
	if len(candidate.Spec.NewKnownUnknowns) == 0 {
		t.Error("NewKnownUnknowns is empty, want at least the honest placeholder unknowns this PR documents")
	}
}

func TestPollHost_NilPack_ChangedSourcesStillDetected(t *testing.T) {
	fetcher := &fakeFetcher{content: map[string][]byte{}}
	for _, s := range OfficialSourcesForHost("claude-code") {
		fetcher.content[s.SourceID] = []byte("content-for-" + s.SourceID)
	}
	_, candidate, has, err := PollHost(context.Background(), fetcher, "claude-code", nil, "omca knowledge poll", time.Now())
	if err != nil {
		t.Fatalf("PollHost: %v", err)
	}
	// A nil pack means every source has no baseline to compare against, so
	// nothing is reported "changed" (there's nothing to diff FROM) --
	// distinct from "no Pack published at all" being itself worth
	// surfacing, which is knowledgeDriftSignals' job (internal/report),
	// not the Poller's.
	if has {
		t.Fatalf("hasCandidate = true for a nil pack, want false (no baseline means nothing to diff, not an assumed change); candidate=%+v", candidate)
	}
}

func TestPollHost_FetchError_Propagates(t *testing.T) {
	sources := OfficialSourcesForHost("codex")
	fetcher := &fakeFetcher{err: map[string]error{sources[0].SourceID: fmt.Errorf("network down")}}
	if _, _, _, err := PollHost(context.Background(), fetcher, "codex", nil, "omca knowledge poll", time.Now()); err == nil {
		t.Fatal("PollHost: want the first source's fetch error to propagate, got nil")
	}
}

// --- PackForHost -----------------------------------------------------------

func TestPackForHost_PicksLexicallyLastID(t *testing.T) {
	root := t.TempDir()
	writePackDir(t, root, "codex/cli/0.144", codexManifestJSON)
	repo, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}
	pack, ok := PackForHost(repo, "codex")
	if !ok {
		t.Fatal("PackForHost: want ok=true")
	}
	if pack.Knowledge.Metadata.Host != "codex" {
		t.Errorf("Host = %q", pack.Knowledge.Metadata.Host)
	}
}

func TestPackForHost_NoPacksForHost(t *testing.T) {
	if _, ok := PackForHost(Repository{}, "codex"); ok {
		t.Fatal("PackForHost: want ok=false for an empty repository")
	}
}

// --- HTTPFetcher / httpGet mechanics (httptest only, never a real network
// call) ----------------------------------------------------------------

// TestHTTPFetcher_Fetch_RejectsNonAllowlisted_NeverCallsHTTPClient proves
// Fetch's allowlist gate runs BEFORE any HTTP client is touched: the
// Transport here panics if ever invoked, so this test would fail loudly
// (not silently pass for the wrong reason) if the gate were bypassed.
func TestHTTPFetcher_Fetch_RejectsNonAllowlisted_NeverCallsHTTPClient(t *testing.T) {
	panicTransport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTPFetcher.Fetch made an HTTP call for a non-allowlisted Source -- the allowlist gate must reject before ever reaching the network")
		return nil, nil
	})
	f := HTTPFetcher{Client: &http.Client{Transport: panicTransport}}
	bad := Source{Host: "codex", SourceID: "not-real", Kind: "official-doc", URL: "https://attacker.example/x"}
	if _, err := f.Fetch(context.Background(), bad); err == nil {
		t.Fatal("Fetch: want an error for a non-allowlisted Source, got nil")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestHTTPGet_LocalHTTPTestServer_Success exercises httpGet's real GET/read
// mechanics against a local httptest.Server -- this is the ONLY kind of
// live HTTP round trip this test suite ever performs; the server is an
// in-process loopback listener this test itself started, never a real
// third-party host.
func TestHTTPGet_LocalHTTPTestServer_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello from local test server"))
	}))
	defer ts.Close()

	raw, err := httpGet(context.Background(), ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("httpGet: %v", err)
	}
	if !bytes.Equal(raw, []byte("hello from local test server")) {
		t.Errorf("httpGet: got %q", raw)
	}
}

func TestHTTPGet_LocalHTTPTestServer_NonOKStatus_Errors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	if _, err := httpGet(context.Background(), ts.Client(), ts.URL); err == nil {
		t.Fatal("httpGet: want an error for a 404 response, got nil")
	}
}

// TestClientRefusingRedirects_DoesNotFollowToADifferentServer is a
// regression test (Copilot review finding on this PR): HTTPFetcher.Fetch
// previously used the caller's http.Client as-is, which follows Go's
// default redirect policy (up to 10 redirects, to ANY host) -- an
// allowlisted URL that responded with a redirect to a non-allowlisted
// domain would still be silently fetched, weakening the entire "closed
// allowlist" guarantee ValidateSource exists to provide. Two separate local
// httptest servers stand in for "the allowlisted origin" and "a
// non-allowlisted redirect target": the redirect must never be followed,
// proven both by asserting an error and by proving the second server never
// received a request at all.
func TestClientRefusingRedirects_DoesNotFollowToADifferentServer(t *testing.T) {
	var targetHit bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("this must never be reached"))
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	client := clientRefusingRedirects(origin.Client())
	if _, err := httpGet(context.Background(), client, origin.URL); err == nil {
		t.Fatal("httpGet through a redirect-refusing client: want an error (the redirect response itself fails the 200-only check), got nil")
	}
	if targetHit {
		t.Error("the redirect target server received a request -- the redirect was followed instead of refused")
	}
}

// TestHTTPGet_OversizedBody_ErrorsRatherThanSilentlyTruncating is a
// regression test (Copilot review finding on this PR): httpGet previously
// silently truncated a response body larger than maxFetchBytes and
// returned it as if it were the complete document -- computing a digest
// over that silently truncated content could hide a real change past the
// cutoff, or produce a spurious "changed" between two documents that are
// actually identical up to the truncation point. An oversized response
// must now be rejected with a clear error, never truncated and returned as
// if complete.
func TestHTTPGet_OversizedBody_ErrorsRatherThanSilentlyTruncating(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, maxFetchBytes+1024)
		_, _ = w.Write(buf)
	}))
	defer ts.Close()

	if _, err := httpGet(context.Background(), ts.Client(), ts.URL); err == nil {
		t.Fatal("httpGet against a response larger than maxFetchBytes: want an error, got nil (was it silently truncated instead?)")
	}
}

// TestHTTPGet_BodyExactlyAtLimit_Succeeds proves the boundary itself is
// still accepted -- only responses that GENUINELY exceed maxFetchBytes are
// rejected, not ones that land exactly on it.
func TestHTTPGet_BodyExactlyAtLimit_Succeeds(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, maxFetchBytes)
		_, _ = w.Write(buf)
	}))
	defer ts.Close()

	raw, err := httpGet(context.Background(), ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("httpGet with a body exactly at maxFetchBytes: want success, got: %v", err)
	}
	if len(raw) != maxFetchBytes {
		t.Errorf("len(raw) = %d, want exactly maxFetchBytes = %d", len(raw), maxFetchBytes)
	}
}

func TestHTTPFetcher_Fetch_DefaultsToHTTPDefaultClient(t *testing.T) {
	f := HTTPFetcher{}
	if f.Client != nil {
		t.Fatal("test premise: Client should start nil")
	}
	// Only proves the zero-value Client is accepted and the allowlist gate
	// still runs first (still rejects a bad Source) -- not a real network
	// call, since ValidateSource rejects before http.DefaultClient is ever
	// used.
	bad := Source{Host: "codex", SourceID: "not-real"}
	if _, err := f.Fetch(context.Background(), bad); err == nil {
		t.Fatal("Fetch: want an error, got nil")
	}
}
