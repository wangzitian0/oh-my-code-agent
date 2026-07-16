# ADR 0005: Plugin Distribution and Contract Versioning

Status: accepted

## Context

Host support is packaged as versioned adapter plugins behind one frozen
adapter contract; Claude Code and Codex are the first-party plugins, and every
other host integrates through the same contract or stays at the
knowledge/observation tier (init.md decision 16). Architecturally, host
adapters are plugins behind one frozen contract that own physical host
semantics and do not compose Profiles or classify cross-host Drift
(`docs/architecture/README.md` §9). The `HostAdapter` interface and
`PluginManifest` (declaring `AdapterID`, `AdapterVersion`, `ContractVersion`,
supported hosts, Knowledge Packs, and fixtures) are the actual contract
surface (`architecture/README.md` §9).

Two distribution questions need a frozen answer before code depends on them:
whether first-party adapters get a privileged shortcut around the contract
they ship inside the same binary as, and how the contract can evolve (adding
capability vs. breaking compatibility) without silently breaking either
first-party or, eventually, external adapters. The roadmap already commits to
"Freeze adapter plugin contract v1" in M0 and to qualifying "the out-of-process
plugin transport: an adapter as a separate executable speaking the versioned
contract over stdio" only in M6 (`docs/project/roadmap.md`, M0 and M6
deliverables). This ADR freezes both the in-process/first-party rule and the
versioning policy so the M6 work has a stable contract to qualify against
rather than one still being redefined.

## Decision

1. **First-party adapters compile in, but are contract-only.** The
   `claude-code` and `codex` adapters compile directly into the `omca` binary
   for v1 (init.md decision 16; `architecture/README.md` §9), but they may use
   only the public plugin contract — the `HostAdapter` interface, the
   `PluginManifest` shape, and whatever supporting types the contract package
   exports. They may not import or call private core packages (e.g.
   `internal/resolve`, `internal/drift`, `internal/reconcile`,
   `internal/artifact` internals) directly, even though nothing at the Go
   compiler level would stop it once code lives in the same binary.

2. **This restriction is meant to be mechanically enforced, not just
   documented.** It is intended to be checked by an import-boundary lint: a
   CI-run static check over the module's import graph that fails the build if
   any package under `internal/adapters/**` (or successor adapter packages)
   imports anything outside the plugin contract package(s) and shared stable
   domain types (`architecture/README.md` §6 layout: `internal/plugin/`,
   `internal/domain/`). This ADR does not specify the exact tool (e.g. a
   dependency-graph linter or a custom import-boundary check wired into
   `golangci-lint`/CI), only that first-party contract compliance must be a
   CI-checkable, not honor-system, property before it can be trusted for M6's
   external-plugin promise.

3. **Out-of-process transport is a day-one design constraint, a day-M6
   shipped capability.** The `HostAdapter` contract is designed from v1 to be
   transport-agnostic: the same operations must be expressible as direct
   in-process Go calls (first-party, v1) or as the identical contract spoken
   over stdio by a separate executable (external plugins). This shapes the
   contract's method signatures and manifest from the start (context-carrying
   requests/responses, no shared-memory or process-internal assumptions,
   `architecture/README.md` §9). However, the out-of-process transport itself
   is not a shipped, qualified capability until M6: "Qualify the out-of-process
   plugin transport... Port one host... through the external plugin path"
   (roadmap M6 deliverables). Before M6, the design constraint exists so the
   contract does not have to change shape later; it is not something a v1
   adapter author can rely on as working, tested infrastructure.

4. **Contract versioning policy.**

   - Within contract v1, changes are **additive-only**: new optional
     operations, new optional manifest fields, and new capability
     vocabulary entries may be added without breaking an adapter written
     against an earlier v1 minor revision. An adapter that does not implement
     a newly added optional operation continues to work; the core treats the
     absence as `UNSUPPORTED`/`OBSERVED` for that operation
     (`docs/knowledge/README.md` §5 capability vocabulary), never as a hard
     failure.
   - A **breaking change** (removing or changing the meaning of an existing
     operation, changing a required manifest field's type or semantics)
     requires a new major contract version (v2, ...). It cannot be made inside
     v1 under any justification, including a first-party adapter's
     convenience.
   - Every breaking change ships with migration notes for existing adapters:
     what changed, why, and the concrete steps an adapter maintainer (first-
     party or external) takes to move from the old major version to the new
     one. An adapter declares the contract version it targets via
     `PluginManifest.ContractVersion`, so the core can detect and reject (or
     run in a compatibility mode for, if one is later defined) an adapter
     built against an unsupported major version rather than silently
     misbehaving.

## Alternatives Considered

- **Let first-party adapters call private core APIs directly since they ship
  in the same binary.** Rejected: this would let `claude-code`/`codex` quietly
  depend on internals no external plugin can reach, meaning the contract M6
  needs to qualify would not actually be the contract the two most important,
  best-tested adapters exercise every day. The whole point of "first-party
  adapters... use only the public plugin contract" (`architecture/README.md`
  §9) is that they are the strongest possible dogfood for the contract
  external plugins will later depend on.
- **Enforce the contract-only rule by code review alone, with no automated
  check.** Rejected: code review catches this only as long as every reviewer
  remembers to check it on every adapter change; a lint that fails CI is the
  only way to make the guarantee durable as the codebase and contributor set
  grow, and the issue's acceptance bar expects this to be checkable, not
  aspirational.
- **Ship the out-of-process transport in M0/M1 alongside the in-process
  contract.** Rejected: the roadmap deliberately sequences this — M0-M5 focus
  on qualifying two first-party, in-process hosts; building and qualifying a
  second transport before the contract itself has proven stable against real
  hosts would risk freezing the wrong shape early, and M6 explicitly exists to
  do this once the contract has mileage (roadmap M6).
- **Allow breaking changes within v1 if "no external adapter exists yet
  anyway."** Rejected: first-party adapters are real consumers of the
  contract from M0 onward, and the additive-only rule is what lets the
  out-of-process transport constraint (item 3) hold — a contract that breaks
  freely pre-M6 cannot credibly be called "day-one design constraint" for
  something qualified later.
- **Version the contract per-operation instead of as one whole-contract major
  version.** Rejected: per-operation versioning would multiply compatibility
  matrices between adapters and core without a clear benefit at this scale;
  a single `ContractVersion` on the manifest, checked against a small number
  of accepted major versions, is simpler to reason about and matches the
  `PluginManifest` shape already defined (`architecture/README.md` §9).

## Consequences

- CI must include an import-boundary lint over `internal/adapters/**` (or
  successor first-party adapter packages) before those adapters are
  considered conformant; this is a concrete follow-up for the M0/M1
  implementation work, not left to reviewer memory.
- The `HostAdapter` interface and `PluginManifest` must be designed and
  reviewed with the out-of-process transport in mind from the first
  implementation, even though stdio transport code itself is M6 work.
- Any proposed change to `HostAdapter` or `PluginManifest` must be classified
  as additive or breaking before merge; a breaking change blocks on having
  migration notes ready, not just a version bump.
- M6's "port one host through the external plugin path" deliverable can
  proceed against a contract that has not changed shape since M0, because
  first-party adapters have been exercising the same public contract the
  whole time.
- A first-party adapter that is found importing private core APIs is a bug
  against this ADR, fixable by moving the needed capability into the public
  contract (if it is a legitimate adapter need) rather than by exempting the
  adapter from the boundary.
