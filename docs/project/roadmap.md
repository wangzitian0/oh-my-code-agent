# Implementation Roadmap

Status: draft

Milestones are capability gates, not calendar commitments. A later milestone
must not compensate for an unmet exit condition in an earlier one.

## M0: Contracts and Qualification Lab

### Deliverables

- Accept the v1alpha1 schemas for Profile, Binding, Activation, Observation,
  HostKnowledge, Report, RepairProposal, Generation, and Evidence.
- Define stable logical IDs, digests, Artifact URIs, and redaction rules.
- Build temporary HOME/worktree fixtures for Codex.
- Capture Codex Instructions, Skills, MCP, Hook, permission, trust, and config
  precedence cases.
- Record Runtime Isolation, Ownership, Credential, and Knowledge Update ADRs.
- Establish Markdown, schema, fixture, and secret-leak CI checks.

### Exit Gate

```text
unknown precedence returns UNKNOWN
observation fixtures prove zero writes and zero execution
all public schemas reject unknown major versions
all fixture outputs are reproducible from committed inputs
```

## M1: Native Observation and Trusted Report

### Deliverables

- Detect Codex binary, exact version, surface, native homes, worktree, cwd,
  profile, trust, and invocation flags.
- Inventory native user, shared user, repository, directory, system, and session
  sources for the initial concepts.
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

## M2: Worktree Bootstrap Runtime

### Deliverables

- Implement `omca env` and direnv integration.
- Implement a non-recursive Codex PATH shim.
- Generate a minimal Codex bootstrap home and virtual user home.
- Exclude native user-global Instructions, Skills, MCP, Hooks, and Plugins from
  the bootstrap path.
- Load supported repository inputs and conservative permission defaults.
- Record complete generation manifests and native exclusions.
- Implement `omca run --mode isolated`, `--mode native`, and `omca doctor`.

### Exit Gate

```text
first managed launch does not read native user-global config as a parent
native global sources remain visible in the report
direct codex inside direnv uses the OMCA generation
direct native launch is detectable as unmanaged
```

## M3: Profiles, Activation, and Immutable Generations

### Deliverables

- Resolve personal, company, multiple team, project, and local worktree identity.
- Implement `REQUIRED`, `DEFAULT`, `AVAILABLE`, and `DENIED`.
- Persist identity selection and Activation under worktree state.
- Compile complete content-addressed generations.
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

- Implement E0 through available E3 evidence for Codex.
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

## M6: Additional Hosts

### Deliverables

- Build Claude Code and OpenCode Qualification Packs and fixtures.
- Port trusted observation before adding compilation.
- Add isolated runtime mechanisms only when the host exposes a reversible
  config/home boundary or a qualified overlay strategy.
- Mark every operation EXACT, COMPATIBLE, PARTIAL, OPAQUE, UNKNOWN, or UNSUPPORTED.
- Preserve vendor extensions without forcing a lowest-common-denominator schema.

### Exit Gate

```text
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
Codex on macOS
+ zsh and direnv
+ one worktree
+ personal + company + project Profiles
+ Instructions, Skills, MCP, Hooks inventory
+ trusted native report
+ minimal isolated bootstrap
+ activate one reviewed Skill
+ compile pending
+ restart and verify
+ rollback
```

This slice is complete only when the report and rollback are credible. Adding a
second host before that point increases surface area without proving the product.

## Delivery Risks

| Risk | Required response |
|---|---|
| Host has no clean config/home isolation | Observation-only or qualified overlay; do not fake isolation. |
| Host reads assets outside its configured home | Virtual process home, explicit disable list, or capability gap. |
| Authentication is coupled to the isolated home | OS keyring or identity-specific login; never copy native tokens silently. |
| Repository assets cannot be filtered | Report residual load or use a future overlay workspace. |
| Native introspection is missing | Static E2 evidence, isolated probes, and an honest verification ceiling. |
| Vendor upgrade changes precedence | Mark Knowledge stale and block affected compilation. |
| MCP becomes a context burden | Keep four coarse tools and use Artifact URIs for detail. |
| Root/admin policy bypasses runtime isolation | Report boundary and require container/VM for stronger guarantees. |

## Definition of MVP

MVP ends when M0 through M5 pass for the first Codex environment. M6 and M7 are
post-MVP expansion. Documentation or mock TUI output without executable
qualification, immutable generations, restart verification, and rollback does
not satisfy MVP.
