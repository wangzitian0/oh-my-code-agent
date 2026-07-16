package domain

import "fmt"

// KnownHostIDs are the canonical host IDs from docs/ontology/README.md §4
// (Host Registry). Aliases are a lookup convenience at the adapter boundary;
// desired-state documents must use the canonical ID directly.
var KnownHostIDs = map[string]bool{
	"claude-code":     true,
	"codex":           true,
	"opencode":        true,
	"cursor":          true,
	"github-copilot":  true,
	"antigravity-cli": true,
	"pi":              true,
	"openclaw":        true,
	"hermes-agent":    true,
}

// ValidateHostID rejects a hosts-selector entry that does not name a
// canonical host ID, with an error naming the offending value.
func ValidateHostID(id string) error {
	if !KnownHostIDs[id] {
		return fmt.Errorf("unknown host id %q", id)
	}
	return nil
}

// ValidateHostIDs validates every entry in a hosts selector list.
func ValidateHostIDs(ids []string) error {
	for _, id := range ids {
		if err := ValidateHostID(id); err != nil {
			return err
		}
	}
	return nil
}
