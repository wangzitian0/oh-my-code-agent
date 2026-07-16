# ADR 0002: Ownership Levels

Status: accepted

## Context

OMCA observes, generates, and sometimes writes into files it does not fully
control: isolated generation artifacts it creates from scratch, native global
configuration it must never treat as an implicit parent, repository-shared
files a host also reads directly, and OS-level state (credential stores,
`/etc`, MDM policy) it can never touch (init.md, "Trust Boundary" and
"Non-goals"; `docs/architecture/runtime.md` §2, §10). Every artifact, drift
finding, and generation-graph edge needs one unambiguous ownership
classification so the report never implies a write guarantee OMCA cannot
back, and so the compiler and reconciler know which paths they are allowed to
touch at all (`docs/architecture/README.md` §5.4 "Every edge contains Adapter
ID, Knowledge digest, mapping relation, and ownership").

`docs/architecture/README.md` §10 already defines five ownership levels and a
v1 preference. This ADR freezes that definition as an accepted decision (it
was previously only descriptive architecture text) and makes explicit where
each level applies across the v1 surfaces described in `runtime.md` §5, §7,
and §10, so later PRs implementing the Runtime Compiler, Reconciler, and
Native Observer do not have to re-derive it.

## Decision

OMCA uses exactly five ownership levels for every artifact, field, or state
class it classifies. These match `docs/architecture/README.md` §10 verbatim:

```text
managed      OMCA owns the complete generated artifact
patched      OMCA owns specific fields in an external artifact
observed     OMCA reports but does not write
passthrough  OMCA parses around and preserves the native block
external     another authority owns the state
```

Where each applies in v1:

- **managed** — Everything OMCA generates inside an isolated generation's
  `hosts/<host>/<surface>/` tree: the bootstrap and later compiled host
  configuration, conservative permission defaults, the OMCA MCP server
  registration, and rendered project-loadable Instructions the adapter
  supports (`runtime.md` §3, §5.3). This is the primary v1 write path; v1
  prefers `managed` artifacts inside isolated generations over writing
  anywhere else (`architecture/README.md` §10).
- **patched** — Owning specific fields inside an artifact OMCA does not fully
  generate. V1 does **not** exercise this on the first end-to-end path:
  "persistent patching of native global files is outside the first end-to-end
  path" (`architecture/README.md` §10). It is reserved for a later milestone
  where OMCA must inject a narrow, reviewed field into an otherwise
  externally- or host-owned file without regenerating the whole file. No v1
  MVP scenario (init.md "MVP Acceptance Scenario") requires it.
- **observed** — OMCA reports but never writes. This is the v1 default for
  every native global configuration directory in the threat model (`~/.codex`,
  `~/.claude`, `~/.config/opencode`, `~/.agents/skills`, and system/`/etc`
  sources; `runtime.md` §2), for repository sources before an explicit
  activation decision (Instructions, Skills, MCP servers, Hooks, Plugins/
  Extensions per the table in `runtime.md` §10), and for any host or version
  outside a qualified Knowledge Pack's range (ADR 0004).
- **passthrough** — OMCA parses around and preserves the native block
  byte-for-byte inside an otherwise-managed artifact: unknown vendor fields,
  comments, and ordering that round-trip through a generation without
  semantic interpretation (init.md invariant "observe never writes or executes
  discovered assets"; `docs/knowledge/README.md` capability vocabulary
  `OPAQUE`). Applies wherever a Knowledge Pack marks a concept/operation
  `OPAQUE` for a given host and version.
- **external** — Another authority owns the state outright and OMCA never
  writes or copies it: the OS credential store, `/etc` and MDM-managed policy,
  a root-administered or replaced host binary, and repository-owner-committed
  files the worktree opened (init.md "Non-goals", "Trust Boundary"; `runtime.md`
  §2, §8). Credentials specifically are always `external` — see ADR 0003.

A single artifact class does not change ownership level based on convenience;
it changes only when a Knowledge Pack qualification or an explicit ADR
supersession raises or lowers it (e.g., a future ADR could move a specific
repository MCP field from `observed` to `patched` once a fixture proves it
safe — see `docs/knowledge/README.md` §5, "reconcileMode").

## Alternatives Considered

- **Three levels (owned / observed / external) instead of five.** Rejected:
  collapsing `managed` and `patched` would hide the difference between "OMCA
  generated this whole file" and "OMCA edited one field in a file it does not
  otherwise control," which matters for both risk review and rollback (a
  `patched` write must never clobber unrelated native content). Collapsing
  `passthrough` into `observed` would hide that a passthrough field still
  lives inside a `managed` artifact and must round-trip, unlike a purely
  observed native file OMCA never touches.
- **A single per-host "supported" flag instead of per-artifact ownership.**
  Rejected: `docs/knowledge/README.md` §5 already establishes "there is no
  host-wide 'supported' flag" — capability and ownership are per concept and
  operation. A host-wide flag would let one well-supported field imply write
  safety for an unrelated, unqualified one.
- **Let `patched` cover v1's repository-source table (Instructions, Skills,
  MCP, Hooks) instead of `observed`.** Rejected: `runtime.md` §10 explicitly
  scopes repository sources to cataloging and risk review, not field-level
  writes into files also read directly by the host; treating them as
  `patched` in v1 would overstate a write guarantee no fixture has proven
  (`docs/knowledge/README.md` §10, qualification suite).

## Consequences

- The Runtime Compiler and Reconciler must tag every artifact and generation
  edge with one of these five levels, and the report/drift layer must render
  the level so a user never mistakes `observed` for a promise of control
  (`architecture/README.md` §5.4).
- `patched` exists in the vocabulary from v1 but has zero production use until
  a later milestone proves a fixture-backed narrow-field write; this keeps the
  design forward-compatible without pretending v1 does something it does not.
- Knowledge Pack qualification (ADR 0004) is the only mechanism that can move
  a capability toward `managed`/`patched` for a previously `observed` or
  `passthrough` field; an LLM-authored proposal alone cannot promote ownership
  (init.md invariant: "unknown behavior cannot be promoted to managed by an
  LLM").
- Reporting and MCP tools can rely on one closed enum across the whole
  codebase instead of ad hoc per-adapter labels.
