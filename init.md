# oh-my-code-agent Project Charter

Status: draft

## Product Statement

Build a local-first, ontology-first control plane that gives programmers with
multiple identities a trusted report of their effective coding-agent harness
and an isolated, directory- or worktree-scoped runtime containing only the
capabilities selected for that context.

```text
Observe -> Model -> Reconcile
```

- **Observe** native files, host state, versions, scopes, and unknown content
  broadly without executing discovered code.
- **Model** only the subset that can be mapped to a versioned ontology with
  explicit confidence and provenance.
- **Reconcile** an approved desired state into a new immutable runtime
  generation, then activate it on restart.

The primary user-facing outcome is a trusted report. Runtime management exists
to make the report actionable, not to hide uncertainty or claim that different
hosts behave identically.

## Approved Product Decisions

1. Repository documentation, code, schemas, configuration keys, and CLI output
   are English unless a localized user asset explicitly requires another
   language.
2. The primary user is a programmer who combines personal, company, team,
   project, and temporary task identities.
3. Profiles compose; they are not a fixed inheritance ladder.
4. Top-down assets use explicit activation intent instead of global
   installation: `REQUIRED`, `DEFAULT`, `AVAILABLE`, or `DENIED`.
5. Company Skills and MCP servers normally enter a catalog as `AVAILABLE`; they
   do not enter every model context by default.
6. Repository Instructions are loadable project inputs. Repository Skills and
   MCP servers are discovered but activated according to policy and context.
7. The primary runtime scope is a directory or Git worktree.
8. `direnv` establishes the context and a PATH shim makes direct host commands
   use the current OMCA runtime by default.
9. Native global configuration is observed but is not inherited by default.
10. The runtime uses immutable generations. Changes are written to desired
    state, compiled to `pending`, and activated after restart.
11. A minimal bootstrap runtime starts before the target host. Most daily
    inspection, activation, and repair interactions happen through the OMCA MCP
    server after the host starts.
12. The deterministic core owns discovery, normalization, drift classification,
    compilation, verification, and rollback. Any LLM may explain results or
    submit a schema-constrained repair proposal.
13. Company and team policy is primarily advisory in v1. OMCA never represents
    model instructions as hard security enforcement.
14. Secrets and credentials are supplied through `direnv`, an OS credential
    store, or references. OMCA does not copy native credential files by default.
15. Third-party host knowledge changes only through repository pull requests
    approved by maintainers.

## Activation Intent

| Intent | Meaning | Lower-scope behavior |
|---|---|---|
| `REQUIRED` | The asset must be active in matching contexts. | Cannot be disabled without an explicit exception allowed by the defining policy. |
| `DEFAULT` | The asset is active unless a lower scope opts out. | May be disabled. |
| `AVAILABLE` | The asset is discoverable and selectable. | Inactive until selected. |
| `DENIED` | The asset must not be active. | Cannot be re-enabled by lower scopes. |

Activation intent is not host precedence. It is desired-state policy that the
host adapter compiles into proven native behavior.

## Operating Model

```text
enter worktree
  -> direnv evaluates `omca env`
  -> the host shim selects or creates a bootstrap generation
  -> the host starts with an isolated home and the OMCA MCP server
  -> OMCA observes native and runtime sources
  -> the user or an LLM reviews the trusted report
  -> an approved change updates local desired state
  -> OMCA compiles a pending immutable generation
  -> the host restarts and pending becomes current
  -> OMCA verifies and records the new effective state
```

The first host session must not require the real native global home. Otherwise
global Hooks, MCP servers, Plugins, or Instructions could execute before OMCA
can report them.

## Sources of Truth

The project separates stable meaning, changing vendor facts, user intent, and
generated artifacts:

| Layer | Source of truth | Mutability |
|---|---|---|
| Product behavior | This charter and Product Requirements | Maintainer-reviewed PR |
| Canonical meaning | Ontology | Versioned schema change |
| Host behavior | Knowledge Pack plus fixtures | Immutable pack, superseded by PR |
| Desired state | Profiles, bindings, policies, exceptions, local activation | User or reviewed repository change |
| Effective state | Runtime manifest and host verification evidence | Immutable per generation |
| Native configuration | Observation source only | Owned by its external installer or user |

