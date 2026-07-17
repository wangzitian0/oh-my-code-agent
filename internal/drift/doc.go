// Package drift classifies machine-level Drift assertions and groups them
// by root cause into human-scale action cards (docs/architecture/reporting.md
// §6, "Drift Model", and §7, "Root-cause Aggregation").
//
// # Scope and PR-17 dependency
//
// This engine consumes a graph-shaped [Signal] input: one candidate
// difference between an expected value (from the DESIRED/CURRENT plane) and
// an observed/effective value (from the OBSERVED/HOST_EFFECTIVE plane), for
// one entity field at one host in one project's context. Per issue #22's
// round-3 scoping note (pinned META issue #37), PR-17 ("Resolver: produce
// real Observed/Effective/Desired graphs", issue #21) had not landed when
// this package was built, so this package has no compiled dependency on
// PR-17's graph type and cannot be integration-tested against its real
// output yet. Tests in this package build [Signal] values by hand,
// hand-shaped to plausibly resemble what PR-17's resolver will eventually
// emit, and are documented as fixture/synthetic wherever they stand in for
// that integration. Once PR-17 lands, a thin adapter translating its real
// Observed/Effective/Desired graph into []Signal is the intended integration
// point — this package's public surface (Classify, ClassifyAll, Group) does
// not need to change for that adapter to plug in.
//
// # Pipeline
//
// [ClassifyAll] turns a slice of [Signal] into a slice of [Assertion], each
// wrapping a domain.DriftAssertion (the frozen protocol shape from
// internal/domain/report.go) plus the structured project/host/adapter-version
// dimensions the grouping stage needs but the flat protocol type does not
// carry as separate fields. [Group] then aggregates those assertions into
// [ActionCard] values, keyed by (root cause, remediation, outcome class,
// adapter version) per reporting.md §7, with deterministic sample selection
// and a queryable full matrix on every card.
package drift
