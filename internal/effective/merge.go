package effective

import (
	"fmt"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/ontology"
)

// Options carries the facts this package's merge operators need but that
// neither domain.PrecedenceProgram (a frozen, four-field type: ID, Identity,
// Operator, Fixture — docs/ontology/README.md §7's Adapter Record Contract)
// nor a Candidate itself supplies: an explicit scope precedence order, an
// explicit FIRST_MATCH priority list, and an explicit deny set. Every one of
// today's real Knowledge Packs (knowledge/hosts/claude-code/cli/2.1,
// knowledge/hosts/codex/cli/0.144) supplies none of these — REPLACE,
// DEEP_MERGE's leaf-conflict case, and UNION_BY_ID's content-collision case
// all correctly stay unresolved for those Packs as a result, matching the
// committed fixture goldens (fixture_test.go). merge_test.go proves each
// operator's real, deterministic behavior end-to-end by supplying an
// Options value explicitly, the way a future Knowledge Pack revision that
// earns a qualified resolve capability would need to.
type Options struct {
	// ScopeRank ranks domain.ObservationScope.Kind values: a higher number
	// wins. Used by REPLACE, DEEP_MERGE (leaf conflicts), and UNION_BY_ID
	// (content collisions under the same ID).
	ScopeRank map[string]int
	// ScopePriority is an ordered, highest-priority-first list of scope
	// kinds. Used by FIRST_MATCH.
	ScopePriority []string
	// DeniedRefs names Candidate.Ref values an applicable deny policy
	// blocks, independent of domain.SourceDisposition (a caller may know
	// about a deny policy a Candidate's own Disposition does not yet
	// reflect). Used by DENY_WINS.
	DeniedRefs map[string]bool
}

