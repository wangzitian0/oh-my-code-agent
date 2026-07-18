# Trusted Reporting and Debugging

Status: draft

## 1. Trust Definition

An OMCA report is trusted when it is:

- **broad:** known native, project, runtime, system, and invocation sources are inventoried;
- **lossless:** unknown fields and opaque assets are retained without execution;
- **versioned:** every host conclusion resolves to an immutable Knowledge Pack;
- **deterministic:** classification does not depend on an LLM;
- **evidence-backed:** every effective conclusion states how it was established;
- **honest:** unknown, stale, partial, and unsupported behavior remain visible;
- **reproducible:** input and output digests can reconstruct the result;
- **actionable:** the user can inspect impact, stage a repair, restart, verify, or roll back; and
- **debuggable:** every summary expands to the complete matrix and native artifact trace.

The report does not claim that a model will obey advisory Instructions or that
two hosts with similar configuration will produce identical behavior.

## 2. Reported State Planes

The report never collapses these planes:

| Plane | Meaning |
|---|---|
| `NATIVE` | Sources discovered in the unmodified host environment. |
| `OBSERVED` | Parsed and normalized facts, including opaque or unknown content. |
| `DESIRED` | Effective Profile, policy, exception, and Activation intent. |
| `CURRENT` | Artifacts used by the active runtime generation. |
| `PENDING` | Artifacts prepared for the next restart. |
| `HOST_EFFECTIVE` | Values reported or behaviorally inferred from the running host. |

A source can be discovered in `NATIVE`, excluded from `CURRENT`, selected in
`DESIRED`, and present in `PENDING` at the same time. That is not contradictory;
it is the expected restart workflow.

## 3. Source Disposition

```text
DISCOVERED   found in a known physical source
IMPORTED     explicitly brought into desired/runtime state
ACTIVE       present in the current or host-effective state
AVAILABLE    cataloged but not selected
EXCLUDED     intentionally absent from the runtime
DENIED       blocked by policy
SHADOWED     discovered but ineffective because another source wins
ORPHANED     source or installer ownership no longer resolves
OPAQUE       retained without semantic interpretation
UNKNOWN      identity, precedence, or behavior is not proven
```

Every exclusion includes a reason. Isolation must never make native content
disappear from the report.

## 4. Evidence Levels

| Level | Name | Meaning |
|---|---|---|
| `E0` | `DISCOVERED` | A physical source or host surface was found. |
| `E1` | `PARSED` | The source was parsed losslessly or retained safely as opaque. |
| `E2` | `RESOLVED` | A qualified resolver computed the effective result. |
| `E3` | `HOST_REPORTED` | A native status, environment, debug, or introspection interface confirmed it. |
| `E4` | `BEHAVIOR_PROBED` | An isolated session produced the expected canary behavior. |
| `E5` | `EXTERNALLY_PROVEN` | An OS, enterprise control, or independent audit system established it. |

Evidence is monotonic only when the higher level proves the same claim. An `E4`
behavior probe does not prove an `E3` prompt assembly trace, and neither turns
an advisory Instruction into hard enforcement.

`docs/architecture/evidence-ceiling.md` names, per host and concept, which of
E0-E3 this repository can actually reach today and why — internal/assurance's
`VerifyGraph` enforces that no reported conclusion exceeds it.

## 5. Guarantee Levels

| Guarantee | Meaning |
|---|---|
| `HARD` | An OS, enterprise, or host enforcement mechanism prevents violation. |
| `RECONCILED` | OMCA detects and restores drift, but temporary divergence is possible. |
| `ADVISORY` | Correctness depends on model or human compliance. |
| `OBSERVED` | OMCA can report but cannot control the outcome. |

Evidence answers “why do we believe this result?” Guarantee answers “what
prevents it from changing or being violated?” They are independent dimensions.

## 6. Drift Model

Each machine-level Drift assertion has the form:

```text
entity ID
+ field
+ expected value
+ observed/effective value
+ root cause
+ remediation
+ host/version/context cell
+ evidence
```

Canonical categories:

```text
CONFIG_DRIFT       managed artifact differs from desired state
EFFECTIVE_DRIFT    host-effective state differs from desired/current state
SOURCE_DRIFT       representations of one logical entity diverge
CAPABILITY_GAP     adapter cannot safely normalize, compile, or verify
KNOWLEDGE_DRIFT    host version or evidence exceeds qualified Knowledge
CONTEXT_DRIFT      invocation bypassed or selected a different context
EXCEPTION          authorized, documented, and unexpired difference
UNKNOWN            the system cannot safely classify the result
```

## 7. Root-cause Aggregation

The machine evaluates the full cross product:

```text
projects × hosts × versions × concepts × invocation contexts
```

