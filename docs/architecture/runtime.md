# Runtime Architecture

Status: draft

## 1. Purpose

The runtime prevents user-global native configuration from becoming an
unreviewed parent of every coding-agent session. It does not hide native state:

> Observe native configuration, but do not inherit it implicitly.

The runtime is scoped to a directory or Git worktree and combines multiple
identities, project requirements, local activation, qualified host behavior,
and explicit policy.

## 2. Threat Model

V1 protects against accidental or aggressive user-scope installers that write
to locations such as:

```text
~/.codex
~/.claude
~/.config/opencode
~/.agents/skills
```

Examples include globally installed Skills, MCP servers, Hooks, Plugins, broad
permissions, stale Instructions, and orphaned state.

V1 does not claim to resist:

- root or MDM policy;
- `/etc` configuration and admin Skills;
- a replaced host binary;
- a compromised shell, direnv, or OMCA binary;
- repository-owner changes committed to the opened worktree; or
- operating-system credential-store compromise.

System and repository sources remain observable and risk-classified. Stronger
isolation requires a container, VM, or operating-system security boundary.

## 3. Bootstrap Before the Host

An MCP server starts after the host has already begun resolving configuration.
Therefore MCP can manage the next generation, but it cannot establish the first
isolation boundary.

The pre-host bootstrap is intentionally small:

```text
resolve worktree ID
  -> resolve host binary and version
  -> select current generation
  -> create a minimal bootstrap generation if none exists
  -> set isolated environment variables
  -> execute the real host binary
```

The bootstrap generation contains:

- conservative permission defaults;
- the OMCA MCP server;
- project-loadable Instructions supported by the adapter;
- no implicit user-global Skill, MCP, Hook, Plugin, or Instruction; and
- a manifest explaining every included and excluded source.

All broad observation, profile selection, activation, and repair can happen
after launch through the OMCA MCP or TUI.

## 4. direnv and Host Shims

Recommended `.envrc` integration:

```bash
eval "$(omca env --shell bash)"
```

`omca env` exports context and prepends an OMCA shim directory to `PATH`:

```text
OMCA_CONTEXT_ID
OMCA_WORKTREE_ID
OMCA_REAL_HOME
OMCA_STATE_DIR
OMCA_SHIM_DIR
PATH=$OMCA_SHIM_DIR:$PATH
```

The shim locates the real host binary without recursively invoking itself,
selects the current generation, injects host-specific environment, and uses
`exec` so signal and exit behavior remain native.

Inside the direnv environment:

```bash
codex       # managed current generation
claude      # managed current generation
opencode    # unmanaged until an adapter plugin qualifies it
omca        # management TUI
```

Outside the environment, a direct native host command remains possible but is
classified as unmanaged. `omca doctor` reports PATH bypass, missing direnv
approval, stale generation, and a host binary that moved after qualification.

## 5. Immutable Generations

```text
worktree state
├── desired/
├── generations/
│   ├── gen-000001/
│   └── gen-000002/
├── current -> generations/gen-000001
├── pending -> generations/gen-000002
└── ledger/
```

### 5.1 Desired state

`desired/` is a persistent source containing identity selection, Activation,
local overrides, and references to shared Profiles. MCP and TUI changes target
this layer.

### 5.2 Current generation

`current` is immutable for the lifetime of a host session. A host may watch
files dynamically, so immutability is enforced at the OMCA artifact boundary
rather than relying on host reload semantics.

Compiled configuration and asset trees are read-only. Host-written sessions,
logs, databases, caches, and trust state live in separately classified mutable
paths; they cannot silently turn a generation artifact into a state directory.

### 5.3 Pending generation

Changes compile to a complete `pending` generation. The compiler never edits
`current` in place. A pending manifest contains:

```text
generation ID
parent generation ID
worktree and invocation context
selected Profiles and Activation
Ontology version
Knowledge Pack IDs and digests
source and desired-state digests
host artifacts and ownership
native/current/pending diff
risk confirmations
expected evidence and guarantee
```