// distinctByContent returns one representative Candidate per distinct
// ContentDigest among candidates, sorted by Ref for determinism. len(...)==1
// means every candidate agrees on content: nothing to adjudicate, regardless
// of how many physical sources reported it.
func distinctByContent(candidates []Candidate) []Candidate {
	byDigest := map[string]Candidate{}
	for _, c := range candidates {
		if _, ok := byDigest[c.ContentDigest]; !ok {
			byDigest[c.ContentDigest] = c
		}
	}
	out := make([]Candidate, 0, len(byDigest))
	for _, c := range byDigest {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

func candidateRefs(candidates []Candidate) []string {
	refs := make([]string, 0, len(candidates))
	for _, c := range candidates {
		refs = append(refs, c.Ref)
	}
	sort.Strings(refs)
	return refs
}

// highestEvidence returns the strongest EvidenceLevel among candidates
// (EvidenceLevel.Rank, internal/domain/evidencelevel.go), so a resolution
// outcome never claims weaker evidence than what actually backs it.
func highestEvidence(candidates []Candidate) domain.EvidenceLevel {
	best := domain.EvidenceLevelDiscovered
	for _, c := range candidates {
		if c.EvidenceLevel.Rank() > best.Rank() {
			best = c.EvidenceLevel
		}
	}
	return best
}

// ResolveGroup resolves one LogicalGroup: trivially, if every Candidate
// already agrees on content; otherwise only when the host's Knowledge Pack
// both declares a usable PrecedenceProgram for the concept AND has qualified
// (EXACT/COMPATIBLE) resolve capability for it (see doc.go's "central
// discipline"). Exactly one of the two return values is non-nil.
func ResolveGroup(group LogicalGroup, hk domain.HostKnowledge, capOps domain.CapabilityOps, opts Options) (*EffectiveEntry, *Conflict) {
	if len(group.Candidates) == 0 {
		return nil, &Conflict{Concept: group.Concept, LogicalID: group.LogicalID, Reason: "empty group"}
	}

	distinct := distinctByContent(group.Candidates)
	if len(distinct) == 1 {
		winner := distinct[0]
		refs := candidateRefs(group.Candidates)
		reason := fmt.Sprintf("only one physical source for %q", group.LogicalID)
		if len(group.Candidates) > 1 {
			reason = fmt.Sprintf("%d physical sources for %q agree byte-for-byte; nothing to adjudicate", len(group.Candidates), group.LogicalID)
		}
		return &EffectiveEntry{
			Concept:   group.Concept,
			LogicalID: group.LogicalID,
			Provenance: Provenance{
				SelectedSource: winner.Ref,
				ActiveSources:  refs,
			},
			EvidenceLevel: highestEvidence(group.Candidates),
			Guarantee:     domain.GuaranteeObserved,
			Confirmed:     false,
			Reason:        reason,
		}, nil
	}

	program, ok := LookupProgram(hk, group.Concept)
	operator := ontology.MergeOperator(program.Operator)
	if !ok || !operator.Valid() {
		return nil, &Conflict{
			Concept:       group.Concept,
			LogicalID:     group.LogicalID,
			Candidates:    group.Candidates,
			Program:       program.ID,
			Operator:      program.Operator,
			EvidenceLevel: highestEvidence(group.Candidates),
			Reason: fmt.Sprintf(
				"no usable precedence program for concept %q: %d sources disagree and either no PrecedenceProgram is declared or its operator (%q) is not one of the nine closed docs/ontology/README.md §3.1 operators -- an UNKNOWN/undeclared operator is never guessed past",
				group.Concept, len(distinct), program.Operator,
			),
		}
	}

	if !capabilityQualified(capOps) {
		return nil, &Conflict{
			Concept:       group.Concept,
			LogicalID:     group.LogicalID,
			Candidates:    group.Candidates,
			Program:       program.ID,
			Operator:      string(operator),
			EvidenceLevel: highestEvidence(group.Candidates),
			Reason: fmt.Sprintf(
				"precedence program %q declares operator %s for concept %q, but the Knowledge Pack's resolve capability is %q (not EXACT/COMPATIBLE); %d sources disagree and this package refuses to guess a winner",
				program.ID, operator, group.Concept, capOps.Resolve, len(distinct),
			),
		}
	}

	return applyOperator(group, program, operator, opts)
}

func capabilityQualified(capOps domain.CapabilityOps) bool {
	return capOps.Resolve == domain.CapabilityExact || capOps.Resolve == domain.CapabilityCompatible
}

// applyOperator applies operator's real merge semantics to group, once both
// ResolveGroup gates (a usable operator, a qualified resolve capability)
// have passed.
func applyOperator(group LogicalGroup, program domain.PrecedenceProgram, operator ontology.MergeOperator, opts Options) (*EffectiveEntry, *Conflict) {
	switch operator {
	case ontology.OpReplace:
		return resolveByScopeRank(group, program, operator, opts.ScopeRank)
	case ontology.OpUnionByID:
		return resolveUnionByID(group, program, opts.ScopeRank)
	case ontology.OpDeepMerge:
		return resolveDeepMerge(group, program, opts.ScopeRank)
	case ontology.OpConcatOrdered:
		return resolveConcatWithinGroup(group, program, opts.ScopeRank)
	case ontology.OpFirstMatch:
		return resolveFirstMatch(group, program, opts.ScopePriority)
	case ontology.OpNamespace:
		return resolveNamespace(group, program)
	case ontology.OpDenyWins:
		return resolveDenyWins(group, program, opts.DeniedRefs)
	case ontology.OpManagedGuardrail:
		return resolveManagedGuardrail(group, program)
	default: // OpUnspecified: "vendor does not define conflict resolution; surface a conflict."
		return nil, &Conflict{
			Concept: group.Concept, LogicalID: group.LogicalID, Candidates: group.Candidates,
			Program: program.ID, Operator: string(operator),
			EvidenceLevel: highestEvidence(group.Candidates),
			Reason:        fmt.Sprintf("operator %s does not define a conflict winner (docs/ontology/README.md §3.1)", operator),
		}
	}
}

// resolveByScopeRank picks the single candidate whose scope has the highest
// rank in scopeRank — REPLACE's real behavior ("a higher source replaces a
// scalar or whole entity"). It is unresolved (a Conflict) when scopeRank is
// empty, does not cover every candidate's scope, or the top rank is tied
// between two distinct-content candidates: REPLACE always needs a total
// scope order, and this package never invents one.
func resolveByScopeRank(group LogicalGroup, program domain.PrecedenceProgram, operator ontology.MergeOperator, scopeRank map[string]int) (*EffectiveEntry, *Conflict) {
	distinct := distinctByContent(group.Candidates)
	if len(scopeRank) == 0 {
		return nil, conflictf(group, program, operator, "%s requires an explicit scope precedence order; none was supplied", operator)
	}
	best := -1
	var winners []Candidate
	for _, c := range distinct {
		rank, ok := scopeRank[c.Scope.Kind]
		if !ok {
			return nil, conflictf(group, program, operator, "%s: scope %q has no declared rank in the supplied scope order", operator, c.Scope.Kind)
		}
		switch {
		case rank > best:
			best = rank
			winners = []Candidate{c}
		case rank == best:
			winners = append(winners, c)
		}
	}
	if len(winners) != 1 {
		return nil, conflictf(group, program, operator, "%s: %d candidates tie for the highest scope rank; refusing to guess between them", operator, len(winners))
	}
	winner := winners[0]
	var ignored []string
	for _, c := range distinct {
		if c.Ref != winner.Ref {
			ignored = append(ignored, c.Ref)
		}
	}
	sort.Strings(ignored)
	return &EffectiveEntry{
		Concept:   group.Concept,
		LogicalID: group.LogicalID,
		Provenance: Provenance{
			Program: program.ID, Operator: operator,
			SelectedSource: winner.Ref,
			ActiveSources:  []string{winner.Ref},
			IgnoredSources: ignored,
		},
		EvidenceLevel: highestEvidence(group.Candidates),
		Guarantee:     domain.GuaranteeObserved,
		Confirmed:     highestEvidence(group.Candidates).Rank() >= domain.EvidenceLevelHostReported.Rank(),
		Reason:        fmt.Sprintf("%s: scope %q (rank %d) outranks the other %d candidate(s)", operator, winner.Scope.Kind, best, len(distinct)-1),
	}, nil
}

// resolveUnionByID resolves one already-ID-matched group under UNION_BY_ID.
// "Union by ID" alone does not say what happens when two sources define the
// same ID with different content (docs/ontology/README.md §3.1's own
// wording only promises "entities merge by canonical logical ID while
// retaining provenance") — this package treats that as needing the same
// scope order REPLACE needs, and refuses to guess without one.
func resolveUnionByID(group LogicalGroup, program domain.PrecedenceProgram, scopeRank map[string]int) (*EffectiveEntry, *Conflict) {
	return resolveByScopeRank(group, program, ontology.OpUnionByID, scopeRank)
}

// resolveDeepMerge recursively merges every candidate's Fields. Non-
// conflicting leaves union cleanly (DEEP_MERGE's real, common-case
// behavior); a leaf where two candidates supply different scalar values
// needs scopeRank to pick a winner the same way REPLACE does, and is
// unresolved without one. A candidate with nil Fields (opaque/unstructured
// content) cannot be deep-merged structurally at all.
func resolveDeepMerge(group LogicalGroup, program domain.PrecedenceProgram, scopeRank map[string]int) (*EffectiveEntry, *Conflict) {
	distinct := distinctByContent(group.Candidates)
	for _, c := range distinct {
		if c.Fields == nil {
			return nil, conflictf(group, program, ontology.OpDeepMerge, "DEEP_MERGE: candidate %q has no structured content to merge", c.Ref)
		}
	}
	merged, conflictPath, err := deepMergeAll(distinct, scopeRank)
	if err != nil {
		return nil, conflictf(group, program, ontology.OpDeepMerge, "DEEP_MERGE: leaf conflict at %q across %d candidates and no scope order resolves it", conflictPath, len(distinct))
	}
	digest, digErr := domain.CanonicalDigest(merged)
	if digErr != nil {
		return nil, conflictf(group, program, ontology.OpDeepMerge, "DEEP_MERGE: %v", digErr)
	}
	return &EffectiveEntry{
		Concept:   group.Concept,
		LogicalID: group.LogicalID,
		Provenance: Provenance{
			Program: program.ID, Operator: ontology.OpDeepMerge,
			// SelectedSource is documented as "the winning Candidate.Ref";
			// DEEP_MERGE has no single source winner (every input stays
			// active), so it stays empty here rather than being overloaded
			// with the merged-content digest, which isn't a Ref at all.
			ActiveSources: candidateRefs(group.Candidates),
			Constraints:   []string{fmt.Sprintf("DEEP_MERGE: merged content digest %s", digest)},
		},
		EvidenceLevel: highestEvidence(group.Candidates),
		Guarantee:     domain.GuaranteeObserved,
		Confirmed:     highestEvidence(group.Candidates).Rank() >= domain.EvidenceLevelHostReported.Rank(),
		Reason:        fmt.Sprintf("DEEP_MERGE: %d candidates merged with no unresolved leaf conflicts", len(distinct)),
	}, nil
}

// deepMergeAll recursively merges every candidate's Fields, highest-
// scopeRank first (so a later, lower-priority candidate never silently
// overwrites an already-set leaf the way a naive left-to-right merge would).
// A leaf both an earlier and a later candidate set to different scalar
// values is a real conflict: if scopeRank covers every candidate's scope it
// is resolved (the highest-ranked value already "won" by merge order); if
// not, this function reports the first unresolvable path and stops.
func deepMergeAll(distinct []Candidate, scopeRank map[string]int) (map[string]any, string, error) {
	ordered := append([]Candidate(nil), distinct...)
	complete := scopeRankCoversAll(scopeRank, distinct)
	if complete {
		sort.SliceStable(ordered, func(i, j int) bool {
			return scopeRank[ordered[i].Scope.Kind] < scopeRank[ordered[j].Scope.Kind]
		})
	}
	result := map[string]any{}
	for _, c := range ordered {
		path, ok := deepMergeInto(result, c.Fields, "")
		if !ok && !complete {
			return nil, path, fmt.Errorf("unresolved leaf conflict at %q", path)
		}
	}
	return result, "", nil
}

// scopeRankCoversAll reports whether scopeRank declares an explicit rank for
// every distinct candidate's scope kind. This must be checked by set
// membership, not by comparing len(scopeRank) to the number of distinct
// scopes here: a scopeRank map that happens to have the same number of
// entries as there are distinct scopes, but names entirely different scope
// kinds, would otherwise be treated as "complete." Every lookup in the sort
// comparator would then silently fall back to the zero value, ranking every
// candidate as 0 — a real leaf conflict would get resolved by incidental
// stable-sort (original) order instead of an actual, intended precedence.
func scopeRankCoversAll(scopeRank map[string]int, distinct []Candidate) bool {
	for _, c := range distinct {
		if _, ok := scopeRank[c.Scope.Kind]; !ok {
			return false
		}
	}
	return true
}

// deepMergeInto merges src into dst in place (recursively, for nested
// map[string]any values). It returns ok=false the first time a scalar (or
// differently-typed) leaf already present in dst would be silently
// overwritten by a different value from src — the caller decides whether
// that is tolerable (a supplied scope order means "the merge order already
// encodes the intended winner") or must be reported as an unresolved
// conflict.
func deepMergeInto(dst, src map[string]any, path string) (string, bool) {
	ok := true
	for k, v := range src {
		p := k
		if path != "" {
			p = path + "." + k
		}
		existing, present := dst[k]
		if !present {
			dst[k] = v
			continue
		}
		existingMap, existingIsMap := existing.(map[string]any)
		vMap, vIsMap := v.(map[string]any)
		if existingIsMap && vIsMap {
			childPath, childOK := deepMergeInto(existingMap, vMap, p)
			if !childOK {
				return childPath, false
			}
			continue
		}
		if !valuesEqual(existing, v) {
			dst[k] = v // merge order (highest scopeRank last) determines the winner
			ok = false
			path = p
		}
	}
	if !ok {
		return path, false
	}
	return "", true
}

func valuesEqual(a, b any) bool {
	da, errA := domain.CanonicalDigest(a)
	db, errB := domain.CanonicalDigest(b)
	return errA == nil && errB == nil && da == db
}

// resolveConcatWithinGroup applies CONCAT_ORDERED to candidates that share
// one logical ID (the rarer, within-group use — see compose.go for the more
// common cross-group composition CONCAT_ORDERED concepts like instruction
// actually need). Every candidate is always active (concatenation never
// shadows). Confirmed follows EffectiveEntry.Confirmed's documented contract
// exactly like ComposeConcept does: true only when scopeRank covers every
// candidate's scope (the order is known) AND the winning outcome is backed
// by E3+ evidence — order-confirmed alone is not enough. Composed stays
// false: EffectiveEntry.Composed documents "spanning every logical entity of
// the concept" (compose.go's cross-entity composition), and this is a
// per-group resolution, not that.
func resolveConcatWithinGroup(group LogicalGroup, program domain.PrecedenceProgram, scopeRank map[string]int) (*EffectiveEntry, *Conflict) {
	refs := candidateRefs(group.Candidates)
	ordered, orderConfirmed := concatOrder(group.Candidates, scopeRank)
	confirmed := orderConfirmed && highestEvidence(group.Candidates).Rank() >= domain.EvidenceLevelHostReported.Rank()
	reason := "CONCAT_ORDERED: all sources remain active; exact order unconfirmed (no scope order supplied)"
	if orderConfirmed {
		reason = fmt.Sprintf("CONCAT_ORDERED: all sources active in confirmed order %v", ordered)
	}
	return &EffectiveEntry{
		Concept:   group.Concept,
		LogicalID: group.LogicalID,
		Provenance: Provenance{
			Program: program.ID, Operator: ontology.OpConcatOrdered,
			ActiveSources: refs,
		},
		EvidenceLevel: highestEvidence(group.Candidates),
		Guarantee:     domain.GuaranteeAdvisory,
		Confirmed:     confirmed,
		Reason:        reason,
	}, nil
}

// resolveFirstMatch applies FIRST_MATCH: the first scope in priority
// (highest first) that has any candidate wins; later candidates (at lower-
// priority scopes, or additional candidates at the same winning scope with
// different content) are ignored/unresolved respectively. Unresolved
// (Conflict) when priority is empty, does not cover every candidate's
// scope, or more than one distinct-content candidate exists at the winning
// scope.
func resolveFirstMatch(group LogicalGroup, program domain.PrecedenceProgram, priority []string) (*EffectiveEntry, *Conflict) {
	distinct := distinctByContent(group.Candidates)
	if len(priority) == 0 {
		return nil, conflictf(group, program, ontology.OpFirstMatch, "FIRST_MATCH requires an explicit scope priority order; none was supplied")
	}
	rankOf := func(scope string) int {
		for i, s := range priority {
			if s == scope {
				return i
			}
		}
		return -1
	}
	for _, c := range distinct {
		if rankOf(c.Scope.Kind) == -1 {
			return nil, conflictf(group, program, ontology.OpFirstMatch, "FIRST_MATCH: scope %q is not in the supplied priority order", c.Scope.Kind)
		}
	}
	best := len(priority)
	var winners []Candidate
	for _, c := range distinct {
		r := rankOf(c.Scope.Kind)
		switch {
		case r < best:
			best = r
			winners = []Candidate{c}
		case r == best:
			winners = append(winners, c)
		}
	}
	if len(winners) != 1 {
		return nil, conflictf(group, program, ontology.OpFirstMatch, "FIRST_MATCH: %d distinct-content candidates tie at the same highest-priority scope", len(winners))
	}
	winner := winners[0]
	var ignored []string
	for _, c := range group.Candidates {
		if c.Ref != winner.Ref {
			ignored = append(ignored, c.Ref)
		}
	}
	sort.Strings(ignored)
	return &EffectiveEntry{
		Concept:   group.Concept,
		LogicalID: group.LogicalID,
		Provenance: Provenance{
			Program: program.ID, Operator: ontology.OpFirstMatch,
			SelectedSource: winner.Ref,
			ActiveSources:  []string{winner.Ref},
			IgnoredSources: ignored,
		},
		EvidenceLevel: highestEvidence(group.Candidates),
		Guarantee:     domain.GuaranteeObserved,
		Confirmed:     highestEvidence(group.Candidates).Rank() >= domain.EvidenceLevelHostReported.Rank(),
		Reason:        fmt.Sprintf("FIRST_MATCH: scope %q is the first present in priority order", winner.Scope.Kind),
	}, nil
}

// resolveNamespace applies NAMESPACE: duplicate names remain distinct
// through their own scope/source prefix, so every candidate stays active
// under its own Ref with nothing ignored and no single "selected" winner —
// this is the one operator whose real behavior never needs an external
// ranking fact to be internally consistent.
func resolveNamespace(group LogicalGroup, program domain.PrecedenceProgram) (*EffectiveEntry, *Conflict) {
	refs := candidateRefs(group.Candidates)
	return &EffectiveEntry{
		Concept:   group.Concept,
		LogicalID: group.LogicalID,
		Provenance: Provenance{
			Program: program.ID, Operator: ontology.OpNamespace,
			ActiveSources: refs,
		},
		EvidenceLevel: highestEvidence(group.Candidates),
		Guarantee:     domain.GuaranteeObserved,
		Confirmed:     highestEvidence(group.Candidates).Rank() >= domain.EvidenceLevelHostReported.Rank(),
		Reason:        fmt.Sprintf("NAMESPACE: %d differently-scoped/sourced candidates remain simultaneously active, each addressable by its own namespaced Ref", len(refs)),
	}, nil
}

// resolveDenyWins applies DENY_WINS: any candidate named in denied (or
// already domain.DispositionDenied) is excluded outright; among the
// remaining candidates, zero means "nothing active" (resolved), exactly one
// distinct-content survivor means it wins, and more than one distinct-
// content survivor is unresolved (DENY_WINS decides exclusion, not which of
// several non-denied sources wins — that is a separate question this
// operator does not answer).
func resolveDenyWins(group LogicalGroup, program domain.PrecedenceProgram, denied map[string]bool) (*EffectiveEntry, *Conflict) {
	var survivors []Candidate
	var deniedRefs []string
	for _, c := range group.Candidates {
		if denied[c.Ref] || c.Disposition == domain.DispositionDenied {
			deniedRefs = append(deniedRefs, c.Ref)
			continue
		}
		survivors = append(survivors, c)
	}
	sort.Strings(deniedRefs)

	distinctSurvivors := distinctByContent(survivors)
	confirmed := highestEvidence(group.Candidates).Rank() >= domain.EvidenceLevelHostReported.Rank()
	switch len(distinctSurvivors) {
	case 0:
		return &EffectiveEntry{
			Concept:   group.Concept,
			LogicalID: group.LogicalID,
			Provenance: Provenance{
				Program: program.ID, Operator: ontology.OpDenyWins,
				IgnoredSources: deniedRefs,
				Constraints:    []string{"DENY_WINS: every candidate denied"},
			},
			EvidenceLevel: highestEvidence(group.Candidates),
			Guarantee:     domain.GuaranteeHard,
			Confirmed:     confirmed,
			Reason:        "DENY_WINS: every candidate is denied; nothing active",
		}, nil
	case 1:
		winner := distinctSurvivors[0]
		return &EffectiveEntry{
			Concept:   group.Concept,
			LogicalID: group.LogicalID,
			Provenance: Provenance{
				Program: program.ID, Operator: ontology.OpDenyWins,
				SelectedSource: winner.Ref,
				ActiveSources:  candidateRefs(survivors),
				IgnoredSources: deniedRefs,
				Constraints:    []string{"DENY_WINS: applicable denials removed the rest"},
			},
			EvidenceLevel: highestEvidence(group.Candidates),
			Guarantee:     domain.GuaranteeHard,
			Confirmed:     confirmed,
			Reason:        fmt.Sprintf("DENY_WINS: %d candidate(s) denied, one distinct-content survivor remains", len(deniedRefs)),
		}, nil
	default:
		return nil, conflictf(group, program, ontology.OpDenyWins, "DENY_WINS: %d distinct-content candidates survive after denial and still disagree", len(distinctSurvivors))
	}
}

// resolveManagedGuardrail applies MANAGED_GUARDRAIL: a managed-scope
// candidate is admin policy constraining the result, not merely a higher-
// priority value — exactly one managed-scope candidate present makes it the
// authoritative winner regardless of what non-managed candidates say.
// Unresolved when no managed-scope candidate is present (there is no
// guardrail to apply) or when more than one managed-scope candidate
// disagrees (the guardrail source itself is internally inconsistent).
func resolveManagedGuardrail(group LogicalGroup, program domain.PrecedenceProgram) (*EffectiveEntry, *Conflict) {
	var managed []Candidate
	for _, c := range group.Candidates {
		if c.Scope.Kind == "managed" {
			managed = append(managed, c)
		}
	}
	distinctManaged := distinctByContent(managed)
	if len(distinctManaged) == 0 {
		return nil, conflictf(group, program, ontology.OpManagedGuardrail, "MANAGED_GUARDRAIL: no managed-scope source present to constrain the result")
	}
	if len(distinctManaged) > 1 {
		return nil, conflictf(group, program, ontology.OpManagedGuardrail, "MANAGED_GUARDRAIL: %d managed-scope sources disagree", len(distinctManaged))
	}
	winner := distinctManaged[0]
	var ignored []string
	for _, c := range group.Candidates {
		if c.Ref != winner.Ref {
			ignored = append(ignored, c.Ref)
		}
	}
	sort.Strings(ignored)
	return &EffectiveEntry{
		Concept:   group.Concept,
		LogicalID: group.LogicalID,
		Provenance: Provenance{
			Program: program.ID, Operator: ontology.OpManagedGuardrail,
			SelectedSource: winner.Ref,
			ActiveSources:  []string{winner.Ref},
			IgnoredSources: ignored,
			Constraints:    []string{"MANAGED_GUARDRAIL: managed-scope source constrains the result"},
		},
		EvidenceLevel: highestEvidence(group.Candidates),
		Guarantee:     domain.GuaranteeHard,
		Confirmed:     highestEvidence(group.Candidates).Rank() >= domain.EvidenceLevelHostReported.Rank(),
		Reason:        "MANAGED_GUARDRAIL: the managed-scope source is authoritative",
	}, nil
}

func conflictf(group LogicalGroup, program domain.PrecedenceProgram, operator ontology.MergeOperator, format string, args ...any) *Conflict {
	return &Conflict{
		Concept:       group.Concept,
		LogicalID:     group.LogicalID,
		Candidates:    group.Candidates,
		Program:       program.ID,
		Operator:      string(operator),
		EvidenceLevel: highestEvidence(group.Candidates),
		Reason:        fmt.Sprintf(format, args...),
	}
}
