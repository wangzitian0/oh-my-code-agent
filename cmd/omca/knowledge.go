package main

import (
	stdcontext "context"
	"fmt"
	"io"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
)

// runKnowledge implements `omca knowledge poll [--json]`: for every host
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
// opened here (issue #33/PR-29's explicit job).
//
// This function itself is intentionally thin: it wires the one real,
// impure Fetcher (knowledge.HTTPFetcher, which makes real outbound HTTP
// requests) and the real on-disk Knowledge repository into
// pollAllHostsAndRender, which holds all the actual logic and is what this
// package's own tests exercise with a fake Fetcher instead -- so no
// automated test in this repository ever reaches a real HTTPFetcher call
// through this command (see internal/knowledge/poll_test.go's identical
// discipline for HTTPFetcher itself).
func runKnowledge(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 || args[0] != "poll" {
		fmt.Fprintln(stderr, "usage: omca knowledge poll [--json]")
		return 2
	}
	jsonOut, extra, err := parseJSONOnlyFlags(args[1:])
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
		_, candidate, has, err := knowledge.PollHost(stdcontext.Background(), fetcher, host, pack, "omca knowledge poll", now)
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
