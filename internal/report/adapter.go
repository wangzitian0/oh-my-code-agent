package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/drift"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
)

// BuildDriftSignals is the PR-18-anticipated adapter (see doc.go's "The
// PR-18-anticipated adapter" section): it translates one host's real
// Effective Graph (internal/effective.EffectiveGraph, PR-17) into the
// []drift.Signal shape internal/drift's Classify/ClassifyAll/Group pipeline
// (PR-18) consumes, so `omca drift` runs end-to-end on real graph output
// rather than only internal/drift's own hand-built fixtures.
//
// Two graph-native situations become signals, both computable from the
// Effective Graph alone (no Desired Graph correlation needed — see the
// "Known follow-up" note below):
//
//   - an unresolved effective.Conflict becomes SOURCE_DRIFT: "representations
//     of one logical entity diverge" (reporting.md §6) is exactly what a
//     Conflict already means — more than one distinct-content Candidate for
//     one logical entity, with no qualified resolver able to pick a winner.
//   - an effective.AmbiguousIdentity becomes UNKNOWN: the Identity Matcher
//     found two Candidates suspicious enough to flag without enough signal
//     to decide whether they are the same logical entity at all —
//     "the system cannot safely classify the result" (reporting.md §6)
//     describes exactly this, one layer beneath SOURCE_DRIFT's already-
//     grouped ambiguity.
//
// # Known follow-up: Desired-vs-Effective correlation (EFFECTIVE_DRIFT)
//
// reporting.md §6's EFFECTIVE_DRIFT ("host-effective state differs from
// desired/current state") is deliberately NOT produced by this adapter.
// Correlating a resolve.ResolvedAsset (Desired Graph, Profile-authored
// asset ID) with an effective.EffectiveEntry (Effective Graph, physically-
// derived LogicalID — e.g. extract.go's "name|source.Kind" for skill,
// "scope.root|source.path" for instruction) is not a solved identity
// mapping in this codebase yet: the two ID schemes are not guaranteed to
// coincide even when they name the same real-world asset, and guessing a
// correlation would silently produce false-positive or false-negative drift
// — exactly the "guessed adapter" internal/effective's own doc.go central
// discipline forbids one layer down. Per this project's "capability-gap
// shipping is allowed, hiding is not" policy (issue #13 round-2 audit,
// issue #47/#55 precedent), this is filed as a named, honest scope gap
// rather than a silently-approximate EFFECTIVE_DRIFT signal; a future PR
// that builds a real Desired<->Effective identity bridge is the correct
// place to add it.
//
// A third situation needs more than the Effective Graph alone: an unqualified
// knowledge.Resolution (ADR-0004 decision 3 -- "no published Pack's version
// range covers the installed version ... treated as UNKNOWN and degrades to
// observed/OBSERVED reconcile mode") becomes KNOWLEDGE_DRIFT
// (knowledgeDriftSignals below), so a human sees the same fail-closed fact
// cmd/omca/mcp.go's capabilityFuncForMCP already enforces at the write gate,
// surfaced here as a first-class, groupable ActionCard instead of only a
// silent capability denial.
//
// project is the human-facing project label every emitted Signal's Project
// field carries (reporting.md §7's "8 projects x 5 hosts x 40 artifacts"
// impact dimension). A caller building a report across several worktrees
// calls this once per (project, host) Graphs pair and concatenates the
// results before handing them to drift.ClassifyAll.
func BuildDriftSignals(project string, g effective.Graphs, resolution knowledge.Resolution, repo knowledge.Repository) []drift.Signal {
	var out []drift.Signal
	out = append(out, conflictSignals(project, g)...)
	out = append(out, ambiguousIdentitySignals(project, g)...)
	out = append(out, knowledgeDriftSignals(project, g, resolution, repo)...)
	return out
}

// conflictSignals turns every effective.Conflict into one SOURCE_DRIFT
// Signal, EntityID'd by "<concept>/<logicalID>" (stable and unique per
// logical entity within one host, matching effective.EffectiveGraph.Find's
// own (concept, logicalID) key).
func conflictSignals(project string, g effective.Graphs) []drift.Signal {
	out := make([]drift.Signal, 0, len(g.Effective.Conflicts))
	for _, c := range g.Effective.Conflicts {
		refs := candidateRefs(c.Candidates)
		reason := c.Reason
		if reason == "" {
			reason = fmt.Sprintf("%s %q has %d candidate sources and no qualified resolver could select a winner", c.Concept, c.LogicalID, len(c.Candidates))
		}
		out = append(out, drift.Signal{
			EntityID:      c.Concept + "/" + c.LogicalID,
			Concept:       c.Concept,
			Field:         "selectedSource",
			Category:      domain.DriftSourceDrift,
			Expected:      "single resolved source",
			Observed:      refs,
			RootCause:     reason,
			Remediation:   "qualify a precedence program for this concept in the host's Knowledge Pack so the resolver can select a winner, or resolve the collision with an explicit Profile/Exception",
			Project:       project,
			Host:          g.Host,
			HostVersion:   g.HostVersion,
			EvidenceLevel: c.EvidenceLevel,
			Guarantee:     domain.GuaranteeObserved,
		})
	}
	return out
}

