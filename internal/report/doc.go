// Package report computes one immutable [Artifact] per worktree and
// projects it to stable JSON and human text (docs/architecture/reporting.md,
// issue #23/PR-19). It is the M3-closing package: every other M3 package
// (internal/effective's Observed/Effective/Desired graphs, PR-17;
// internal/drift's classification and root-cause grouping, PR-18;
// internal/knowledge's Pack lifecycle, PR-05/PR-28) is a data source this
// package composes, never recomputes.
//
// # One artifact, several projections
//
// [Build] is the single entry point that computes an [Artifact]: per-host
// Observed/Effective/Desired graphs (via internal/effective), the graph's
// drift signals classified and grouped into root-cause [DriftCard]s (via
// [BuildDriftSignals] and internal/drift), per-host Knowledge lifecycle
// status, per-host context-cost estimates, and the cross-host
// duplicate-capability section. Every CLI query this package's doc comment
// section below names is a read-only projection of that one Artifact value —
// docs/architecture/reporting.md §10's "All read commands support stable
// JSON output. Human output is a projection of the same immutable report
// artifact" is implemented literally: [Artifact].Report nests the frozen
// protocol envelope (schemas/protocol/report.v1alpha1.schema.json) exactly
// as that closed schema declares it (apiVersion/kind/metadata/spec, nothing
// more — see [Artifact]'s own doc comment for why this is a named field
// rather than an anonymously embedded/flattened one), with this package's
// M3 additions (ActionCards, Hosts, DuplicateCapabilities, Debug) as
// siblings at Artifact's own top level. `omca report --json` therefore
// contains a byte-exact domain.Report document at its "report" key, and
// every human-text renderer in human.go reads only exported Artifact
// fields — it computes nothing a JSON consumer could not also read.
//
// # The PR-18-anticipated adapter
//
// [BuildDriftSignals] is the "thin adapter translating [PR-17's] real
// Observed/Effective/Desired graph into []Signal" internal/drift/doc.go's
// "Scope and PR-17 dependency" section names as its own intended integration
// point. It lives here, not in internal/drift, so internal/drift keeps its
// existing PR-18 dependency direction (no compiled dependency on
// internal/effective) and this package — which already depends on both —
// is the natural composition boundary.
//
// # CLI queries this package backs
//
//	omca report                          -> Build + human/JSON projection of the whole Artifact
//	omca drift                           -> Build + project ActionCards (list)
//	omca drift show <drift-id>           -> Build + project one ActionCard by its content-addressed ID
//	omca explain <concept> <id> [--trace] -> Explain, optionally expanding ResolverTrace/PhysicalSources/KnowledgeEvidence
//	omca matrix <drift-id>                -> Build + project one ActionCard's full Matrix
//	omca compare --native --current       -> ComparePlanes across two named planes
//	omca diff current pending             -> ComparePlanes (positional plane names)
//
// docs/architecture/reporting.md §10 additionally lists `omca status` and
// `omca observe`, already served by internal/mcp/status.go (PR-11) and
// internal/observe (PR-08) respectively — this package does not duplicate
// either.
package report
