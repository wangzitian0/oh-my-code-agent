package effective

import (
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// LookupProgram finds the one PrecedenceProgram hk declares for concept, by
// convention: PrecedenceProgram.ID is "<concept>.<name>" (docs/knowledge/
// README.md §4's illustrative example uses "codex.skills.discovery" —
// "<host>.<concept-plural>.<name>" — but since one HostKnowledge document is
// already scoped to a single host, this package uses the simpler
// "<concept>.<name>" form with the exact singular ontology concept ID, e.g.
// "instruction.concat-ordered", "mcp_server.union-by-id",
// "skill.replace-by-scope"). It reports ok=false when no program matches
// (this concept has no declared program at all) or when more than one
// program's ID matches the same concept prefix (an ambiguous Pack must not
// silently pick one) — both cases mean "no usable precedence program," the
// same outcome an explicitly UNKNOWN/invalid operator produces (see
// merge.go's ResolveGroup): this package never guesses which of two
// candidate programs governs a concept any more than it guesses which
// source wins a collision.
func LookupProgram(hk domain.HostKnowledge, concept string) (domain.PrecedenceProgram, bool) {
	prefix := concept + "."
	var found domain.PrecedenceProgram
	count := 0
	for _, p := range hk.PrecedencePrograms {
		if strings.HasPrefix(p.ID, prefix) {
			found = p
			count++
		}
	}
	if count == 1 {
		return found, true
	}
	return domain.PrecedenceProgram{}, false
}