### 5.4 Activation

Activation occurs at a restart boundary:

```text
validate pending
  -> ensure source digests still match
  -> close or detach the current host session
  -> atomically switch current
  -> launch the host
  -> verify
  -> append Ledger entry
```

If verification fails, OMCA can restore the parent generation and relaunch.

### 5.5 Parallel hosts in one worktree

A generation contains one artifact tree per host and surface
(`hosts/<host>/<surface>/`). Hosts launch independently from the same
generation and may run in parallel in one worktree, each with its own
host-scoped loadout.

Activation advances the worktree's `current` pointer, but a running session
keeps the generation it was launched with: generation directories are immutable
and retained while any session references them. After activation, `omca status`
and `omca doctor` report sessions still running on a superseded generation and
which hosts require a restart. `restart_required` is therefore per host, not
per worktree.

## 6. MCP-first Reconciliation

The OMCA MCP server is present in every managed runtime unless an explicit
diagnostic mode disables it. It exposes a small control surface:

```text
omca_status
omca_query
omca_propose
omca_stage
```

The MCP server receives `OMCA_RUN_ID` or an equivalent immutable context token.
It does not infer another worktree from the model prompt.

Typical flow:

```text
user asks the model to enable a Skill
  -> model queries current report
  -> model calls omca_propose
  -> deterministic core validates Activation and capability
  -> low-risk change updates local desired state
  -> compiler creates pending generation
  -> tool result returns restart_required=true
```

MCP may stage runtime-only changes. Activating executable MCP servers, Hooks,
Plugins, Extensions, expanded permissions, or shared-source modifications still
requires explicit human confirmation.

## 7. First-party Host Isolation Strategies

Claude Code and Codex are the first-party adapter plugins. Codex leads
qualification because it demonstrates both the value and the difficulty of
runtime isolation with the cleanest documented boundary; Claude Code follows
inside the same milestones through its own mechanisms.

### 7.1 Codex

Codex uses `CODEX_HOME` for config and state, while user Skills can also be
discovered from `$HOME/.agents/skills`. The first adapter therefore needs an
isolated Codex home and a virtual process home:

```text
OMCA_REAL_HOME=/Users/alice
HOME=<generation>/virtual-home
CODEX_HOME=<worktree-state>/state/hosts/codex/cli/codex-home
OMCA_RUN_ID=<generation-id>
```