Generated host files are build artifacts. They never become canonical desired
state, even when they persist across restarts.

## Product Goals

- Produce a complete, reproducible inventory of relevant coding-agent sources.
- Explain what is native, current, pending, desired, excluded, shadowed, or
  unknown for a worktree.
- Estimate context and tool-schema cost before activating Skills or MCP servers.
- Detect duplicate tools, stale configuration, unexpected Hooks, broad
  permissions, and unqualified host upgrades.
- Preserve unknown vendor fields and native assets without executing them.
- Let a user activate only the assets needed for the current identity and task.
- Make every generated value traceable to intent, source, adapter, Knowledge
  Pack, and evidence.
- Allow any MCP-capable LLM to query the report and propose a repair.
- Make restart, rollback, and native-versus-runtime comparison routine.

## Non-goals

- Prevent a root administrator, MDM, replaced host binary, or compromised shell
  from controlling the machine.
- Guarantee identical model behavior across hosts.
- Translate executable Plugins, Hooks, or Extensions automatically.
- Treat prompt compliance as a security boundary.
- Import every native global asset into an OMCA runtime.
- Modify shared project or organization policy without a reviewable change.
- Build a hosted SaaS, secret manager, marketplace, or background fleet manager
  in v1.
- Support every host concept for write operations.

## Trust Boundary

V1 protects a worktree runtime from user-scoped global installers and historical
configuration, including writes to locations such as `~/.codex` and
`~/.agents/skills`. It observes system and repository sources but cannot promise
to isolate system-admin or repository-owner policy without a stronger container
or virtual-machine boundary.

Unknown precedence, an unqualified host version, a lossy round trip, or missing
verification evidence automatically downgrades a capability to observation or
blocked planning.

## MVP Scope

### Hosts

- OpenAI Codex as the first end-to-end adapter.
- Claude Code and OpenCode after the Codex runtime and report pass their exit gates.
- Other hosts remain inventory and Knowledge targets until qualified per capability.

### Concepts

- Instructions
- Skills
- MCP servers
- Permissions needed to describe runtime safety
- Hooks as inventory and risk-reporting entities

### User Surfaces

- `omca` TUI for report, activation, restart, rollback, and Debug views.
- `omca env` for direnv integration.
- Host shims such as `codex` inside an OMCA environment.
- `omca mcp serve` for model-facing queries and repair proposals.
- Stable JSON output for automation and test fixtures.

### MVP Acceptance Scenario

In a repository with shared project guidance and native user configuration:

```bash
cd <worktree>
direnv allow
codex
```

The system must:

1. create or select a clean bootstrap generation without inheriting native
   global configuration;
2. start Codex with the OMCA MCP server and project-loadable inputs;
3. report native, current, desired, and excluded Instructions, Skills, MCP
   servers, Hooks, and permissions;
4. show provenance, coverage, host version, evidence level, and context-cost
   estimate;
5. let the user activate an `AVAILABLE` Skill through the MCP or TUI;
6. compile a pending generation without mutating the active one;
7. activate it after restart and verify the new effective state;
8. roll back to the previous generation;
9. compare the isolated runtime with the native host configuration; and
10. leave the real global configuration unchanged.

## Invariants

```text
observe never writes or executes discovered assets
native global configuration is never an implicit runtime parent
same inputs plus the same Knowledge digest produce the same generation digest
current generations are immutable
pending is activated only at a restart boundary
every effective value has provenance and evidence
unknown behavior cannot be promoted to managed by an LLM
denied intent cannot be weakened by a lower scope
generated artifacts are not desired-state sources
secrets do not enter reports, plans, manifests, or model context
```

## Documentation

The authoritative documentation map and precedence rules are in
[docs/README.md](docs/README.md). The implementation sequence and exit gates are
in [docs/project/roadmap.md](docs/project/roadmap.md).
