# Product Requirements

Status: draft

## 1. Primary User

The primary user is a programmer who works under multiple simultaneous
identities:

```text
personal
+ company
+ one or more teams
+ project
+ optional temporary task
```

The user may use several coding-agent hosts across personal and company
repositories. Native installers have already placed Instructions, Skills, MCP
servers, Hooks, Plugins, credentials, and state in global and project locations.

The user does not want to understand each host's complete precedence model in
order to answer basic operational questions.

## 2. Primary Job to Be Done

> When I enter a directory or worktree, tell me what coding-agent harness will
> actually be used, why it will be used, what differs from my intended setup,
> and what will change after I approve a repair.

The report is trusted only when it is reproducible, evidence-backed, explicit
about unknown behavior, and traceable to native sources and runtime artifacts.

## 3. User Outcomes

The user must be able to answer:

1. Which identities and Profiles apply here?
2. Which Instructions, Skills, MCP servers, Hooks, Plugins, and permissions were discovered?
3. Which assets are active, available, excluded, denied, shadowed, or unknown?
4. Which source and policy produced each effective value?
5. How much model context or tool-schema cost does the active harness introduce?
6. Is the host version covered by qualified Knowledge?
7. Is this process using an OMCA runtime or an unmanaged native environment?
8. What is different between native, current, pending, and desired state?
9. Can the difference be repaired safely, and will a restart be required?
10. How can the previous runtime be restored?
11. Which hosts can run in parallel in this worktree, and how do their
    loadouts deliberately differ?

## 4. Desired-state Model

### 4.1 Profiles

Profiles are composable sets of intent. A Profile can contribute assets,
preferences, constraints, and vendor extensions without owning a physical host
path.

```yaml
apiVersion: omca.dev/v1alpha1
kind: Profile

metadata:
  id: company:example

spec:
  assets:
    skills:
      - id: code-review
        intent: AVAILABLE
      - id: deep-refactor
        intent: DEFAULT
        hosts: [claude-code]
    mcpServers:
      - id: internal-docs
        intent: AVAILABLE
      - id: codegraph
        intent: DEFAULT
        hosts: [codex]
    instructions:
      - id: engineering-baseline
        intent: DEFAULT
  policy:
    permissions:
      sandbox:
        intent: DEFAULT
        value: workspace-write
```

An asset entry may carry a `hosts` selector listing canonical host IDs. Without
a selector, the intent applies to every host launched in the matching context.
A host-scoped entry refines host-neutral entries for the listed hosts; it can
never weaken a `DENIED` intent from any scope. This is how one worktree gives
parallel hosts deliberately different loadouts.

### 4.2 Bindings

Bindings select Profiles for a context. They do not establish host precedence.

```yaml
apiVersion: omca.dev/v1alpha1
kind: Binding

metadata:
  id: binding:order-service

spec:
  match:
    repository: github.com/example/order-service
    paths: ["**"]
  profiles:
    - personal:alice
    - company:example
    - team:payments
    - project:order-service
```

### 4.3 Local activation

Local activation records worktree-specific choices without modifying a shared
Profile:

```yaml
apiVersion: omca.dev/v1alpha1
kind: Activation

metadata:
  worktree: worktree:sha256:...

spec:
  enable:
    skills: [code-review]
    mcpServers: [codegraph]
  disable:
    skills: [release-production]
  hosts:
    claude-code:
      enable:
        skills: [ui-review]
    codex:
      disable:
        mcpServers: [internal-docs]
```

Host-scoped Activation entries follow the same rules as host-scoped Profile
intent: they refine the host-neutral selection for one host and cannot
re-enable a `DENIED` asset.

The compiler evaluates `REQUIRED`, `DEFAULT`, `AVAILABLE`, and `DENIED` before
host-specific rendering. Ambiguous conflicts remain visible and block unsafe
generation.

## 5. Default Behavior

### 5.1 Context selection

The default context combines:

```text
personal default
+ repository-bound company Profiles
+ matching team Profiles
+ project Profile
+ local worktree activation
```

Explicit CLI context wins only for context selection. It does not override a
`DENIED` constraint or invent host precedence.

If multiple company or project identities remain plausible, OMCA asks the user
to select one and stores the choice locally. It does not commit personal
identity choices to the repository.

### 5.2 Repository sources

| Source | Default treatment |
|---|---|
| Repository Instructions | Loadable project input; active unless a supported project policy says otherwise. |
| Repository Skills | Discovered and `AVAILABLE` unless a Profile assigns another intent. |
| Repository MCP servers | Discovered and `AVAILABLE`; activation shows command, network, and secret references. |
| Repository Hooks | Observed; activation requires explicit confirmation. |
| User global native sources | Observed but not inherited by an isolated runtime. |
| System/admin sources | Observed and reported; isolation depends on the host and operating system. |

### 5.3 Direct host commands

Inside an approved direnv environment, direct commands such as `codex` resolve
to an OMCA shim and launch the current worktree generation. Outside an OMCA
environment, the native command may run, but OMCA classifies it as unmanaged.

The `omca` command opens the management TUI. `omca run <host>` remains an
explicit launch path for diagnosis and automation.

## 6. Functional Requirements

### FR-1: Broad observation

OMCA must discover known global, shared, repository, directory, local, session,
and system sources for the detected host version without executing discovered
Hooks, Plugins, Extensions, MCP servers, or Skills.

