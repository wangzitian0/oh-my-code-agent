package runtime

import "github.com/wangzitian0/oh-my-code-agent/internal/domain"

// sourceKey identifies one GenerationSourceEntry within a generation's
// Spec.Sources list well enough to compare "the same source" across two
// generations: (Concept, Source) is the pair every source-comparison in this
// package already keys on (compile_full.go's sourceEntryFingerprint folds
// in strictly more fields for content-addressing, but Concept+Source is
// what identifies WHICH decision changed, as opposed to whether its
// recorded Reason text also changed).
type sourceKey struct {
	Concept string
	Source  string
}

// includedSourceSet indexes gen's Spec.Sources by sourceKey, for entries
// where Included is true -- "what this generation actually activated,"
// keyed for a newly-included lookup.
func includedSourceSet(gen domain.Generation) map[sourceKey]domain.GenerationSourceEntry {
	out := make(map[sourceKey]domain.GenerationSourceEntry, len(gen.Spec.Sources))
	for _, s := range gen.Spec.Sources {
		if !s.Included {
			continue
		}
		out[sourceKey{Concept: s.Concept, Source: s.Source}] = s
	}
	return out
}

// DiffProposedChanges compares current and pending generations' Spec.Sources
// and returns one ProposedChange for every source that is newly Included in
// pending but was not Included in current -- i.e. every desired-state
// decision this activation would actually turn on. This is the real bridge
// between "what Compile decided" (Generation.Spec.Sources, already fully
// computed and ledgered as manifest content) and RequireConfirmation's input
// (activate.go/confirmation.go): cmd/omca's `omca activate` calls this to
// build the change list it then classifies and gates on, rather than asking
// an operator to describe changes by hand.
//
// Only source Concepts this package's own resolved/compiled vocabulary
// actually produces are classified into a ProposedChange (mcpServer, skill,
// instruction -- resolve.AssetKind's camelCase wire vocabulary,
// compile_full.go's resolvedAssetSources -- and permission,
// compile.go's resolveSandboxPermission). An Observation-derived source
// using the ontology's own snake_case Concept vocabulary (docs/ontology/
// README.md, e.g. "mcp_server") is a record of what was discovered/excluded
// at the M1 bootstrap-policy layer, never itself a desired-state activation
// decision, and is intentionally not turned into a ProposedChange here (see
// compile_full.go's resolvedAssetSources doc comment for why these are two
// separate vocabularies for two separate kinds of record). currentGen may be
// the zero value (Generation{}) for a host's first-ever activation -- an
// empty Spec.Sources list correctly means "everything in pending is newly
// included."
func DiffProposedChanges(currentGen, pendingGen domain.Generation, host string) []ProposedChange {
	before := includedSourceSet(currentGen)

	var changes []ProposedChange
	for _, s := range pendingGen.Spec.Sources {
		if !s.Included {
			continue
		}
		key := sourceKey{Concept: s.Concept, Source: s.Source}
		if _, already := before[key]; already {
			continue
		}

		detail := map[string]string{}
		if s.Reason != "" {
			detail["reason"] = s.Reason
		}

		switch s.Concept {
		case "mcpServer":
			changes = append(changes, ProposedChange{Kind: ChangeEnableMCPServer, AssetID: s.Source, Host: host, Detail: detail})
		case "skill":
			changes = append(changes, ProposedChange{Kind: ChangeSelectReviewedSkill, AssetID: s.Source, Host: host, Detail: detail})
		case "instruction":
			changes = append(changes, ProposedChange{Kind: ChangeSelectReviewedInstruction, AssetID: s.Source, Host: host, Detail: detail})
		case "permission":
			changes = append(changes, ProposedChange{Kind: ChangeExpandAccess, AssetID: s.Source, Host: host, Detail: detail})
		default:
			// An Observation-derived or capability-gap source, or any other
			// concept this compiler's resolved/compiled vocabulary does not
			// itself produce -- not a desired-state activation decision, so
			// not classified (see doc comment above).
		}
	}
	return changes
}
