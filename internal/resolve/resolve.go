package resolve

import (
	"fmt"
	"sort"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Resolve computes the fully-resolved desired state for host, given an
// already-selected set of Profiles (the result of Binding selection —
// Resolve does not implement Binding matching), the worktree's Activation,
// any applicable Exceptions, and a reference instant now.
//
// now is an explicit parameter, not time.Now(), so that Resolve stays a
// pure function: an Exception's expiry is meaningless without a reference
// clock, and calling time.Now() internally would make identical inputs
// produce different output across calls (violating the determinism this
// package guarantees and that Deliverable #4's property test checks).
// Callers pass time.Now() at the call site; tests pass a fixed instant.
//
// Resolve returns a non-nil error only for genuinely invalid input (an
// unknown host ID, per domain.ValidateHostID). An asset+host pair for which
// resolution cannot determine a single winning intent is not an error: it
// is reported as a Conflict in the returned ResolvedState so it stays
// visible and blocks generation, per init.md and docs/product/requirements.md
// §4.3 ("Ambiguous conflicts remain visible and block unsafe generation").
//
// # Precedence algorithm
//
// For each (kind, assetID) mentioned by any input Profile or by the
// Activation's skills/mcpServers selections:
//
//  1. Collect every Profile AssetRef entry that applies to host: a
//     host-neutral entry (no hosts selector) applies to every host; a
//     host-scoped entry applies only to the hosts it lists
//     (docs/product/requirements.md §4.1).
//
//  2. Reduce each Profile's own applicable entries to at most one intent.
//     REQUIRED and DENIED are "sticky": within one Profile, a sticky entry
//     always wins over a merely DEFAULT/AVAILABLE ("soft") entry for the
//     same host, because a Profile is a single scope with no internal
//     order — this is how one Profile can set a host-neutral DEFAULT and
//     narrow it to DENIED for one specific host without contradicting
//     itself. If the same Profile has two applicable entries that
//     genuinely disagree (both REQUIRED and DENIED, or two different soft
//     values with nothing to prefer one over the other), that Profile is
//     internally ambiguous and resolution reports a Conflict.
//
//  3. Combine the (at most one per Profile) reduced intents across
//     Profiles, in the order Profiles were passed (docs/product/
//     requirements.md §5.1 composes personal, company, team, and project
//     Profiles broad-to-narrow, in that order; Resolve assumes the same
//     convention — later entries are the more specific, "lower" scope):
//
//     - DENIED beats REQUIRED and any soft intent from any other Profile,
//     regardless of order ("denied intent cannot be weakened by a lower
//     scope", init.md Invariants) — unless a valid, unexpired Exception
//     scoped to (one of) the denying Profiles' IDs applies, in which case
//     the DENIED is excepted and resolution falls through to REQUIRED.
//     - Symmetrically, REQUIRED beats any soft intent from any other
//     Profile regardless of order ("cannot be disabled without an
//     explicit exception", init.md) — unless a valid, unexpired Exception
//     scoped to (one of) the requiring Profiles' IDs applies, in which
//     case REQUIRED no longer blocks a later Activation disable.
//     - REQUIRED and DENIED from different Profiles directly contradict
//     each other and neither documented rule adjudicates between them:
//     this is the "two Profiles disagree in a way lower-precedence rules
//     don't resolve" case, reported as a Conflict, unless a valid
//     Exception scoped to one side's Profile resolves it in that side's
//     favor.
//     - Otherwise (only DEFAULT/AVAILABLE present, possibly from several
//     Profiles), the last applicable Profile in the input order wins —
//     "a later/more-specific Profile ... can turn it off" (DEFAULT "may
//     be disabled" by a narrower scope).
//     - An assetID with no applicable Profile entry at this host at all
//     has no Profile-derived baseline; see step 4.
//
//  4. Apply the Activation on top of the Profile-derived baseline, host-
//     neutral entries first and then this host's host-scoped entries (a
//     host-scoped entry "refines" — and is applied after, so it can
//     override — the host-neutral selection, docs/product/requirements.md
//     §4.3):
//
//     - A host-neutral Activation enable/disable can only ever *select*
//     within a baseline the Profiles already established at this host —
//     it cannot invent presence at a host the Profiles never scoped the
//     asset to. This is why, in the requirements §4 golden scenario, a
//     host-neutral `enable: {mcpServers: [codegraph]}` does not activate
//     codegraph for claude-code: codegraph's only Profile entry is
//     DEFAULT scoped to hosts:[codex], so it has no baseline at
//     claude-code for a host-neutral selection to act on.
//     - A host-scoped Activation entry for exactly this host, by contrast,
//     is already as specific as it can be, and may introduce activation
//     for an assetID with no Profile baseline at all (as ui-review is,
//     in the same golden scenario, activated purely by
//     `hosts.claude-code.enable.skills: [ui-review]`).
//     - Neither can re-enable a DENIED intent, and neither can disable a
//     REQUIRED intent, except through a valid, unexpired, scope-matching
//     Exception (the round-2 audit's exception golden cases).
func Resolve(profiles []domain.Profile, activation domain.Activation, exceptions []domain.Exception, host string, now time.Time) (ResolvedState, error) {
	if err := domain.ValidateHostID(host); err != nil {
		return ResolvedState{}, fmt.Errorf("resolve: %w", err)
	}

	candidates := collectProfileCandidates(profiles)
	universe := collectUniverse(candidates, activation, host)

	state := ResolvedState{Host: host}
	for _, key := range universe {
		asset, conflict, ok := resolveOne(key, candidates[key], activation, exceptions, host, now)
		if !ok {
			state.Conflicts = append(state.Conflicts, conflict)
			continue
		}
		state.Assets = append(state.Assets, asset)
	}
	return state, nil
}

// assetKey identifies one asset within one kind's namespace: the same ID
// string used for a skill and an mcpServer names two different assets.
type assetKey struct {
	kind AssetKind
	id   string
}

// profileEntry is one Profile AssetRef, tagged with the owning Profile's
// position in the input slice (for "last applicable Profile wins" soft-tier
// tie-breaking) and ID (for Exception scope matching).
type profileEntry struct {
	profileIdx  int
	profileID   string
	intent      domain.Intent
	hostNeutral bool
	hosts       map[string]bool
}

func (e profileEntry) appliesTo(host string) bool {
	if e.hostNeutral {
		return true
	}
	return e.hosts[host]
}

// collectProfileCandidates indexes every AssetRef across all three asset
// groups, for every Profile, keyed by (kind, assetID).
func collectProfileCandidates(profiles []domain.Profile) map[assetKey][]profileEntry {
	out := map[assetKey][]profileEntry{}
	add := func(kind AssetKind, refs []domain.AssetRef, idx int, profileID string) {
		for _, ref := range refs {
			key := assetKey{kind, ref.ID}
			entry := profileEntry{profileIdx: idx, profileID: profileID, intent: ref.Intent}
			if len(ref.Hosts) == 0 {
				entry.hostNeutral = true
			} else {
				entry.hosts = make(map[string]bool, len(ref.Hosts))
				for _, h := range ref.Hosts {
					entry.hosts[h] = true
				}
			}
			out[key] = append(out[key], entry)
		}
	}
	for idx, p := range profiles {
		add(KindSkill, p.Spec.Assets.Skills, idx, p.Metadata.ID)
		add(KindMCPServer, p.Spec.Assets.MCPServers, idx, p.Metadata.ID)
		add(KindInstruction, p.Spec.Assets.Instructions, idx, p.Metadata.ID)
	}
	return out
}

// collectUniverse returns every assetKey Resolve must decide for: every
// Profile-mentioned (kind, id) regardless of host (so an out-of-host-scope
// asset like codegraph-at-claude-code is reported inactive rather than
// silently omitted), plus every (skill|mcpServer, id) the Activation
// mentions for this host, host-neutrally or host-scoped. The result is
// sorted by (kind, id) so iteration order never depends on Go's randomized
// map order.
func collectUniverse(candidates map[assetKey][]profileEntry, activation domain.Activation, host string) []assetKey {
	seen := make(map[assetKey]bool, len(candidates))
	for k := range candidates {
		seen[k] = true
	}
	addSelection := func(kind AssetKind, ids []string) {
		for _, id := range ids {
			seen[assetKey{kind, id}] = true
		}
	}
	addSelection(KindSkill, activation.Spec.Enable.Skills)
	addSelection(KindMCPServer, activation.Spec.Enable.MCPServers)
	addSelection(KindSkill, activation.Spec.Disable.Skills)
	addSelection(KindMCPServer, activation.Spec.Disable.MCPServers)
	if hostAct, ok := activation.Spec.Hosts[host]; ok {
		addSelection(KindSkill, hostAct.Enable.Skills)
		addSelection(KindMCPServer, hostAct.Enable.MCPServers)
		addSelection(KindSkill, hostAct.Disable.Skills)
		addSelection(KindMCPServer, hostAct.Disable.MCPServers)
	}

	keys := make([]assetKey, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		return keys[i].id < keys[j].id
	})
	return keys
}

