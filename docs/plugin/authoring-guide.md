# Plugin Authoring Guide

Status: accepted

## Goal

A third party can build and qualify an observation-tier adapter from this
document alone, without forking or reading unrelated parts of this
repository. Everything this guide describes is real, shipped code as of this
writing — the tier policy, the frozen contract, the out-of-process
transport, and the conformance suite all already exist; this guide only
walks through using them and is honest about the one place they do not yet
line up perfectly (see [Known gaps](#known-gaps)).

This document does not define policy. It cites and demonstrates:

- [`docs/adr/0005-plugin-distribution.md`](../adr/0005-plugin-distribution.md)
  — the frozen tier, distribution, and contract-versioning decision.
- [`docs/knowledge/README.md`](../knowledge/README.md) §12 "Governance" — the
  two-tier host-support policy.
- [`internal/plugin/contract.go`](../../internal/plugin/contract.go) — the
  frozen v1 `HostAdapter` interface.
- [`internal/plugin/manifest.go`](../../internal/plugin/manifest.go) —
  `PluginManifest`.
- [`internal/plugin/transport`](../../internal/plugin/transport/) — the M6
  out-of-process transport (PR-25, issue #29).
- [`internal/plugin/conformance`](../../internal/plugin/conformance/) — the
  one conformance suite every adapter is checked against.

A working, minimal, conformance-passing example lives at
[`examples/plugins/minimal-observer/`](../../examples/plugins/minimal-observer/)
and is what every code snippet below is taken from verbatim.

## Tier policy and the promotion path

Host support follows a frozen two-tier policy
(`docs/knowledge/README.md` §12 "Governance"; `docs/adr/0005-plugin-distribution.md`):

- **Tier 1 (first-party, qualified):** Claude Code and Codex today. Their
  adapters compile into the `omca` binary, their maintainers keep Knowledge
  fresh and fixtures green, and they may write (`MANAGED`/`PATCHED`) native
  artifacts.
- **Tier 2 (knowledge-only / observation):** every other host. Its Knowledge
  Packs may go `DUE` or `STALE` without blocking a release, and it stays at
  the observation tier — reporting, never writing.

**A new adapter always starts at Tier 2.** There is no shortcut to Tier 1:
`docs/knowledge/README.md` §12 states the promotion path plainly —
"promoting a capability requires an adapter plugin with fixtures from a
maintainer or the community." Concretely, that means: build and qualify a
Tier 2 adapter first (this guide), accumulate real fixtures against a real
host, and only then propose promoting specific capabilities through the
Knowledge Candidate review process `docs/knowledge/README.md` §8–§9
describes — a decision repository maintainers make per capability, never
something an adapter grants itself by declaring a higher `Capability` level.

This guide is entirely about the first step: building and qualifying the
Tier 2 adapter that makes promotion possible at all.

### Why first-party adapters cannot shortcut this

`docs/adr/0005-plugin-distribution.md` decision 1 states that even Claude
Code's and Codex's adapters, though compiled into the same `omca` binary,
"may use only the public plugin contract... They may not import or call
private core packages... directly, even though nothing at the Go compiler
level would stop it." Decision 2 makes this a CI-enforced property, not an
honor-system one: **`internal/plugin/importboundary_test.go`
(`TestImportBoundary`) is a real, already-running test** that shells out to
`go list -deps` for every internal package and fails the build if anything
outside `internal/adapters/**` itself depends on `internal/adapters/**`. It
is not a future TODO — it runs in this repository's CI today, and its own
doc comment records that it caught a real seeded violation during
development.

The practical consequence for a third-party author: the contract in
`internal/plugin/contract.go` is genuinely the entire surface available to
you. A first-party adapter cannot see anything more, so there is nothing an
external plugin is structurally prevented from doing that a first-party one
can do.

## The frozen v1 contract

Every adapter — first-party or external — implements exactly this interface
(`internal/plugin/contract.go`):

```go
type HostAdapter interface {
    ID() AdapterID
    Detect(context.Context, DetectRequest) ([]HostInstance, error)
    Capabilities(context.Context, HostInstance) (CapabilityManifest, error)
    Observe(context.Context, ObserveRequest) (ObservationSet, error)
    Resolve(context.Context, ResolveRequest) (HostEffectiveState, error)
    Compile(context.Context, CompileRequest) (ArtifactSet, error)
    Verify(context.Context, VerifyRequest) (EvidenceSet, error)
    Launch(context.Context, LaunchRequest) error
}
```

and declares itself with a `PluginManifest` (`internal/plugin/manifest.go`):

```go
type PluginManifest struct {
    AdapterID       AdapterID
    AdapterVersion  string
    ContractVersion string
    Hosts           []HostSelector
    KnowledgePacks  []KnowledgeRef
    Fixtures        []FixtureRef
}
```

Nothing in this guide asks you to implement anything beyond these two types
and the request/response types `contract.go` defines alongside them. If you
find yourself wanting a capability the contract does not expose, that is a
contract gap to raise, not something to work around by reaching into a
private package — there is no private package to reach into from outside
this repository anyway.

### What a Tier 2 / observation-only adapter must implement

An adapter that starts (as every adapter does) at Tier 2 needs only four
methods to do real work:

| Method | Why an observation-only adapter needs it |
|---|---|
| `ID` | Bare identity accessor. |
| `Detect` | Find installed host instances without executing anything. |
| `Capabilities` | Declare, per concept, what this adapter can do — the vocabulary in `docs/knowledge/README.md` §5. |
| `Observe` | Inventory native/runtime sources. Zero writes, zero execution — this is the whole point of Tier 2. |

`Resolve`, `Compile`, `Verify`, and `Launch` exist in the interface because
the contract is one frozen shape for every tier (`ADR 0005` decision 3): a
Tier 2 adapter is allowed to answer any of them with
`plugin.ErrUnsupportedOperation` for a concept it does not support —
"the host has no corresponding operation or concept at all"
(`internal/plugin/errors.go`'s own doc comment on `ErrUnsupportedOperation`).
This is not a workaround; it is the taxonomy working as designed, and
`internal/plugin/conformance.Run` explicitly proves this error path when an
adapter's own `CapabilityManifest` declares a concept `UNSUPPORTED`
(see [Known gaps](#known-gaps) for the one real caveat on this).

## Building the example: `examples/plugins/minimal-observer`

The example is deliberately small: two files implementing the adapter
(`adapter.go`, ~200 lines with comments), one wiring `main.go`, and one
embedded fixture (`testhost/marker.json`) standing in for "a host is
installed here." It never probes, execs, or otherwise depends on any real
software on the machine it runs on — including the real `codex`/`claude`
CLIs that may well be installed on your own machine. Copy the directory as a
starting point; nothing in it assumes your machine looks like anything in
particular.

### Detect: a synthetic host, on purpose

```go
//go:embed testhost/marker.json
var markerJSON []byte

func (a *Adapter) Detect(_ context.Context, _ plugin.DetectRequest) ([]plugin.HostInstance, error) {
    return []plugin.HostInstance{a.host}, nil
}
```

A real adapter's `Detect` looks for its host's actual installed binary or
config directory (still without executing anything) and returns zero
instances when it is genuinely absent. This example ships its "evidence"
embedded in its own binary (`testhost/marker.json`) instead, so it detects
the exact same host on every machine, every time — no setup step for a
reader to get wrong, and no dependency on anything actually installed.

### Capabilities: Tier 2, declared honestly

```go
conceptFile: {
    Discover: plugin.CapabilityExact, Parse: plugin.CapabilityUnsupported,
    Normalize: plugin.CapabilityUnsupported, Resolve: plugin.CapabilityPartial,
    Compile: plugin.CapabilityPartial, Verify: plugin.CapabilityPartial,
    ReconcileMode: plugin.ReconcileObserved,
},
conceptMCP: {
    Discover: plugin.CapabilityUnsupported, /* ...every operation UNSUPPORTED... */
    ReconcileMode: plugin.ReconcileObserved,
},
```

`conceptFile`'s `ReconcileMode: OBSERVED` is the actual definition of
"observation-only" (`docs/knowledge/README.md` §5): "OMCA reports but does
not write." `conceptMCP` stands in for a concept a real host might have that
this synthetic one simply does not — every operation on it reports
`ErrUnsupportedOperation`, which the guide's example test
(`TestAdapter_UnsupportedConcept_ReportsErrUnsupportedOperation`) checks
directly.

### Observe: a couple of file-based sources, read-only

```go
obs, err := adapter.Observe(ctx, plugin.ObserveRequest{
    Host: host, Roots: []string{dir}, // dir contains settings.json and skills/deploy/SKILL.md
})
```

`Observe` walks the given roots, reads each file exactly once to compute a
digest, and never writes, creates, removes, or executes anything — the
zero-side-effects property `internal/plugin/conformance.Run` proves
mechanically (see below). `examples/plugins/minimal-observer/adapter_test.go`'s
`TestAdapter_Observe_TwoFileSources` demonstrates this over two synthetic
files.

### Wiring `transport.Serve` in `main()`

This is the entire out-of-process side of the story
(`internal/plugin/transport`, PR-25/issue #29):

```go
func main() {
    adapter := NewAdapter()
    manifest := plugin.PluginManifest{
        AdapterID:       adapter.ID(),
        AdapterVersion:  "0.1.0",
        ContractVersion: plugin.ContractVersion,
        Hosts: []plugin.HostSelector{
            {HostID: "demo-observer-host", Surfaces: []string{"cli"}, VersionRange: ">=0.0.0"},
        },
    }
    if err := transport.Serve(os.Stdin, os.Stdout, manifest, adapter); err != nil {
        fmt.Fprintln(os.Stderr, "minimal-observer:", err)
        os.Exit(1)
    }
}
```

`transport.Serve` reads newline-delimited JSON-RPC 2.0 requests from
`os.Stdin`, dispatches each to `adapter` through the exact `plugin.HostAdapter`
interface, and writes responses to `os.Stdout` — the same framing
`internal/mcp/server.go` already established for this codebase's other
stdio server, not a second convention. This `main()` is the entire
integration point: nothing else in the binary needs to know it is being
driven remotely.

### Running the conformance suite against it, over the real transport

The core loads an external adapter through `transport.RemoteAdapter`, which
itself implements `plugin.HostAdapter` by speaking the wire protocol to an
already-started subprocess. Because `RemoteAdapter` is just another
`plugin.HostAdapter`, it can be driven through
`internal/plugin/conformance.Run` exactly like an in-process adapter — this
is the mechanism that proves the transport preserves contract parity across
the process boundary, mirroring PR-25's own
`TestRemoteAdapter_ConformanceParity` pattern
(`internal/plugin/transport/conformance_test.go`):

```go
cmd := exec.Command(minimalObserverBinary) // the real, compiled example binary
remote, err := transport.NewRemoteAdapter(cmd)
if err != nil {
    t.Fatalf("NewRemoteAdapter: unexpected error: %v", err)
}
defer remote.Close()

conformance.Run(t, remote)
```

`examples/plugins/minimal-observer/conformance_test.go`'s
`TestMinimalObserver_ConformanceOverRealTransport` is exactly this, built
from the example's own real source and run as a genuine OS subprocess. This
test passing — `go test ./examples/plugins/minimal-observer/...` from the
repository root — **is** this guide's "its example skeleton compiles and
passes conformance" proof; nothing here is claimed only in prose.

## Qualification checklist

`internal/plugin/conformance.Run` (`internal/plugin/conformance/conformance.go`)
is the one conformance suite every adapter — first-party or external — is
checked against. It calls exactly six sub-checks, in this order; the table
below has exactly one row per sub-check, by name, so an adapter author can
tell at a glance which real, running code each checklist item maps to.

| # | Sub-check (`conformance.go`) | What it proves | Your adapter passes when |
|---|---|---|---|
| 1 | `runID` | `ID()` returns a non-empty `AdapterID`. | `ID()` never returns `""`. |
| 2 | `runDetect` | `Detect` returns at least one `HostInstance` in the conformance environment, with no error. | Your adapter can always detect *something* in the environment `conformance.Run` runs it in (a fixed `HostInstance`, embedded evidence, or a fixture the test itself sets up — never a probe of the real machine running CI). |
| 3 | `runCapabilities` | Every `Capability` and `ReconcileMode` value your `CapabilityManifest` declares is one of the closed, valid enum values. | You only ever use the vocabulary in `docs/knowledge/README.md` §5 (`EXACT`/`COMPATIBLE`/`PARTIAL`/`OPAQUE`/`UNKNOWN`/`UNSUPPORTED`) and `internal/plugin/reconcile.go` (`MANAGED`/`PATCHED`/`OBSERVED`/`OPAQUE`/`BLOCKED`). |
| 4 | `runNotDetectedTaxonomy` | `Capabilities`, `Observe`, `Resolve`, `Compile`, `Verify`, and `Launch` all return `plugin.ErrNotDetected` (via `errors.Is`) when called against a `HostInstance` your own `Detect` did not return. | Every per-host method checks the given `HostInstance` against what `Detect` reports before doing anything else. |
| 5 | `runObserveZeroSideEffects` | `Observe`, run against a sandboxed temp tree containing a canary script, performs zero writes and zero execution — the tree is byte-for-byte unchanged afterward, and the canary's marker file was never created. | `Observe` only ever calls `os.ReadFile`/`os.Open` (or read-only directory walks) on its `Roots` — never `os.WriteFile`, `os.Create`, `os.Remove`, or `exec.Command`. |
| 6 | `runResolveCompileVerifyLaunch` | At least one concept your `CapabilityManifest` declares usable (`Resolve`/`Compile` not `UNSUPPORTED`, `ReconcileMode` not `BLOCKED`) passes a full `Observe → Resolve → Compile → Verify → Launch` happy path with no error, `Resolve`'s result `Host` field matches the detected host, and **if** your manifest separately declares a concept `UNSUPPORTED` for `Resolve`, `Resolve` on it returns `plugin.ErrUnsupportedOperation`; **if** it declares a concept `BLOCKED`, `Compile`/`Launch` on it return `plugin.ErrCapabilityDenied`. | At least one concept works end to end (see [Known gaps](#known-gaps) below for what this requires of even a pure observation adapter), and any concept you mark `UNSUPPORTED`/`BLOCKED` actually returns the matching sentinel error rather than succeeding or erroring some other way. |

Every one of these six names — `runID`, `runDetect`, `runCapabilities`,
`runNotDetectedTaxonomy`, `runObserveZeroSideEffects`, and
`runResolveCompileVerifyLaunch` — is checked mechanically, not just written
down here: `internal/plugin/conformance/checklist_test.go`'s
`TestRun_SubChecksMatchKnownList` parses `conformance.go`'s actual `Run`
function body and fails if the real sub-checks and this file's manually
maintained list of names ever disagree, and
`TestRun_SubChecksMatchDocumentedChecklist` fails if any of those names stop
appearing in this document. If a future sub-check is added to `Run` without
both of those being updated, CI fails — this checklist cannot silently
drift from the suite it claims to describe.

To run the full suite against your own adapter, over the real transport,
copy `examples/plugins/minimal-observer/conformance_test.go`'s pattern:
build your adapter's binary, launch it as a subprocess, wrap it with
`transport.NewRemoteAdapter`, and call `conformance.Run(t, remote)`.

## Known gaps

This project's own convention is to track a capability gap honestly rather
than gloss over it. Two apply here:

1. **`conformance.Run` cannot certify a zero-write-capability adapter
   today.** Sub-check 6 (`runResolveCompileVerifyLaunch`) `t.Fatal`s if no
   concept in your `CapabilityManifest` satisfies "`Resolve` and `Compile`
   are not `UNSUPPORTED`, and `ReconcileMode` is not `BLOCKED`" — it always
   requires at least one concept to complete a real
   `Resolve`/`Compile`/`Verify`/`Launch` round trip successfully. An adapter
   whose host genuinely supports *no* write-shaped concept at all (every
   concept `UNSUPPORTED` on `Resolve`/`Compile`) cannot pass `conformance.Run`
   as it exists today, even though such an adapter would be a completely
   legitimate Tier 2 citizen under the tier policy itself. This example
   works within that requirement by keeping exactly one concept
   (`conceptFile`) functional end to end — with `ReconcileMode: OBSERVED`,
   so the core never actually persists anything it renders — purely to
   satisfy this structural requirement, and reports
   `ErrUnsupportedOperation` honestly for the other concept
   (`conceptMCP`) it does not support. If your real host has no concept you
   can honestly wire up this way, raise it with maintainers before
   assuming `conformance.Run` will pass; widening the suite to accept a
   fully-unsupported adapter is a real gap this guide does not solve.

2. **A synthetic example cannot use a real canonical host ID.** `HostSelector`
   (`internal/plugin/manifest.go`) is validated against a closed vocabulary
   (`internal/domain/host.go`'s `KnownHostIDs`, backing
   `docs/ontology/README.md` §4's Host Registry) — every entry in it names a
   real product (Claude Code, Codex, OpenCode, Cursor, GitHub Copilot,
   Antigravity CLI, Pi, OpenClaw, Hermes Agent). This example's host is
   entirely invented, so its manifest deliberately uses the host ID
   `demo-observer-host`, which is *not* in that vocabulary — and therefore
   its `PluginManifest` never passes `PluginManifest.Validate()` or
   registers through `plugin.Registry.Register()`; the example only ever
   goes through the transport handshake and `conformance.Run`, neither of
   which validates the manifest. A genuine third-party adapter for a host
   not yet in `KnownHostIDs` needs that host added to the ontology's Host
   Registry first — a real governance step this guide does not walk
   through, since it is a decision about ontology content, not about plugin
   code.

## Where this fits with everything else

- The tier policy and promotion path are frozen by
  `docs/adr/0005-plugin-distribution.md` and stated in
  `docs/knowledge/README.md` §12; this guide does not restate them as new
  policy, only shows how to act on them.
- `docs/architecture/README.md` §9 ("Core Interfaces") is the architectural
  description of the same `HostAdapter`/`PluginManifest` shapes this guide
  walks through operationally.
- `docs/project/roadmap.md`'s M6 milestone ("Publish plugin authoring
  documentation and the qualification checklist... Port one host... through
  the external plugin path, observation tier first") is what this guide and
  its example satisfy.
