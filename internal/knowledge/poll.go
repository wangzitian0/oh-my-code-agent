package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Fetcher fetches one allowlisted Source's current raw content.
//
// The production implementation ([HTTPFetcher]) makes a real net/http
// request; every automated test in this package supplies a fake or
// httptest-backed Fetcher instead, so `go test` never makes a real
// outbound request to a real third-party domain (see doc.go's safety
// note). A future real caller -- e.g. an `omca knowledge poll` CLI command
// -- is the only place [HTTPFetcher] is meant to run for real.
type Fetcher interface {
	Fetch(ctx context.Context, s Source) ([]byte, error)
}

// maxFetchBytes bounds how much of one source's response body HTTPFetcher
// reads: official docs/schemas are small, human-authored pages, and an
// unbounded read of an unexpectedly huge or slow-draining response body is
// not a risk this package needs to accept for a feature whose whole job is
// polling a short, known set of documents.
const maxFetchBytes = 8 << 20 // 8 MiB

// HTTPFetcher is the production [Fetcher]: a real HTTP GET against
// Source.URL using Client (http.DefaultClient if nil). It refuses to fetch
// anything that does not pass [ValidateSource] first -- the same
// closed-allowlist discipline internal/auth.PlanAllowlistedSymlinks applies
// to symlink targets, applied here to network destinations.
type HTTPFetcher struct {
	Client *http.Client
}

// Fetch implements Fetcher: it rejects anything that is not exactly one of
// the closed allowlist's entries ([ValidateSource]), then performs the real
// GET via [httpGet]. Splitting the allowlist gate from the actual HTTP
// mechanics this way lets this package's own tests exercise the GET/status/
// body-size-limit logic against a local httptest.Server (via httpGet
// directly) without that test URL needing to be a real allowlisted
// third-party domain -- ValidateSource's own rejection behavior is proven
// independently in source_test.go. No automated test in this package calls
// Fetch itself with a real allowlisted URL.
func (f HTTPFetcher) Fetch(ctx context.Context, s Source) ([]byte, error) {
	if err := ValidateSource(s); err != nil {
		return nil, fmt.Errorf("knowledge: HTTPFetcher.Fetch: %w", err)
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	client = clientRefusingRedirects(client)
	raw, err := httpGet(ctx, client, s.URL)
	if err != nil {
		return nil, fmt.Errorf("knowledge: HTTPFetcher.Fetch: %w", err)
	}
	return raw, nil
}

// clientRefusingRedirects returns a shallow copy of base with CheckRedirect
// set to refuse every redirect, whatever base's own CheckRedirect already
// was: an allowlisted URL that responds with a redirect to a
// non-allowlisted domain must not be silently followed and fetched anyway,
// which is exactly what Go's default redirect policy (follow up to 10
// redirects, to any host) would otherwise do -- a real Copilot review
// finding on this PR, since ValidateSource only ever checks the ORIGINAL
// request URL, never anywhere a 3xx response might redirect to.
// http.ErrUseLastResponse makes the Client return the 3xx response itself
// rather than follow it, which httpGet's own existing "200 only" status
// check then rejects with a clear error -- no separate redirect-specific
// error path is needed.
func clientRefusingRedirects(base *http.Client) *http.Client {
	c := *base
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &c
}

// httpGet performs the actual bounded HTTP GET + read: request construction,
// the GET itself, a 200-only status check, and a maxFetchBytes-bounded body
// read. It has no knowledge of the Source allowlist at all -- Fetch is the
// only production caller, always after ValidateSource has already passed --
// which is what makes it safe and meaningful for tests to call directly
// against a local httptest.Server.
func httpGet(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %q: %w", rawURL, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %q: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %q: unexpected status %d", rawURL, resp.StatusCode)
	}
	// Read one byte past maxFetchBytes so a response that is actually
	// LARGER than the limit is detected and rejected, rather than silently
	// truncated -- computing a digest over a silently truncated body could
	// produce an incorrect "changed"/"not changed" result whenever the
	// upstream document's size crosses maxFetchBytes (a real Copilot
	// review finding on this PR): the old and new digests would both be
	// digests of the same truncated prefix, hiding a real change past the
	// cutoff, or two genuinely-identical-up-to-the-cutoff documents could
	// spuriously read as unchanged.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading body of %q: %w", rawURL, err)
	}
	if len(raw) > maxFetchBytes {
		return nil, fmt.Errorf("reading body of %q: response exceeds the %d byte limit, refusing to digest a truncated body", rawURL, maxFetchBytes)
	}
	return raw, nil
}