Human output groups by:

```text
root cause + remediation + outcome class + adapter version
```

One company Profile issue affecting forty artifacts is one action card, not
forty top-level alerts.

```text
DR-017  Company baseline is not represented in pending runtimes       MEDIUM

Expected    workspace-write + on-request
Source      company:example/security-default
Impact      8 projects · 5 hosts · 40 artifacts
Evidence    38 × E3, 2 × E2
Guarantee   RECONCILED
Action      rebuild 38 artifacts; retain 2 explicit exceptions

Samples
  infra2 / Codex      danger-full-access -> workspace-write
  finance / Claude    approval bypass    -> on-request
```

Sample selection is deterministic and covers each distinct outcome, adapter
version, and exceptional bucket before adding redundant examples. The report
always exposes the complete matrix count and query.

## 8. Context-cost Reporting

OMCA reports the cost of active and candidate assets where evidence permits:

| Cost | Method |
|---|---|
| Instruction content | Token count using a declared tokenizer or byte estimate. |
| Skill discovery metadata | Name/description text included by the host. |
| Skill full content | Loaded content size when the host activates the Skill. |
| MCP tool schema | Serialized tool definitions visible to the model. |
| Duplicate capability | Logical tool fingerprint collision across transports. |
| Always-on Hook output | Measured or configured injected content. |

Every estimate includes `method`, `hostVersion`, and `confidence`. Unknown prompt
assembly is reported as unknown rather than converted into a false token count.

The default report highlights capabilities that are globally installed but not
needed by the current worktree, because reducing accidental context is a core
product outcome.

## 9. Human Information Architecture

```text
Workspace
├── Overview
│   ├── Context and identities
│   ├── Runtime status
│   ├── Coverage and Knowledge status
│   └── Context-cost summary
├── Drift
│   └── Root-cause action cards
├── Assets
│   ├── Active
│   ├── Available
│   ├── Excluded
│   └── Unknown
├── Generations
│   ├── Current
│   ├── Pending
│   └── History
├── Profiles and Activation
└── Debug
    ├── Native vs Current vs Pending
    ├── Effective State
    ├── Host Matrix
    ├── Precedence Trace
    ├── Evidence
    └── Native Artifacts
```

The default view uses logical IDs, intent, impact, reason, and action. Native
paths, merge operators, precedence programs, and raw fields appear only in
Explain or Debug.

## 10. CLI Queries

```bash
omca status
omca observe [--host codex] [--json]
omca report [--worktree <id>]
omca drift
omca drift show <drift-id>
omca explain <concept> <logical-id> [--trace]
omca matrix <drift-id>
omca compare --native --current
omca diff current pending
omca generation show <generation-id>
omca doctor <host>
```

All read commands support stable JSON output. Human output is a projection of
the same immutable report artifact.

## 11. MCP Contract

The MCP surface remains small to avoid creating its own context problem:

```text
omca_status
omca_query
omca_propose
omca_stage
```

### 11.1 `omca_status`

Returns context, identities, current/pending generation, high-level Drift,
coverage, Knowledge state, and restart requirement.

### 11.2 `omca_query`

Queries a logical entity, Drift ID, evidence record, generation, or artifact.
Large content is returned through paged results or artifact URIs rather than one
unbounded tool response.

### 11.3 `omca_propose`

Accepts a schema-constrained desired-state proposal tied to a report
fingerprint. It does not accept raw native file edits.

### 11.4 `omca_stage`

Compiles an approved low-risk proposal into a pending generation. It returns
the diff, required confirmations, and `restart_required`. It never mutates
`current`.

Shared-source changes, executable activation, permission expansion, and final
generation activation remain human-controlled.

## 12. LLM Annotations

LLM explanations are stored separately from deterministic report fields:

```yaml
annotation:
  author:
    kind: llm
    model: <runtime model ID>
  basedOnReport: sha256:...
  text: <explanation>
  confidence: user-facing only
```

An annotation cannot change a Drift category, evidence level, capability,
guarantee, or source disposition.

## 13. Canary Probes

Canary probes are a last-resort verification method for host behavior that
cannot be introspected directly. They run only in disposable homes, workspaces,
and fresh sessions with randomized non-secret nonces.

Use native debug/status output first. A canary may indicate that an Instruction
was behaviorally visible, but it cannot prove the complete prompt or a hard
policy boundary.

## 14. Debug Invariants

```text
every action card expands to all affected cells
every cell expands to desired and observed values
every effective value expands to its resolver trace
every resolver trace expands to physical sources and Knowledge evidence
every generation expands to a complete manifest and parent
every exclusion records an explicit reason
every LLM explanation is separable from deterministic facts
```
