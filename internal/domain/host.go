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

// ValidateHostIDs validates every entry in a hosts selector list. Entries
// must be unique, matching the `uniqueItems: true` constraint on
// `hostsSelector` in schemas/domain/common.v1alpha1.schema.json.
func ValidateHostIDs(ids []string) error {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if err := ValidateHostID(id); err != nil {
			return err
		}
		if seen[id] {
			return fmt.Errorf("duplicate host id %q in hosts selector", id)
		}
		seen[id] = true
	}
	return nil
}
