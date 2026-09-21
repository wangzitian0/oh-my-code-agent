# ADR 0006: Host Tiers and Conditional HOME Virtualization

Status: accepted

Supersedes nothing. Decides a question
[ADR 0001](0001-runtime-isolation.md) explicitly deferred and
[product requirements](../product/requirements.md) §10 tracked as open
question 6.

## Context

Until PR #109, `internal/shim.Plan.Exec` virtualized `HOME` unconditionally
for every managed host:

```go
overrides := map[string]string{
    p.NativeHomeEnvVar: p.NativeHomeDir,
    "HOME":             p.VirtualHomeDir,
    "OMCA_REAL_HOME":   p.RealHomeDir,
}
```

That single assignment is what made the charter invariant "native global
configuration is never an implicit runtime parent" true in practice. It is
the mechanism `AGENTS.md` calls "load-bearing" and
[`runtime.md`](../architecture/runtime.md) §7.1.1 describes as closing the
`$HOME/.agents/skills`-style native config leak.

PR #109 made it conditional on a new `Plan.CanVirtualizeHome`, fed by a new
`domain.DefaultHostCapability(host)` that returns `false` for `claude-code`.
The change was correct in substance and wrong in process: it shipped as a
side effect of a hub feature, with no ADR, no requirements update, and no
change to any document that still promised complete isolation. This ADR
records the decision that PR #109 made implicitly, and fixes the claims that
went stale when it landed.

The substantive reason a per-host retreat is needed:

- Codex separates configuration (`CODEX_HOME`) from credentials, so a
  generation can own the former while
  [ADR 0003](0003-credential-handling.md)'s fallback order resolves the
  latter. A virtual `HOME` costs nothing the adapter cannot recover.
- Claude Code couples account and OAuth state to the OS Keychain and to a
  monolithic user state file. `runtime.md` §7.2 already fixed the binding
  constraint that follows: "isolation must not force a fresh login for every
  generation". A virtual `HOME` violates exactly that constraint, and
  ADR 0003 forbids the obvious workarounds — copying credential material into
  a generation, or broad-symlinking the native home.

So for Claude Code the choice is not "isolated or not isolated". It is
"inherit the user-global scope, or break the user's login on every
generation". PR #109 chose the first. This ADR accepts that choice and makes
its cost visible instead of silent.

`AGENTS.md`'s scope-discipline section independently argues against chasing
the long tail of `$HOME`-virtualization breakage (asdf shim dispatch, host
self-checks) when it does not affect whether the managed concepts are
correctly observed and activated. Tier 2 is the same trade-off applied to a
host whose credential model, not an incidental tool, is what breaks.

## Decision

1. **Three tiers, named in `domain.HostTier`.**

   | Tier | ID | HOME virtualization | User-global scope |
   |---|---|---|---|
   | 1 | `MANAGED` | Yes | Excluded |
   | 2 | `BRIDGE` | No | **Inherited in full** |
   | 3 | `OBSERVED` | N/A — not launched managed | Untouched |

   Codex is Tier 1. Claude Code is Tier 2. Every other host is Tier 3.

2. **The isolation invariant is scoped to Tier 1.** The charter invariant
   "native global configuration is never an implicit runtime parent" holds
   for Tier 1 hosts. For Tier 2 it does not, and no document, report, CLI
   string, or evidence file may state or imply otherwise.

3. **A Tier 2 host must state its residual load.** `runtime.md` §7.2 already
   required this for unexcludable repository assets: "the report states the
   residual load instead of claiming a clean runtime." That rule now governs
   the user-global scope too. Reporting "0 excluded" for a Tier 2 host
   without also stating that the entire native user-global scope is loaded is
   a violation of this ADR, not a cosmetic omission.

4. **`Managed` is not an isolation claim.** `mcp.HostStatus.Managed` means
   "this worktree has a compiled current generation for this host". Consumers
   that need to know whether the user-global scope was actually excluded read
   `Tier` and `UserGlobalIsolated`.

5. **Tier is a capability gap, not a qualification.** Tier 2 is recorded as
   an explicit capability gap under the roadmap's own working rule 4
   ("Capability gaps are tracked states"). It does not satisfy FR-7, and a
   Tier 2 host cannot be counted toward an M1 exit gate that asserts
   user-global exclusion. Existing qualification evidence that recorded
   Claude Code MCP exclusion as passing describes the pre-#109 mechanism and
   is superseded for any installed build that carries `CanVirtualizeHome`.

6. **Tier defaults are code today, Knowledge tomorrow.**
   `domain.DefaultHostCapability` is a hardcoded switch. That is acceptable
   only as the transitional form: a host's tier is a versioned host fact and
   belongs in a Knowledge Pack, under FR-3's rule that only a qualified Pack
   may normalize host behavior. Moving it is a follow-up gate, not optional
   cleanup.

## Alternatives Considered

- **Keep unconditional HOME virtualization for Claude Code.** Rejected: it
  forces a fresh login per generation, which `runtime.md` §7.2 fixes as a
  binding constraint, and the workarounds that would avoid the re-login are
  the ones ADR 0003 prohibits outright.
- **Copy or symlink the native Keychain-bound state into the generation.**
  Rejected by ADR 0003 §3 ("Prohibited regardless of order") and
  `runtime.md` §8. This is the alternative that makes Tier 2 look
  unnecessary, and it is exactly the one the credential ADR exists to
  prevent.
- **Drop Claude Code to Tier 3 (observed only).** Rejected: it is one of two
  first-party hosts and the one the primary user runs most. Observing it
  while refusing to manage anything would remove the repository-scoped
  selection and the report that do work, to avoid admitting one gap.
- **Report Tier 2 as unmanaged (`Managed: false`).** Rejected: it is
  managed — a generation is compiled, selected, and activated for it. The
  honest fix is a separate isolation field, not overloading `Managed` into a
  second meaning.
- **Leave the decision implicit in code, as PR #109 shipped it.** Rejected:
  [`docs/README.md`](../README.md) lists isolation as a frozen cross-cutting
  decision that must live in an ADR, and the product's entire value claim is
  a trustworthy report. A silently weakened invariant with unchanged
  documentation is the specific failure mode that makes a report untrustworthy.

## Consequences

- `README.md`, `init.md`, `requirements.md` (FR-7 and §10 question 6), and
  `runtime.md` §7.2 are corrected by the same change that adds this ADR;
  none of them may keep promising unconditional isolation.
- `mcp.HostStatus` gains `Tier` and `UserGlobalIsolated`. Any consumer that
  treated `Managed` as an isolation claim is now wrong in a way the schema
  makes visible, rather than wrong silently.
- `omca env`'s Tier 2 line and `omca_status`'s Tier 2 detail must both name
  the residual load. A future change that shortens either string back to a
  bare "managed" is a regression against decision 3.
- The M1 exit gate cannot be closed on Claude Code's user-global exclusion.
  It is a tracked gap with its own follow-up, which is a smaller and more
  honest statement than the one the repository made before this ADR.
- Moving tier facts into Knowledge Packs (decision 6) and qualifying what
  Tier 2's bridge actually governs are separate follow-up gates. This ADR
  does not claim either is done.