`CODEX_HOME` is deliberately NOT `<generation>/hosts/codex/cli/codex-home`:
that directory is the generation's own compiled config, made read-only on
disk (§5.2, §12's "current artifacts do not change during a session"), while
Codex uses this same variable to store its own mutable runtime state (session
history, local SQLite databases, `auth.json`) — pointing it directly at a
read-only directory fails the moment Codex tries to write. Each launch
instead points `CODEX_HOME` at a writable directory scoped to the worktree
(§9's "worktree-shared" state class), pre-synced with the current
generation's compiled `config.toml` but otherwise left untouched, so Codex's
own state survives both relaunches and generation recompiles within the same
worktree.

The generated Codex configuration can restore the real `HOME` for subprocesses
through Codex shell-environment policy so Git, SSH, package managers, and build
tools do not accidentally use the virtual discovery home. This behavior must be
proven by a versioned fixture before the adapter becomes managed.

The adapter must inventory at least:

```text
real ~/.codex
real ~/.agents/skills
repository AGENTS.md chain
repository .codex/config.toml chain
repository .agents/skills chain
/etc/codex sources
CLI and profile invocation
```

Project config and Instructions follow Codex trust behavior. System sources may
remain effective even when the user home is isolated and must be reported
separately.

#### 7.1.1 asdf-shimmed host installations

Setting `HOME` to the generation's virtual-home directory is load-bearing (it
is the only thing that actually stops a real host binary from resolving its
own native, unmanaged `$HOME/.agents/skills` — see the isolated-mode
end-to-end regression test guarding this,
`TestRunIsolated_EndToEnd_VirtualizesHome`), but it is not free: a host binary
installed and managed through [asdf](https://asdf-vm.com) resolves via PATH to
a shim script (`~/.asdf/shims/<name>`) whose own dispatch (`exec asdf exec
"<name>" "$@"` in every asdf version this project has observed) needs a real,
resolvable `HOME` to find asdf's own `~/.tool-versions`-derived state. Under
isolated mode's virtualized `HOME`, that dispatch fails outright — exit 126,
no output — strictly inside the asdf shim script itself, strictly after this
project's own `syscall.Exec` has already handed control to it (issue #69).

Rather than either giving up HOME virtualization for asdf-managed installs (a
real regression, per the paragraph above) or leaving the bare exit 126
unexplained, `omca run --mode isolated` and the PATH shim (`internal/shim`)
both detect an asdf shim by location (`internal/shim.IsASDFShim`: the
resolved binary's grandparent directory is named `.asdf`, its parent
`shims`) and resolve straight past it to the concrete, per-version real
binary asdf's own `asdf reshim` step already recorded
(`internal/shim.ResolveASDFShimTarget`): every shim asdf generates begins
with one `# asdf-plugin: <plugin> <version>` comment line per plugin version
that can provide the shimmed command, and the exec target is
`<asdf-data-dir>/installs/<plugin>/<version>/bin/<name>`. This needs no real
asdf invocation and no HOME lookup of its own, so it works correctly under
the fully virtualized HOME the rest of isolated mode requires.

When that comment names exactly one plugin version — the common case, and the
one issue #69's own reproduction hit (a single `nodejs 20.19.0` line for an
npm-global-installed `codex`) — the resolution is unambiguous. Two or more
lines mean two or more installed versions can provide the command, and only
asdf's own `.tool-versions` precedence (undocumented as a stable API, and a
genuine source of fragility across asdf versions) can disambiguate which one
is "current"; this project deliberately does not replicate that algorithm.
In that case, and in any other case the resolved target does not exist or is
not executable, `omca run` fails with a clear, actionable error naming the
asdf shim and pointing at a workaround (install the host outside asdf, or use
`--mode native`) instead of a bare, unexplained exit 126.

### 7.2 Claude Code

Claude Code separates the concerns differently: user-global assets (settings,
Skills, agents, memory, MCP registrations) live under a configuration
directory, while account and OAuth state, project trust decisions, and parts of
the MCP registry share one mutable user state file. The candidate isolation
mechanisms are:

```text
a relocated configuration directory per generation
session flags that restrict which settings and MCP configs load
a strict MCP mode that ignores non-specified registrations
a virtual process home, as for Codex, if the above are incomplete
```

Which combination yields complete user-global exclusion is an open
qualification question (see the product requirements); the adapter must prove
its mechanism with versioned fixtures before Claude Code launches become
managed rather than observed.

Two constraints are fixed regardless of mechanism:

- Account and OAuth state is identity-shared credential state. It is never
  copied into a generation, and isolation must not force a fresh login for
  every generation; if the native state file cannot be shared safely, the
  identity gets an explicit login flow.
- Claude Code reads repository assets (project instructions, project skills,
  project MCP registrations) directly from the worktree. If an unselected
  repository asset cannot be excluded through a proven native mechanism, the
  report states the residual load instead of claiming a clean runtime.

The adapter must inventory at least:

```text
real user-global configuration directory
real user state file (redacted to non-secret facts)
managed policy locations
repository instruction chain
repository .claude assets and .mcp.json chain
plugin and marketplace state
CLI and session flags
```

## 8. Authentication and Secrets

Authentication state is not normal configuration and is never imported from an
untrusted native home as part of runtime compilation.

Preferred order:

```text
OS credential store
  -> direnv-provided API key or secret reference
  -> identity-specific runtime login
  -> explicit, reviewed migration
```

Automatic copying or broad symlinking of native `auth.json`, token caches,
keyrings, `.ssh`, or cloud credential directories is prohibited.

