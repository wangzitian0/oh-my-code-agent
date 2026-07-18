// Package assurance gathers evidence and classifies guarantee levels
// (docs/architecture/README.md §4's Assurance Engine component; issue #26,
// PR-22, "Evidence E0-E3 + verify").
//
// # Scope: E0-E3 only
//
// internal/observe already produces E0 (DISCOVERED) and E1 (PARSED)
// evidence on every domain.Observation (observe/doc.go); internal/effective
// already carries that evidence level through to every EffectiveEntry and
// Conflict (effective/doc.go). This package does not redo either of those
// jobs. Its job is the other half issue #26 names "verify": re-deriving
// each conclusion's evidence level so it is never stronger than what this
// repository can actually back --
//
//   - E2 (RESOLVED): only when a Knowledge Pack's qualified resolve
//     capability (EXACT/COMPATIBLE) actually ran a real merge/composition
//     operator for that entry ([VerifyGraph], qualifiedResolutionRan);
//   - E3 (HOST_REPORTED): only for a cell [Ceilings] documents an actual
//     native introspection surface for -- today, only the host binary's own
//     --version output ([HostVersionEvidence]); every other cell this
//     repository's own committed fixtures/knowledge/manifest.json evidence
//     establishes has no such surface, and stays capped at E1.
//
// E4 (BEHAVIOR_PROBED, canary probes) and E5 (EXTERNALLY_PROVEN) are out of
// this PR's scope entirely -- issue #26's own title is "Evidence E0-E3 +
// verify" -- and this package never produces either.
//
// # The evidence-ceiling table
//
// [Ceilings] (ceiling.go) is the per-host, per-concept anti-drift rule the
// round-2 audit of issue #26 added: docs/architecture/evidence-ceiling.md is
// its human-reviewable mirror, and the two are tested to never diverge
// (ceiling_test.go). Every row is grounded in a finding already committed
// elsewhere in this repository (fixtures/README.md's full --help reviews,
// knowledge/hosts/*/*/*/manifest.json's capabilities.*.resolve values) --
// never in what an introspection surface might plausibly offer. Raising a
// cell requires new committed evidence landing alongside the table edit;
// lowering one never needs permission.
//
// # Evidence and Guarantee stay independent dimensions
//
// [VerifyGraph] only ever re-derives EvidenceLevel. It never touches
// Guarantee: "verification never upgrades ADVISORY behavior to
// enforcement" (issue #26's own acceptance criterion) is a structural
// property of this package, not a rule a caller must remember to uphold --
// Evidence answers "why do we believe this," Guarantee answers "what
// prevents it from changing" (docs/architecture/reporting.md §5), and
// re-deriving the answer to the first question is never license to also
// change the answer to the second.
package assurance