// digestBytes returns a "sha256:<hex>" digest for raw, in the same
// "sha256:<64 lowercase hex>" shape domain.CanonicalDigest produces
// (domain.IsCanonicalDigest's pattern) -- not domain.CanonicalDigest itself,
// since that function canonicalizes a Go value through JSON first, and raw
// fetched bytes are not a Go value to canonicalize, they are the exact
// content being digested.
func digestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PollResult is the outcome of polling one Source and comparing its fresh
// digest against whatever digest the currently published Knowledge Pack
// recorded for that same source's evidence id.
type PollResult struct {
	Source    Source
	OldDigest string // "" when the Pack recorded no baseline digest for this source
	NewDigest string
	Changed   bool
	Reason    string
}

// evidenceDigest returns the KnowledgeEvidenceRef.Digest pack (if non-nil)
// records for sourceID, and whether one was found at all.
func evidenceDigest(pack *Pack, sourceID string) (string, bool) {
	if pack == nil {
		return "", false
	}
	for _, ev := range pack.Knowledge.Evidence {
		if ev.ID == sourceID {
			return ev.Digest, true
		}
	}
	return "", false
}

// PollSource fetches s via fetcher and compares its fresh content digest
// against the digest pack's evidence records for s.SourceID (nil pack, or a
// pack that records no evidence entry for this source id, or an evidence
// entry with an empty digest, all mean "no baseline to compare" -- reported
// honestly as Changed:false with an explanatory Reason, never guessed as a
// change).
func PollSource(ctx context.Context, fetcher Fetcher, s Source, pack *Pack) (PollResult, error) {
	if err := ValidateSource(s); err != nil {
		return PollResult{}, fmt.Errorf("knowledge: PollSource: %w", err)
	}
	raw, err := fetcher.Fetch(ctx, s)
	if err != nil {
		return PollResult{}, fmt.Errorf("knowledge: PollSource: %w", err)
	}
	newDigest := digestBytes(raw)

	old, found := evidenceDigest(pack, s.SourceID)
	switch {
	case !found || old == "":
		return PollResult{Source: s, NewDigest: newDigest, Changed: false,
			Reason: fmt.Sprintf("no baseline digest recorded in the current Pack for source %q; nothing to compare yet", s.SourceID)}, nil
	case old == newDigest:
		return PollResult{Source: s, OldDigest: old, NewDigest: newDigest, Changed: false,
			Reason: "content digest matches the currently published Pack"}, nil
	default:
		return PollResult{Source: s, OldDigest: old, NewDigest: newDigest, Changed: true,
			Reason: "content digest differs from the currently published Pack"}, nil
	}
}

// PackForHost returns the Pack repo currently has loaded for host that a
// Poller should compare fresh digests against. When more than one Pack is
// loaded for host (e.g. a superseded and a current one), it returns the one
// whose metadata.id sorts last -- current committed Packs publish exactly
// one per host today, and Pack IDs embed the version they cover
// (docs/knowledge/README.md §4's "codex:cli:0.144" shape), so a lexically
// later id names a later version for any host that ever does publish more
// than one. Returns ok=false when repo has no Pack for host at all (a poll
// still runs; every source simply has no baseline to compare against).
func PackForHost(repo Repository, host string) (Pack, bool) {
	var matches []Pack
	for _, p := range repo.Packs() {
		if p.Knowledge.Metadata.Host == host {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return Pack{}, false
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Knowledge.Metadata.ID < matches[j].Knowledge.Metadata.ID
	})
	return matches[len(matches)-1], true
}

