// Package plugin defines the frozen host adapter contract, manifests, and the
// plugin registry (docs/architecture/README.md §9; ADR 0005 Plugin
// Distribution and Contract Versioning).
//
// Host adapters are plugins behind one frozen contract: the HostAdapter
// interface, the PluginManifest shape, and the capability and reconcile-mode
// vocabularies exported here. First-party adapters (claude-code, codex)
// compile into the omca binary but may use only this public contract package
// and internal/domain — never private core packages, and never each other.
// Core packages (internal/observe, internal/resolve, internal/reconcile,
// ...) reach adapters only through this package: they call Registry.Lookup
// for a HostAdapter and drive it through the interface below, never through
// an internal/adapters/* import. See importboundary_test.go for the
// mechanical check.
//
// The contract is designed to be transport-agnostic from v1: every method
// takes a context.Context and a request struct carrying an explicit
// InvocationContext, so the same operations can be called in-process (v1) or
// spoken over stdio by an out-of-process plugin (a day-one design constraint,
// qualified starting M6; see ADR 0005 item 3).
//
// Within ContractVersion's major version, the contract evolves additive-only:
// new optional operations, manifest fields, and capability vocabulary entries
// may be added without breaking an adapter written against an earlier minor
// revision. A breaking change requires a new major ContractVersion and ships
// with migration notes (ADR 0005 item 4).
package plugin
