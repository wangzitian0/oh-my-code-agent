# ADR 0001: Runtime Isolation Strategy and Launch Budget

Status: accepted

## Context

The founding pain this project removes is a coding-agent session that
implicitly inherits every user-global Skill, MCP server, Hook, Plugin, and
Instruction the moment it starts (init.md, "Product Statement";
`docs/architecture/runtime.md` §1-2). An MCP server cannot establish that
boundary because it only starts after the host has already begun resolving
configuration (`runtime.md` §3). Isolation must therefore happen in a small,
fast, pre-host bootstrap: a shim selects or creates a generation and `exec`s
the real host binary before the host reads any native global source
(`runtime.md` §3-4, init.md decisions 7-11).

This gives isolation a hard non-functional constraint: the bootstrap must be
fast enough that a programmer never notices a tax on every `codex` or `claude`
invocation, or the isolation will be worked around. The roadmap already commits
to this ("The launch budget is fixed in M0", `docs/project/roadmap.md`, M1 exit
gate commentary) and the M1 exit gate requires the measured overhead to "stay
inside the launch budget." No prior document fixes the actual numbers or the
machine they are measured on. This ADR fixes both, because M1's CI performance
gate needs a concrete, testable assertion, not a qualitative "tens of
milliseconds."

Per-host isolation mechanisms are already scoped and are not re-litigated here:

- Codex isolates through `CODEX_HOME` plus a virtual process `HOME`, with the
  real `HOME` restored for subprocesses via shell-environment policy
  (`runtime.md` §7.1).
- Claude Code isolates through a relocated configuration directory, session
  flags, a strict MCP mode, and a virtual process home as a fallback; which
  combination yields complete user-global exclusion is an open qualification
  question tracked separately, not decided by this ADR (`runtime.md` §7.2).

## Decision

1. **Reference machine.** The strict, CI-asserted launch-budget numbers in
   this ADR are measured on one documented reference machine:

   > Apple M-series MacBook Pro (M2 or later), macOS, local SSD, warm OS page
   > cache, generation cache explicitly cleared before each first-bootstrap
   > measurement ("cold generation cache").

   Numbers gathered on this machine are the strict evidence baseline referenced
   by the M1 exit gate and by any later CI performance test. Other hardware
   (older Intel Macs, Linux CI runners, developer laptops under load) is
   expected to vary; a separate, looser CI ceiling for flake tolerance may be
   defined later when the M1 performance test is implemented — that ceiling is
   an operational concern for the test harness, not a redefinition of the
   reference-machine target fixed here.

2. **Steady-state budget: <= 100ms.** On the reference machine, for an
   unchanged worktree with an existing, valid current generation, the wall-clock
   overhead contributed by OMCA — from the shim intercepting the `codex` or
   `claude` invocation, through generation selection, environment injection,
   and `exec` of the real host binary — must not exceed 100ms. This overhead
   excludes the real host binary's own startup time, since OMCA does not
   control or isolate against that cost.

3. **First bootstrap budget: <= 2s.** On the reference machine, for a worktree
   with no existing generation, the end-to-end time from invocation through
   creating a minimal bootstrap generation (conservative permissions, the OMCA
   MCP server, project-loadable Instructions, manifest) and handing off to the
   real host binary must not exceed 2 seconds.

4. Both numbers are measured and recorded as part of every generation's
   manifest/report, not as a hidden benchmark (`runtime.md` §5.3, roadmap M1
   deliverable "Record complete generation manifests... and the measured launch
   overhead and context cost"). `omca doctor` and the M1 exit gate consume the
   same measurement, so there is one source of truth for "did we meet the
   budget," not a separate perf-only harness.

5. The budget applies per host adapter (Codex and Claude Code each measured
   independently) because their isolation mechanisms differ (`runtime.md`
   §7.1-7.2); a slow mechanism on one host cannot be averaged away by a fast
   one on the other.

6. Meeting the budget is a precondition for a host adapter to be considered
   for the M1 exit gate ("shim plus bootstrap overhead is measured and stays
   inside the launch budget"). A mechanism that cannot meet it is not shipped
   as the default managed path; it is recorded as a capability gap.

## Alternatives Considered

- **No fixed numeric budget, qualitative only ("fast enough").** Rejected: the
  M1 exit gate and any CI performance test need a concrete assertion. A
  qualitative target cannot fail a build, so it would never actually gate
  anything, contradicting the M0 deliverable to record this ADR before code
  depends on it.
- **Container or VM-level isolation for every launch.** Rejected: this is an
  explicit non-goal (init.md, "Non-goals": no stronger container/VM boundary
  promised in v1) and would make the steady-state budget unreachable — the
  isolation mechanism itself must not need a heavier boundary than a process
  environment and an immutable generation directory.
- **Recompute (recompile) the generation on every launch instead of caching a
  current generation and only creating a new one on change.** Rejected: this
  conflates "first bootstrap" cost with "steady state" cost on every
  invocation and cannot meet a 100ms steady-state target once Profile/Knowledge
  resolution is nontrivial (later milestones). The immutable-generation model
  (`runtime.md` §5) already assumes selection, not recompilation, is the
  steady-state path.
- **One combined budget number instead of separate first-bootstrap and
  steady-state numbers.** Rejected: the two paths do fundamentally different
  work (creating a complete generation with a manifest vs. selecting an
  existing one) and conflating them would force either an unrealistically
  tight bootstrap budget or an unnecessarily loose steady-state one, defeating
  the "founding pain" goal of a fast, invisible steady-state shim.
- **Measure on CI runners as the reference machine.** Rejected: CI hardware
  varies by provider and is shared/contended, making it unsuitable as the
  strict evidence baseline. A named, controlled reference machine keeps the
  target numbers reproducible; CI gets its own (separately defined, looser)
  ceiling instead of inheriting the strict target directly.

## Consequences

- The M1 deliverable to "record complete generation manifests... and the
  measured launch overhead" (roadmap) has concrete thresholds to check against:
  100ms steady-state, 2s first bootstrap, both on the named reference machine.
- A later CI performance test can assert against these numbers directly for
  reference-machine runs, and against a separately documented looser ceiling
  everywhere else, without re-deriving what "the launch budget" means.
- Both first-party adapters must instrument and report their own overhead
  per-host; a regression in either blocks that host's M1 exit, not the whole
  milestone silently.
- If Codex or Claude Code's proven isolation mechanism cannot meet the budget,
  M1 records an explicit capability gap and follow-up gate rather than quietly
  loosening the number after the fact (roadmap milestone discipline: "a later
  milestone must not compensate for an unmet exit condition in an earlier
  one").
- Future ADRs or milestone documents that need to change these numbers must do
  so by superseding this ADR, not by silently redefining "the launch budget"
  in a milestone doc.