// PollHost polls every allowlisted Source for host via fetcher, comparing
// each against pack's recorded evidence digests (see [PackForHost]), and
// returns every individual [PollResult] plus, when at least one source
// changed, a [domain.KnowledgeCandidate] documenting it
// (docs/knowledge/README.md §9's Candidate Report field list). hasCandidate
// is false when nothing changed -- PollHost never fabricates a candidate
// for a clean poll.
//
// Several KnowledgeCandidateSpec fields are honest placeholders in this PR
// rather than fully automated:
//
//   - FixtureResults is reported NOT_RUN for every fixture the matched
//     Pack's PrecedencePrograms name a fixture for -- running qualification
//     fixtures automatically is issue #33/PR-29's explicit job, not this
//     Poller's.
//   - VersionRange.New, StaleGenerations, and RequiredAdapterChanges are
//     left empty: determining the correct new version range, which
//     generations a maintainer would consider stale, and what adapter code
//     (if any) needs to change are maintainer judgment calls a digest
//     mismatch alone cannot safely make (ADR-0004 decision 4's own "never
//     act on them silently").
func PollHost(ctx context.Context, fetcher Fetcher, host string, pack *Pack, automation string, now time.Time) ([]PollResult, domain.KnowledgeCandidate, bool, error) {
	sources := OfficialSourcesForHost(host)
	results := make([]PollResult, 0, len(sources))
	var changedSources []domain.ChangedSource

	for _, s := range sources {
		res, err := PollSource(ctx, fetcher, s, pack)
		if err != nil {
			return nil, domain.KnowledgeCandidate{}, false, fmt.Errorf("knowledge: PollHost: host %q: %w", host, err)
		}
		results = append(results, res)
		if res.Changed {
			changedSources = append(changedSources, domain.ChangedSource{
				SourceID:  s.SourceID,
				Kind:      s.Kind,
				URL:       s.URL,
				OldDigest: res.OldDigest,
				NewDigest: res.NewDigest,
			})
		}
	}

	if len(changedSources) == 0 {
		return results, domain.KnowledgeCandidate{}, false, nil
	}

	surface := "cli"
	oldRange := ""
	var affected []domain.AffectedCapability
	var fixtureResults []domain.FixtureResult
	var writeImpacts []domain.WriteCapabilityImpact
	if pack != nil {
		surface = pack.Knowledge.Metadata.Surface
		oldRange = pack.Knowledge.Metadata.VersionRange
		for concept, ops := range pack.Knowledge.Capabilities {
			for op, level := range map[string]domain.CapabilityLevel{
				"discover": ops.Discover, "parse": ops.Parse, "normalize": ops.Normalize,
				"resolve": ops.Resolve, "compile": ops.Compile, "verify": ops.Verify,
			} {
				if level == "" {
					continue
				}
				affected = append(affected, domain.AffectedCapability{Concept: concept, Operation: op, Old: level})
			}
			writeImpacts = append(writeImpacts, domain.WriteCapabilityImpact{
				Concept: concept,
				Change:  domain.WriteCapabilityBlocked,
				Reason:  "STALE Pack (docs/knowledge/README.md §7: \"New version or evidence changed; no expansion of write behavior\") -- write capability stays blocked until a maintainer re-qualifies a Pack covering the changed source",
			})
		}
		for _, prog := range pack.Knowledge.PrecedencePrograms {
			if prog.Fixture != "" {
				fixtureResults = append(fixtureResults, domain.FixtureResult{ID: prog.Fixture, Status: domain.FixtureResultNotRun, Detail: "qualification fixtures are not run automatically by this Poller (issue #33/PR-29)"})
			}
		}
		sort.Slice(affected, func(i, j int) bool {
			if affected[i].Concept != affected[j].Concept {
				return affected[i].Concept < affected[j].Concept
			}
			return affected[i].Operation < affected[j].Operation
		})
		sort.Slice(writeImpacts, func(i, j int) bool { return writeImpacts[i].Concept < writeImpacts[j].Concept })
		sort.Slice(fixtureResults, func(i, j int) bool { return fixtureResults[i].ID < fixtureResults[j].ID })
	}
	sort.Slice(changedSources, func(i, j int) bool { return changedSources[i].SourceID < changedSources[j].SourceID })

	collectedAt := now.UTC().Format(time.RFC3339)
	candidate := domain.KnowledgeCandidate{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "KnowledgeCandidate",
		Metadata: domain.KnowledgeCandidateMetadata{
			ID:          fmt.Sprintf("candidate:%s:%s:%s", host, surface, collectedAt),
			Host:        host,
			Surface:     surface,
			CollectedAt: collectedAt,
			Automation:  automation,
		},
		Spec: domain.KnowledgeCandidateSpec{
			ChangedSources:         changedSources,
			VersionRange:           domain.VersionRangeChange{Old: oldRange},
			AffectedCapabilities:   affected,
			FixtureResults:         fixtureResults,
			WriteCapabilityImpacts: writeImpacts,
			NewKnownUnknowns: []string{
				"the new host version range covering the changed source(s) has not yet been determined; requires maintainer investigation of the actual upstream change",
				"whether adapter code needs to change to reflect the upstream change is not yet determined",
			},
		},
	}
	if err := domain.ValidateKnowledgeCandidate(candidate); err != nil {
		return results, domain.KnowledgeCandidate{}, false, fmt.Errorf("knowledge: PollHost: host %q: built an invalid KnowledgeCandidate: %w", host, err)
	}
	return results, candidate, true, nil
}