### FR-2: Lossless inventory

Every source must retain its physical location, scope, owner when available,
raw digest, parsed digest, trust requirement, host version, provenance, and
opaque vendor fields. Secret values must be redacted before persistence.

### FR-3: Versioned normalization

Only a qualified Knowledge Pack may normalize or resolve host behavior. Unknown
or stale behavior remains observable but cannot be silently promoted to managed.

### FR-4: Trusted report

The report must distinguish native observation, current runtime, pending
runtime, desired state, and host-reported effective state. Every conclusion
must include evidence and coverage.

### FR-5: Root-cause drift

Human output must group cross-product cells by root cause and remediation. It
must show representative samples while retaining a queryable full matrix.

### FR-6: Context-cost report

OMCA must report known or estimated cost for active Instructions, Skill
metadata/content, MCP tool schemas, duplicate tools, and always-on context.
Estimates must identify their method and confidence.

### FR-7: Bootstrap isolation

The first managed host launch must use a minimal runtime that does not inherit
user-global native configuration. It must contain only the safe baseline,
selected project-loadable inputs, and the OMCA MCP server.

### FR-8: Immutable generations

The active generation is immutable. Desired-state changes compile into a
pending generation and require a restart boundary before activation.

### FR-9: Transactional activation

Generation activation must be atomic, recorded in a Ledger, and reversible.
Concurrent source changes invalidate an existing Plan through digest checks.

### FR-10: MCP-first management

An MCP-capable LLM must be able to query status, report, drift, provenance, and
artifacts, and submit a schema-constrained repair proposal. Runtime-only changes
may be staged through MCP; privileged or shared-source changes require explicit
approval outside the model.

### FR-11: Native comparison

The user must be able to compare a managed generation with the native host
environment and run a clearly marked native diagnostic session.

### FR-12: Knowledge upgrades

Host documentation, schema, source, or executable changes create a Knowledge
Candidate. Only a maintainer-reviewed pull request can publish a new immutable
Knowledge Pack.

### FR-13: Host-scoped desired state

Profiles and Activation must support host selectors so that one worktree can
give different hosts deliberately different loadouts. The compiler must resolve
host-neutral and host-scoped intent deterministically and report the resulting
per-host outcome.

### FR-14: Plugin-packaged host support

Each host integration is a versioned adapter plugin behind one frozen adapter
contract, shipping with its own Knowledge Packs, fixtures, and qualification
state. The core must load first-party plugins and, once the out-of-process
transport is qualified, external plugins through the same contract.

## 7. Risk-based Confirmation

| Change | Default confirmation |
|---|---|
| Select an already reviewed Instruction | Stage pending generation automatically. |
| Select an already reviewed Skill | Stage pending generation automatically. |
| Change a model or display preference | Stage pending generation automatically. |
| Enable an MCP server | Confirm command, network destinations, and secret references. |
| Enable a Hook, Plugin, or Extension | Always confirm executable behavior and data flow. |
| Expand filesystem, network, approval, or sandbox access | Always confirm. |
| Modify a shared project/company Profile | Produce a reviewable repository diff. |
| Import a native credential file | Prohibited by default. |

## 8. LLM Boundary

LLM output never changes Drift classification, evidence level, capability
qualification, or guarantee level. An LLM may:

- summarize or explain a deterministic report;
- ask OMCA for narrower evidence;
- propose a canonical desired-state change;
- request compilation of a pending runtime; and
- suggest a semantic merge that a human reviews.

OMCA validates every proposal against schemas, capability gates, policy,
ownership, source digests, and risk confirmation before writing anything.

## 9. Non-functional Requirements

- **Local-first:** observation, normalization, reporting, and compilation work
  without a hosted service or remote LLM.
- **Deterministic:** the same inputs and immutable Knowledge digest produce the
  same normalized graph and generation digest.
- **Explainable:** every effective value is traceable to intent and physical
  sources.
- **Fail closed:** unknown host behavior cannot trigger destructive generation.
- **Recoverable:** every activation has a previous generation and Ledger entry.
- **Secret-safe:** reports, manifests, diffs, logs, and model context contain
  references or redacted values only.
- **Low-context:** the control MCP exposes a deliberately small tool surface and
  loads detailed artifacts only on demand.
- **Lean launch:** a managed launch includes only selected assets — zero
  unselected MCP servers or Skills enter the runtime — and the measured launch
  overhead and context cost appear in the report against a native baseline.
- **Cross-platform path:** macOS with zsh and direnv is the first qualified
  environment; abstractions must not preclude Linux, bash, or fish.
- **Fast common path:** entering an unchanged worktree and selecting its current
  generation should use content-addressed cached results.

## 10. Open Product Questions

These questions do not block documentation restructuring, but must close before
the corresponding milestone:

1. Whether OS keyring credential storage is mandatory for the first Codex adapter.
2. Which mutable Codex state, such as sessions and SQLite data, is shared across generations.
3. Whether repository Instructions are always active or can become `AVAILABLE` through an isolated overlay workspace.
4. The exact context-cost metric exposed to users when a host does not publish prompt assembly details.
5. Whether the TUI can restart a host process directly or only stage and instruct the user to restart.
6. Whether Claude Code's configuration-directory override yields complete
   user-global isolation, or a virtual process home is required as for Codex.
