package resolve

import "github.com/wangzitian0/oh-my-code-agent/internal/domain"

// AssetKind identifies which desired-state asset list an entry belongs to
// (docs/product/requirements.md §4.1 ProfileAssets: skills, mcpServers,
// instructions).
type AssetKind string

const (
	KindSkill       AssetKind = "skill"
	KindMCPServer   AssetKind = "mcpServer"
	KindInstruction AssetKind = "instruction"
)

// ResolvedAsset is the fully-resolved state of one asset for one host:
// whether it ended up active, the Profile-declared Intent that governed the
// decision (empty if no Profile ever declared one — see ResolvedAsset for
// ui-review-style assets introduced only by a host-scoped Activation entry),
// and a human-readable Reason tracing the decision to its source for
// diagnosis and reporting.
type ResolvedAsset struct {
	Kind   AssetKind     `json:"kind"`
	ID     string        `json:"id"`
	Active bool          `json:"active"`
	Intent domain.Intent `json:"intent,omitempty"`
	Reason string        `json:"reason"`
}

// Conflict records one asset+host pair for which Resolve could not
// determine a single winning intent under init.md's precedence rules. A
// conflict is a normal, reportable resolution outcome — not a program
// failure — and it must block generation for that asset+host until the
// authoring Profiles or a valid Exception resolve it. CandidateIntents is
// sorted for determinism and lists the distinct intents that disagreed.
type Conflict struct {
	Kind             AssetKind       `json:"kind"`
	AssetID          string          `json:"id"`
	Host             string          `json:"host"`
	CandidateIntents []domain.Intent `json:"candidateIntents"`
}

// ResolvedState is the fully-resolved desired state for one host: every
// asset Resolve reached a decision for, plus any unresolved conflicts.
// Assets and Conflicts are always sorted by (Kind, ID) so that ResolvedState
// is deep-equal (and, through domain.CanonicalDigest, byte-identical) across
// repeated Resolve calls on the same logical input, regardless of any map
// iteration order used internally.
type ResolvedState struct {
	Host      string          `json:"host"`
	Assets    []ResolvedAsset `json:"assets,omitempty"`
	Conflicts []Conflict      `json:"conflicts,omitempty"`
}

// Find returns the resolved entry for (kind, id), if Resolve reached a
// non-conflicting decision for it.
func (rs ResolvedState) Find(kind AssetKind, id string) (ResolvedAsset, bool) {
	for _, a := range rs.Assets {
		if a.Kind == kind && a.ID == id {
			return a, true
		}
	}
	return ResolvedAsset{}, false
}

// IsActive reports whether (kind, id) is active in rs. It returns false for
// an asset Resolve never reached (never mentioned by any input, or left as
// an unresolved Conflict) as well as for one resolved to inactive.
func (rs ResolvedState) IsActive(kind AssetKind, id string) bool {
	a, ok := rs.Find(kind, id)
	return ok && a.Active
}

// HasConflict reports whether (kind, id) is present in rs.Conflicts.
func (rs ResolvedState) HasConflict(kind AssetKind, id string) bool {
	for _, c := range rs.Conflicts {
		if c.Kind == kind && c.AssetID == id {
			return true
		}
	}
	return false
}
