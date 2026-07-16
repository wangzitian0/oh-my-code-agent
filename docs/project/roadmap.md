# Implementation Roadmap

Status: draft

Milestones are capability gates, not calendar commitments. A later milestone
must not compensate for an unmet exit condition in an earlier one.

Two hosts are first-party throughout this roadmap: OpenAI Codex and Claude
Code. Codex leads inside each milestone because `CODEX_HOME` is the cleanest
documented isolation boundary; a milestone exits only when both first-party
hosts pass its gate, or when a remaining Claude Code gap is recorded as an
explicit capability gap with its own follow-up gate. Every other host stays at
the knowledge/observation tier until M6.

## M0: Contracts, Plugin Protocol, and Qualification Lab

M0 is time-boxed. Semantics that cannot be settled inside the box enter the
schemas as explicit known unknowns instead of extending the freeze.

### Deliverables

- Accept the v1alpha1 schemas for Profile, Binding, Activation, Observation,
  HostKnowledge, Report, RepairProposal, Generation, and Evidence, including
  host-scoped intent (`hosts` selectors).
- Freeze adapter plugin contract v1: the `HostAdapter` operations, the plugin
  manifest, and the qualification requirements every adapter must satisfy.
- Define stable logical IDs, digests, Artifact URIs, and redaction rules.
- Build temporary HOME/worktree fixtures for Codex and Claude Code.
- Capture Instructions, Skills, MCP, Hook, permission, trust, and config
  precedence cases for both first-party hosts.
- Record Runtime Isolation, Ownership, Credential, Knowledge Update, and
  Plugin Distribution ADRs.
- Establish Markdown, schema, fixture, and secret-leak CI checks.

### Exit Gate

```text
unknown precedence returns UNKNOWN
observation fixtures prove zero writes and zero execution
all public schemas reject unknown major versions
all fixture outputs are reproducible from committed inputs
both first-party adapters compile against the frozen plugin contract
host-scoped intent resolves deterministically in golden scenarios
```

## M1: Isolated Launch

This milestone removes the founding pain: a session that loads dozens of
unselected user-global Skills and MCP servers. It ships before deep reporting.

### Deliverables

- Detect host binary, exact version, surface, native homes, worktree, cwd,
  profile, trust, and invocation flags for Codex and Claude Code.
- Implement `omca env`, direnv integration, and non-recursive PATH shims for
  `codex` and `claude`.
- Generate a minimal bootstrap home and virtual user home per host.
- Exclude native user-global Instructions, Skills, MCP, Hooks, and Plugins from
  the bootstrap path.
- Load supported repository inputs and conservative permission defaults.
- Observe enough native and repository sources to explain every exclusion.
- Record complete generation manifests, native exclusions, and the measured
  launch overhead and context cost.
- Implement `omca run --mode isolated`, `--mode native`, and `omca doctor`.

### Exit Gate

```text
a managed launch loads zero unselected MCP servers and Skills
shim plus bootstrap overhead is measured and stays inside the launch budget
managed context cost for the worktree is reported against the native baseline
first managed launch does not read native user-global config as a parent
native global sources remain visible in the report
direct codex or claude inside direnv uses the OMCA generation
direct native launch is detectable as unmanaged
```

The launch budget is fixed in M0. The target is that steady-state entry into an
unchanged worktree adds shim and generation-selection overhead measured in tens
of milliseconds, not seconds, on the reference machine. The measured numbers are
part of the report, not a hidden benchmark.

## M2: Profiles, Activation, and Immutable Generations

### Deliverables

- Resolve personal, company, multiple team, project, and local worktree identity.
- Implement `REQUIRED`, `DEFAULT`, `AVAILABLE`, and `DENIED`.
- Resolve host-neutral and host-scoped intent into per-host desired state.
- Persist identity selection and Activation under worktree state.
- Compile complete content-addressed generations with per-host artifact trees.
- Implement current, pending, parent, Ledger, restart activation, and rollback.
- Add source-digest compare-and-swap and concurrent-change invalidation.
- Show native/current/pending/desired comparison.

### Exit Gate

```text
current never changes during a session
changes compile to pending
activation is atomic and restart-bound
rollback restores the parent generation
generated artifacts never become desired-state sources
two hosts in one worktree run deliberately different loadouts from one desired state
```

## M3: Trusted Report, Drift, and Explain

### Deliverables

- Complete native observation for both first-party hosts: user, shared user,
  repository, directory, system, and session sources for the initial concepts.
- Preserve opaque content and redact secrets.
- Build Observed and expected native Effective Graphs.
- Produce coverage, evidence, Knowledge status, duplicate capability, Hook data
  flow, permission, and context-cost reports.
- Implement root-cause Drift, representative samples, matrix, and Explain.
- Provide human and stable JSON output.

### Exit Gate

```text
the report answers what, why, scope, impact, and evidence
every summary expands to source artifacts
one root cause across N projects remains one action card
LLM availability does not change classification
```

## M4: MCP-first Management

### Deliverables

- Ship the minimal `omca_status`, `omca_query`, `omca_propose`, and `omca_stage` tools.
- Bind MCP calls to an immutable Run or Generation ID.
- Support paged results and Artifact URIs for large evidence.
- Validate RepairProposal against report fingerprint, schemas, capability,
  ownership, policy, and risk.