// resolveOne resolves a single (kind, id) at host. ok is false when the
// asset is left as a Conflict rather than a decided ResolvedAsset.
func resolveOne(key assetKey, entries []profileEntry, activation domain.Activation, exceptions []domain.Exception, host string, now time.Time) (ResolvedAsset, Conflict, bool) {
	applicable := make([]profileEntry, 0, len(entries))
	for _, e := range entries {
		if e.appliesTo(host) {
			applicable = append(applicable, e)
		}
	}

	// Group applicable entries by owning Profile, preserving first-seen
	// (== input slice) order.
	var order []int
	seenIdx := map[int]bool{}
	byIdx := map[int][]profileEntry{}
	for _, e := range applicable {
		if !seenIdx[e.profileIdx] {
			seenIdx[e.profileIdx] = true
			order = append(order, e.profileIdx)
		}
		byIdx[e.profileIdx] = append(byIdx[e.profileIdx], e)
	}
	sort.Ints(order)

	type reducedProfile struct {
		idx       int
		profileID string
		intent    domain.Intent
	}
	reduced := make([]reducedProfile, 0, len(order))
	for _, idx := range order {
		grp := byIdx[idx]
		intent, ok := reduceGroup(grp)
		if !ok {
			return ResolvedAsset{}, Conflict{
				Kind:             key.kind,
				AssetID:          key.id,
				Host:             host,
				CandidateIntents: sortedDistinctIntents(grp),
			}, false
		}
		reduced = append(reduced, reducedProfile{idx: idx, profileID: grp[0].profileID, intent: intent})
	}

	present := len(reduced) > 0

	deniedIDs := map[string]bool{}
	requiredIDs := map[string]bool{}
	hasDenied, hasRequired := false, false
	for _, r := range reduced {
		switch r.intent {
		case domain.IntentDenied:
			hasDenied = true
			deniedIDs[r.profileID] = true
		case domain.IntentRequired:
			hasRequired = true
			requiredIDs[r.profileID] = true
		}
	}

	var baseIntent domain.Intent
	var baseActive bool
	var reason string

	switch {
	case hasDenied && hasRequired:
		if ex, ok := findException(exceptions, key.id, deniedIDs, now); ok {
			baseIntent, baseActive = domain.IntentRequired, true
			reason = fmt.Sprintf("REQUIRED wins: DENIED excepted by scope %q (%s)", ex.Scope, ex.Justification)
		} else if ex, ok := findException(exceptions, key.id, requiredIDs, now); ok {
			baseIntent, baseActive = domain.IntentDenied, false
			reason = fmt.Sprintf("DENIED wins: REQUIRED excepted by scope %q (%s)", ex.Scope, ex.Justification)
		} else {
			return ResolvedAsset{}, Conflict{
				Kind:             key.kind,
				AssetID:          key.id,
				Host:             host,
				CandidateIntents: []domain.Intent{domain.IntentDenied, domain.IntentRequired},
			}, false
		}
	case hasDenied:
		baseIntent, baseActive = domain.IntentDenied, false
		reason = "DENIED by profile policy"
	case hasRequired:
		baseIntent, baseActive = domain.IntentRequired, true
		reason = "REQUIRED by profile policy"
	case present:
		last := reduced[len(reduced)-1]
		baseIntent = last.intent
		baseActive = baseIntent == domain.IntentDefault
		reason = fmt.Sprintf("%s by profile policy", baseIntent)
	default:
		baseActive = false
		reason = "no applicable profile entry"
	}

	active := baseActive
	if present {
		active, reason = applyLayer(key.kind, key.id, "host-neutral activation", activation.Spec.Enable, activation.Spec.Disable, active, baseIntent, reason, deniedIDs, requiredIDs, exceptions, now)
	}
	if hostAct, ok := activation.Spec.Hosts[host]; ok {
		active, reason = applyLayer(key.kind, key.id, "host-scoped activation", hostAct.Enable, hostAct.Disable, active, baseIntent, reason, deniedIDs, requiredIDs, exceptions, now)
	}

	return ResolvedAsset{Kind: key.kind, ID: key.id, Active: active, Intent: baseIntent, Reason: reason}, Conflict{}, true
}

