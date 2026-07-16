package domain

import "fmt"

// KnowledgeStatus is a Knowledge Pack's lifecycle state
// (docs/knowledge/README.md §7, "Lifecycle States").
type KnowledgeStatus string

const (
	// KnowledgeFresh: evidence and installed version remain inside the
	// qualification window.
	KnowledgeFresh KnowledgeStatus = "FRESH"
	// KnowledgeDue: recheck date passed; already qualified versions remain
	// usable.
	KnowledgeDue KnowledgeStatus = "DUE"
	// KnowledgeStale: new version or evidence changed; no expansion of write
	// behavior.
	KnowledgeStale KnowledgeStatus = "STALE"
	// KnowledgeConflicted: primary sources or fixtures disagree; affected
	// operations are blocked.
	KnowledgeConflicted KnowledgeStatus = "CONFLICTED"
	// KnowledgeSuperseded: a newer Pack replaces this one; historical
	// generations still reference it.
	KnowledgeSuperseded KnowledgeStatus = "SUPERSEDED"
	// KnowledgeRetired: no new generation uses the Pack; historical
	// explanation remains available.
	KnowledgeRetired KnowledgeStatus = "RETIRED"
)

// Valid reports whether s is one of the six defined lifecycle states.
func (s KnowledgeStatus) Valid() bool {
	switch s {
	case KnowledgeFresh, KnowledgeDue, KnowledgeStale, KnowledgeConflicted, KnowledgeSuperseded, KnowledgeRetired:
		return true
	default:
		return false
	}
}

// ValidateKnowledgeStatus rejects any value outside the closed lifecycle
// state enum.
func ValidateKnowledgeStatus(s KnowledgeStatus) error {
	if !s.Valid() {
		return fmt.Errorf("invalid knowledge status %q", s)
	}
	return nil
}
