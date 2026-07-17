// Package effective computes the Effective Graph (docs/architecture/README.md
// §5.2) for one host from its Observed Graph ([]domain.Observation, produced
// by internal/observe) and its versioned Knowledge Pack
// (internal/knowledge's loaded domain.HostKnowledge), and exposes all three
// core graphs — Observed, Effective, and Desired (internal/resolve's
// ResolvedState) — through one consistent, queryable [Graphs] type
// (issue #21, PR-17).
//
// # Why a sibling package, not an internal/resolve extension
//
// internal/resolve (PR-13) resolves the Desired Graph: host-neutral intent
// from Profiles, Activation, policy, and Exceptions, with no native host
// path at all (docs/architecture/README.md §5.3). This package resolves the
// Effective Graph: native, per-host, per-concept precedence over physical
// sources actually discovered on disk (docs/architecture/README.md §5.2;
// docs/ontology/README.md §3.2's "Native" resolution contract). The inputs,
// the merge vocabulary (ontology.MergeOperator, not domain.Intent), and the
// central discipline (never guess a winner the Knowledge Pack has not
// qualified) are all different from intent resolution, so this is a new
// package rather than a mode of internal/resolve — matching the issue's own
// framing of "a genuinely different resolution." It imports internal/resolve
// only to embed a DesiredGraph wrapper in [Graphs], never the reverse.
//
// # Pipeline
//
// [ComputeEffectiveGraph] is the entry point:
//
//	extract per-concept Candidates from Observations (extract.go)
//	  -> Identity Matcher: group Candidates into logical entities,
//	     preserving genuine cross-entity ambiguity rather than guessing
//	     (identity.go)
//	  -> per concept: either compose (CONCAT_ORDERED spans every logical
//	     entity for the concept, compose.go) or resolve each logical
//	     entity's group independently against its host's declared
//	     PrecedenceProgram (merge.go)
//	  -> emit EffectiveEntry values, each carrying provenance (selected
//	     source, ignored sources, the program/operator applied) and an
//	     EvidenceLevel, plus unresolved Conflicts and AmbiguousIdentities
//	     that were deliberately not guessed
//	  -> detect duplicate logical capabilities across built-in, MCP, and
//	     plugin tool sources (duplicate.go)
//
// # The central discipline: UNKNOWN is safer than a guessed adapter
//
// A LogicalGroup with more than one distinct-content Candidate is a genuine
// collision. This package resolves it to a concrete winner only when BOTH:
//
//  1. the host's Knowledge Pack declares a PrecedenceProgram for the concept
//     naming one of the nine closed docs/ontology/README.md §3.1 operators
//     (an absent program, or an Operator string that is not one of the nine
//     — including the literal word "UNKNOWN" — is never guessed past); and
//  2. the same Knowledge Pack's capabilities[concept].resolve is EXACT or
//     COMPATIBLE (docs/knowledge/README.md §5) — a Pack may name the
//     documented best-fit operator for a concept it has not yet qualified
//     for automated resolution (exactly what
//     knowledge/hosts/claude-code/cli/2.1/manifest.json and
//     knowledge/hosts/codex/cli/0.144/manifest.json do today: every
//     concept's resolve capability is honestly UNKNOWN), and this package
//     must not silently promote that into a confirmed decision.
//
// Both gates absent or failing produces a [Conflict], never a guess. This is
// exactly why this package reproduces fixtures/{claude-code,codex}/*/*/
// expected-effective.json's selectedSource: "UNKNOWN" entries for every
// genuine multi-source collision in the committed fixture corpus (see
// fixture_test.go) while still implementing every operator's real,
// deterministic merge behavior end-to-end against synthetic Knowledge Packs
// that DO declare resolve: EXACT (merge_test.go) — the gate is about what
// today's real Knowledge Packs have earned, not about whether this package
// knows how to apply the operator.
package effective