// reduceGroup collapses one Profile's applicable entries for one (kind,
// id, host) to at most one intent. See Resolve's doc comment step 2.
func reduceGroup(entries []profileEntry) (domain.Intent, bool) {
	distinct := map[domain.Intent]bool{}
	for _, e := range entries {
		distinct[e.intent] = true
	}
	if len(distinct) == 1 {
		for i := range distinct {
			return i, true
		}
	}
	hasDenied := distinct[domain.IntentDenied]
	hasRequired := distinct[domain.IntentRequired]
	switch {
	case hasDenied && hasRequired:
		return "", false
	case hasDenied:
		return domain.IntentDenied, true
	case hasRequired:
		return domain.IntentRequired, true
	default:
		// Two or more distinct soft (DEFAULT/AVAILABLE) values within the
		// same Profile at the same host: no order exists within a single
		// scope to prefer one, so this is ambiguous.
		return "", false
	}
}

// sortedDistinctIntents returns the distinct intents among entries, sorted,
// for a deterministic Conflict.CandidateIntents.
func sortedDistinctIntents(entries []profileEntry) []domain.Intent {
	seen := map[domain.Intent]bool{}
	out := make([]domain.Intent, 0, len(entries))
	for _, e := range entries {
		if !seen[e.intent] {
			seen[e.intent] = true
			out = append(out, e.intent)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// inSelection reports whether id is named in sel for kind. Instructions
// have no ActivationSelection field, so they are never Activation-selected.
func inSelection(kind AssetKind, id string, sel domain.ActivationSelection) bool {
	var list []string
	switch kind {
	case KindSkill:
		list = sel.Skills
	case KindMCPServer:
		list = sel.MCPServers
	default:
		return false
	}
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

// applyLayer applies one Activation layer's (host-neutral or host-scoped)
// enable/disable selections on top of the current (active, reason) state.
// intent is the Profile-derived baseline intent (unaffected by earlier
// layers), used to decide whether an enable/disable needs an Exception.
func applyLayer(kind AssetKind, id, layerName string, enableSel, disableSel domain.ActivationSelection, active bool, intent domain.Intent, reason string, deniedIDs, requiredIDs map[string]bool, exceptions []domain.Exception, now time.Time) (bool, string) {
	if inSelection(kind, id, enableSel) {
		if intent == domain.IntentDenied {
			if ex, ok := findException(exceptions, id, deniedIDs, now); ok {
				active = true
				reason = fmt.Sprintf("%s enable overrides DENIED via exception scope %q (%s)", layerName, ex.Scope, ex.Justification)
			}
			// else: blocked, DENIED holds — cannot be re-enabled by any
			// lower scope or host-scoped entry (init.md).
		} else if !active {
			// Only attribute the decision to this layer's enable when it
			// actually changes the outcome; an enable on an asset already
			// active from an earlier layer is a no-op and must not hide the
			// real reason it is active (e.g. a Profile REQUIRED/DEFAULT).
			active = true
			reason = layerName + " enable"
		}
	}
	if inSelection(kind, id, disableSel) {
		if intent == domain.IntentRequired {
			if ex, ok := findException(exceptions, id, requiredIDs, now); ok {
				active = false
				reason = fmt.Sprintf("%s disable overrides REQUIRED via exception scope %q (%s)", layerName, ex.Scope, ex.Justification)
			}
			// else: blocked, REQUIRED holds — cannot be disabled without
			// an explicit exception the defining policy allows.
		} else if active {
			// Symmetric with the enable case above: a disable on an asset
			// already inactive (e.g. DENIED or never selected) is a no-op
			// and must not overwrite the real reason it is inactive.
			active = false
			reason = layerName + " disable"
		}
	}
	return active, reason
}

// findException returns the first valid, unexpired Exception for assetID
// whose Scope names one of scopes (the defining Profile(s) for the
// REQUIRED/DENIED intent being excepted). now.Before(ExpiresAt) must hold
// strictly: an Exception whose ExpiresAt is now or earlier is expired and
// has no effect on resolution.
func findException(exceptions []domain.Exception, assetID string, scopes map[string]bool, now time.Time) (domain.Exception, bool) {
	if len(scopes) == 0 {
		return domain.Exception{}, false
	}
	for _, ex := range exceptions {
		if !ex.Valid() {
			continue
		}
		if ex.AssetID != assetID {
			continue
		}
		if !scopes[ex.Scope] {
			continue
		}
		if !now.Before(ex.ExpiresAt) {
			continue
		}
		return ex, true
	}
	return domain.Exception{}, false
}