Before the first Codex managed milestone, qualification must determine whether
OS-keyring credentials are safely reusable across isolated homes on the target
platform. If not, each identity receives an explicit runtime login flow.

## 9. Mutable State

Config artifacts are immutable per generation, but hosts create mutable state:

- sessions and archived sessions;
- logs and crash reports;
- SQLite databases;
- model or provider caches;
- trust decisions;
- memory; and
- installation metadata.

Each state class must be classified as:

```text
generation-local
worktree-shared
identity-shared
host-global external
prohibited import
```

Codex's own `CODEX_HOME`-resident state (sessions, SQLite databases,
`auth.json`) is classified `worktree-shared` (§7.1): scoped under
`OMCA_STATE_DIR`/worktree, not the generation directory, so it survives a
generation recompile instead of being silently wiped by one, and is never
copied into — or shared across — a different worktree's own state.

Sharing state through symlinks is allowed only for an explicit allowlist backed
by fixtures. A broad symlink to the native host home defeats isolation.

## 10. Repository Sources

Repository files are loadable project inputs, but they remain visible in the
report and subject to host trust. The default v1 treatment is:

| Repository source | Runtime behavior |
|---|---|
| Instructions | Included according to the adapter's proven native chain. |
| Skills | Cataloged as `AVAILABLE` unless intent changes them. |
| MCP servers | Cataloged as `AVAILABLE`; activation is risk-reviewed. |
| Hooks | Observed; explicit confirmation before activation. |
| Plugins/Extensions | Observed or opaque until executable trust is reviewed. |

Some hosts load repository assets directly from the real worktree even when
their user home is isolated. If OMCA cannot prevent an unselected repository
asset from loading through a proven host mechanism, it reports the limitation
instead of claiming a clean runtime. A future overlay workspace is a separate
capability, not an assumed behavior.

## 11. Diagnostic Modes

```bash
omca run codex --mode isolated
omca run codex --mode native
omca run codex --generation <id>
omca compare --native --current
omca diff current pending
omca bisect codex
omca rollback <generation-id>
omca doctor codex
```

- `isolated` is the default managed path.
- `native` is an explicit diagnostic baseline and may execute native Hooks or
  MCP servers; it requires a warning.
- `bisect` builds disposable generations that import candidate sources one at a
  time, in stable content-addressed order, and never activates any of them.
  `omca bisect --dry-run <host>` is a mandatory, always-available mode that
  reports the exact plan (which generations, importing which candidates, in
  what order) without compiling or writing anything to disk; omitting
  `--dry-run` runs the real path, which does compile disposable generations
  under the worktree's `generations/` directory but still never sets a
  `current`/`pending` pointer and never appends a Ledger entry -- a bisect run
  is purely an inspection aid, queryable afterward the same way any other
  generation is (`omca diff`, `omca compare`, or reading `manifest.json`
  directly).
- Failed post-activation verification triggers an automated `rollback` to the
  parent generation; the verification failure and the restoration are each
  their own Ledger entry, in that order. This is a distinct check from
  Activation's own compare-and-swap step (§5.4): the CAS check runs BEFORE the
  switch and validates that pending's desired-state INPUTS have not gone
  stale; post-activation verification runs AFTER the switch and validates
  that the now-current generation's own compiled artifact tree still matches
  what its manifest recorded -- has the compiled OUTPUT itself been
  corrupted, partially written, or tampered with. A first activation with no
  parent generation to restore is left as a clearly ledgered, honestly
  unrecoverable failure rather than a silently swallowed one.

Debugging remains tractable because native, current, pending, and historical
generations are all queryable and content-addressed.

## 12. Runtime Invariants

```text
the first managed host process never requires native user-global config
current artifacts do not change during a session
MCP writes desired state or pending generations, never current
activation is atomic and restart-bound
every generation has one complete manifest
native exclusions are explained rather than hidden
credentials are references or isolated state, not generated config
system-level residual behavior is reported explicitly
failed post-activation verification leaves a recoverable previous generation
bisect never activates a disposable generation it builds
```
