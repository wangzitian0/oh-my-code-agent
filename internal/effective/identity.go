package effective

import (
	"fmt"
	"sort"
	"strings"
)

// MatchIdentities groups candidates into LogicalGroups (docs/architecture/
// README.md §4's Identity Matcher: "match physical representations to
// stable logical entities and preserve ambiguity"). Grouping itself is a
// confident, deterministic operation — two Candidates that already agree on
// (concept, LogicalID) per the ontology's own x-logicalIdentity rule are the
// same logical entity by definition (e.g. an MCP server with the same
// transport+id observed at both user and workspace scope: one logical
// server, two physical representations, exactly the issue #21 example).
// Genuine ambiguity — two Candidates that plausibly could or could not be
// the same logical entity — is a separate, weaker signal this function also
// detects (detectAmbiguousIdentities) and returns without acting on: an
// ambiguous pair is never merged into one group and never silently kept
// apart with no record, mirroring internal/resolve's Conflict/preserve-
// ambiguity pattern one layer down, at identity rather than intent.
func MatchIdentities(candidates []Candidate) ([]LogicalGroup, []AmbiguousIdentity) {
	byKey := map[string][]Candidate{}
	var order []string
	for _, c := range candidates {
		key := c.Concept + "\x00" + c.LogicalID
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], c)
	}
	sort.Strings(order)

	groups := make([]LogicalGroup, 0, len(order))
	for _, key := range order {
		parts := strings.SplitN(key, "\x00", 2)
		cands := append([]Candidate(nil), byKey[key]...)
		sort.Slice(cands, func(i, j int) bool { return cands[i].Ref < cands[j].Ref })
		groups = append(groups, LogicalGroup{Concept: parts[0], LogicalID: parts[1], Candidates: cands})
	}

	return groups, detectAmbiguousIdentities(candidates)
}

// detectAmbiguousIdentities flags mcp_server Candidate pairs that declare
// distinct LogicalIDs (so MatchIdentities's confident grouping already
// treats them as different logical entities) but whose full connection
// definition (command/args/env/url — Candidate.Fields, digested into
// ContentDigest) is byte-identical: the same executable/config invoked under
// two different registered names is a real, observed pattern (a user copies
// their global MCP registration into project config and renames the key, or
// two independently-authored registrations happen to wrap the same tool).
// The ontology's strict transport+id identity rule says "different"; the
// content coincidence says "maybe the same physical server." Neither signal
// is strong enough to decide unilaterally, so this function reports the
// pair without merging or dropping either Candidate — see identity_test.go's
// required golden ambiguous-pair case.
//
// This heuristic is scoped to mcp_server only: instruction identity is
// (scope.root, source.path) and skill identity is (name, source.kind), both
// already narrow enough (docs/ontology/README.md's x-logicalIdentity rules
// for those concepts) that a content coincidence across distinct IDs is not
// the same kind of signal — two skills with identical instructions text but
// different names are just two skills with duplicated content, not a
// candidate for "maybe one physical entity."
func detectAmbiguousIdentities(candidates []Candidate) []AmbiguousIdentity {
	var mcp []Candidate
	for _, c := range candidates {
		if c.Concept == "mcp_server" && c.Fields != nil && c.ContentDigest != "" {
			mcp = append(mcp, c)
		}
	}
	sort.Slice(mcp, func(i, j int) bool { return mcp[i].Ref < mcp[j].Ref })

	var out []AmbiguousIdentity
	for i := 0; i < len(mcp); i++ {
		for j := i + 1; j < len(mcp); j++ {
			a, b := mcp[i], mcp[j]
			if a.LogicalID == b.LogicalID {
				continue // same identity already: a collision for merge.go, not an identity ambiguity
			}
			if a.ContentDigest != b.ContentDigest {
				continue
			}
			out = append(out, AmbiguousIdentity{
				Concept: "mcp_server",
				A:       a,
				B:       b,
				Reason: fmt.Sprintf(
					"candidates %q (logical id %q) and %q (logical id %q) declare byte-identical connection definitions (command/args/env/url) under different logical IDs; this may be the same physical MCP server registered twice under different names, or two independent registrations that happen to share configuration -- preserved as ambiguous rather than merged into one entity or treated as confidently distinct",
					a.Ref, a.LogicalID, b.Ref, b.LogicalID,
				),
			})
		}
	}
	return out
}