- Allow low-risk runtime-only Activation to stage pending generations.
- Route executable activation, permission expansion, and shared-source changes
  to explicit human confirmation.
- Keep deterministic and LLM-authored fields structurally separate.

### Exit Gate

```text
an arbitrary MCP-capable LLM can inspect and propose without native file access
MCP cannot mutate current
MCP cannot bypass confirmation classes
tool schemas and default responses remain deliberately small
```

## M5: Verification and Runtime State

### Deliverables

- Implement E0 through available E3 evidence for both first-party hosts.
- Add isolated canary fixtures only where native introspection is insufficient.
- Qualify OS-keyring or identity-specific authentication for isolated homes.
- Classify mutable sessions, SQLite data, logs, caches, trust, and memory.
- Verify restart activation against host-effective state.
- Implement generation bisect and automated rollback on failed verification.

### Exit Gate

```text
verification never overstates advisory behavior as enforcement
credentials do not enter generated config or reports
shared mutable state uses an explicit allowlist
failed verification leaves a recoverable previous generation
```

## M6: Out-of-process Plugins and Community Hosts

### Deliverables

- Qualify the out-of-process plugin transport: an adapter as a separate
  executable speaking the versioned contract over stdio.
- Publish plugin authoring documentation and the qualification checklist.
- Port one host (OpenCode is the first candidate) through the external plugin
  path, observation tier first.
- Add isolated runtime mechanisms only when the host exposes a reversible
  config/home boundary or a qualified overlay strategy.
- Mark every operation EXACT, COMPATIBLE, PARTIAL, OPAQUE, UNKNOWN, or UNSUPPORTED.
- Preserve vendor extensions without forcing a lowest-common-denominator schema.

### Exit Gate

```text
an external plugin qualifies observation without forking the core
host support is declared per concept and operation
an unsupported host operation degrades to observed or blocked
one Desired Graph can compile multiple proven host artifacts
no adapter claims behavioral equivalence between models
```

## M7: Knowledge Automation and Product TUI

### Deliverables

- Poll allowlisted official documentation, schemas, releases, and source revisions.
- Generate Knowledge Candidate reports and maintainer-reviewed pull requests.
- Run affected qualification fixtures automatically.
- Mark installed unqualified versions as Knowledge Drift.
- Build the root-cause-first TUI for reports, assets, Profiles, generations, and Debug.
- Support one human approval for a complete reviewed Change Set.

### Exit Gate

```text
vendor upgrades cannot silently change compilation behavior
historical generations retain their Knowledge digest
ordinary repair does not require understanding native paths or precedence
advanced users can trace every action card to evidence and artifacts
```

## First Implementation Slice

The first executable vertical slice is deliberately narrow:

```text
Codex + Claude Code on macOS
+ zsh and direnv
+ one worktree
+ personal + company + project Profiles
+ user-global and repository Instructions, Skills, MCP inventory
+ minimal isolated bootstrap for both hosts
+ measured launch overhead and context cost
+ activate one reviewed Skill for one host only
+ compile pending
+ restart and verify
+ rollback
```

This slice is complete only when the report, the measured launch numbers, and
rollback are credible. Adding a third host before that point increases surface
area without proving the product.

## Interim Relief

The founding trigger is measurable today: dozens of user-global Skills and MCP
servers enter every session. Until M1 ships, the supported stopgaps are host
native and manual: register MCP servers per project instead of per user, use
host flags that restrict MCP loading to an explicit configuration, and prune
user-global skill directories. M1 automates exactly these mechanisms; nothing
in this roadmap depends on tolerating the pain until then.

## Delivery Risks

| Risk | Required response |
|---|---|
| Host has no clean config/home isolation | Observation-only or qualified overlay; do not fake isolation. |
| Host reads assets outside its configured home | Virtual process home, explicit disable list, or capability gap. |
| Claude Code config-directory isolation proves incomplete | Virtual process home plus explicit disable flags; report residual load as a capability gap. |
| Authentication is coupled to the isolated home | OS keyring or identity-specific login; never copy native tokens silently. |
| Repository assets cannot be filtered | Report residual load or use a future overlay workspace. |
| Native introspection is missing | Static E2 evidence, isolated probes, and an honest verification ceiling. |
| Vendor upgrade changes precedence | Mark Knowledge stale and block affected compilation. |
| MCP becomes a context burden | Keep four coarse tools and use Artifact URIs for detail. |
| Two first-party hosts double milestone cost | Codex leads inside each milestone; shared fixtures and the plugin contract keep the second host incremental; an explicit capability gap beats silent delay. |
| The plugin contract churns after external adapters adopt it | Version the contract; additive-only change within v1; a breaking change requires a major contract version and migration notes. |
| Root/admin policy bypasses runtime isolation | Report boundary and require container/VM for stronger guarantees. |

## Definition of MVP

MVP ends when M0 through M5 pass for Codex and Claude Code on the first macOS
environment. M6 and M7 are post-MVP expansion. Documentation or mock TUI output
without executable qualification, immutable generations, restart verification,
and rollback does not satisfy MVP.