// ambiguousIdentitySignals turns every effective.AmbiguousIdentity into one
// UNKNOWN Signal. UNKNOWN signals are never dropped as a no-diff (see
// drift.Classify's doc comment: "explicit UNKNOWN signals are never skipped
// this way"), so Expected/Observed here are purely descriptive, not a
// mechanism this adapter relies on for filtering.
func ambiguousIdentitySignals(project string, g effective.Graphs) []drift.Signal {
	out := make([]drift.Signal, 0, len(g.Effective.AmbiguousIdentities))
	for _, amb := range g.Effective.AmbiguousIdentities {
		reason := amb.Reason
		if reason == "" {
			reason = fmt.Sprintf("%s: %q and %q were flagged as possibly the same logical entity, with insufficient signal to decide either way", amb.Concept, amb.A.Ref, amb.B.Ref)
		}
		out = append(out, drift.Signal{
			EntityID:    amb.Concept + "/" + amb.A.LogicalID + "~" + amb.B.LogicalID,
			Concept:     amb.Concept,
			Field:       "identity",
			Category:    domain.DriftUnknown,
			Observed:    []string{amb.A.Ref, amb.B.Ref},
			RootCause:   reason,
			Remediation: "manually confirm whether these two sources represent the same logical entity",
			Project:     project,
			Host:        g.Host,
			HostVersion: g.HostVersion,
			Guarantee:   domain.GuaranteeObserved,
		})
	}
	return out
}

// knowledgeDriftSignals turns one host's unqualified knowledge.Resolution
// into a single KNOWLEDGE_DRIFT drift.Signal. A Qualified resolution (the
// installed version falls inside some published Pack's versionRange)
// produces nothing -- there is no drift to report.
//
// This is the report-time half of the same fail-closed fact
// cmd/omca/mcp.go's capabilityFuncForMCP already enforces at the write gate
// (an unqualified Resolution makes CapabilityFor return
// ReconcileMode=OBSERVED with every level empty, which
// internal/mcp/propose.go's capabilityAndPolicyGates then rejects): before
// this function existed, an unqualified host silently lost write capability
// with no human-visible signal explaining why. The Signal names the
// installed version and every version range this host DOES have a published
// Pack for, so a maintainer sees the actual gap rather than just "capability
// denied".
func knowledgeDriftSignals(project string, g effective.Graphs, resolution knowledge.Resolution, repo knowledge.Repository) []drift.Signal {
	if resolution.Qualified {
		return nil
	}

	ranges := packVersionRangesForHost(repo, g.Host)
	var observed string
	if len(ranges) == 0 {
		observed = fmt.Sprintf("no Knowledge Pack is published for host %q at all", g.Host)
	} else {
		observed = fmt.Sprintf("published Knowledge Pack version range(s) for host %q: %s -- none cover the installed version", g.Host, strings.Join(ranges, ", "))
	}

	reason := resolution.Reason
	if reason == "" {
		reason = fmt.Sprintf("host %q version %q does not fall inside any qualified Knowledge Pack's versionRange", g.Host, g.HostVersion)
	}

	return []drift.Signal{{
		EntityID:    "host/" + g.Host,
		Concept:     "host",
		Field:       "knowledgeQualification",
		Category:    domain.DriftKnowledgeDrift,
		Expected:    fmt.Sprintf("installed version %q inside a qualified Knowledge Pack's versionRange", g.HostVersion),
		Observed:    observed,
		RootCause:   reason,
		Remediation: "poll allowlisted official sources, build a Knowledge Candidate, run qualification fixtures, and publish an updated Pack covering this version (docs/knowledge/README.md §8); until then this host degrades to observation-only and write capability expansion is blocked",
		Project:     project,
		Host:        g.Host,
		HostVersion: g.HostVersion,
		Guarantee:   domain.GuaranteeObserved,
	}}
}

// packVersionRangesForHost returns the sorted, deduplicated set of
// metadata.versionRange values every loaded Pack declares for host --
// exactly the set an unqualified Resolution's installed version fell
// outside of, so knowledgeDriftSignals can show a human what IS published
// rather than only that the installed version is not covered by it.
func packVersionRangesForHost(repo knowledge.Repository, host string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, p := range repo.Packs() {
		if p.Knowledge.Metadata.Host != host {
			continue
		}
		r := p.Knowledge.Metadata.VersionRange
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// candidateRefs returns every Candidate.Ref in c, sorted for determinism
// (Conflict.Candidates order already traces back to identity.go's own
// deterministic grouping, but sorting here makes this adapter's output
// independent of that upstream ordering choice too).
func candidateRefs(candidates []effective.Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.Ref)
	}
	sort.Strings(out)
	return out
}
