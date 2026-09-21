# oh-my-code-agent

`omca` is a local-first control plane for coding-agent runtimes.

It observes native host configuration without trusting it, models the parts
that can be proven through a vendor-neutral ontology, and reconciles an
explicit desired state into a per-worktree runtime for each directory or Git
worktree.

How much of the native user-global scope that runtime actually excludes
depends on the host's tier ([ADR 0006](docs/adr/0006-host-tiers.md)). Codex is
Tier 1: `HOME` is virtualized and the user-global scope is excluded. Claude
Code is Tier 2: its credentials are Keychain-bound, so `HOME` is left pointing
at the real user home and the native user-global Skills and MCP registrations
still load. `omca env` and `omca_status` state that residual rather than
reporting a clean runtime.

The primary product outcome is a trusted, explainable report. Configuration
management is deliberately limited to capabilities that have versioned
evidence and executable qualification fixtures.

Host support is *designed* to be plugin-based: one frozen adapter contract,
with first-party adapters for Claude Code and OpenAI Codex, and other hosts
joining through the same contract or remaining at the knowledge/observation
tier. The contract, its manifest, registry, conformance suite and
out-of-process transport are implemented and tested — but **no first-party
adapter exists yet**, and no production code path drives the contract. Host
semantics are currently hardcoded across eleven files in the core; see
[architecture §9.1](docs/architecture/README.md#91-where-host-semantics-actually-live)
for the inventory a migration has to move.

One desired state can give parallel hosts in the same worktree deliberately
different loadouts.

## Documentation

- [Project charter](init.md): goals, approved decisions, invariants, and MVP.
- [Documentation map](docs/README.md): source-of-truth boundaries and reading order.
- [Product requirements](docs/product/requirements.md): users, workflows, and defaults.
- [Architecture](docs/architecture/README.md): components, data model, interfaces, and storage.
- [Runtime architecture](docs/architecture/runtime.md): bootstrap isolation, direnv, and immutable generations.
- [Trusted reporting](docs/architecture/reporting.md): evidence, drift, MCP tools, and debugging.
- [Ontology](docs/ontology/README.md): canonical concepts and host mappings.
- [Knowledge lifecycle](docs/knowledge/README.md): versioned third-party facts and upgrades.
- [Architecture decision records](docs/adr/): frozen isolation, ownership, credential, knowledge update, and plugin distribution decisions.
- [Roadmap](docs/project/roadmap.md): gated implementation plan.

## Install from a reviewed checkout

```bash
GOBIN="$HOME/.local/bin" go install -trimpath ./cmd/omca
```

Add `$HOME/.local/bin` to PATH. The executable includes the reviewed Knowledge
Packs and ontology JSON; it can run after the build checkout is moved or removed.
Updating those built-in facts requires rebuilding from a reviewed revision.
No adjacent data directory or workspace submodule is needed at runtime.

## Development

```bash
omca --help  # list CLI entry points without opening the TUI
make build   # go build ./...
make test    # go test ./... -race -coverprofile=coverage.out
make cover   # print total coverage; CI floor is 67%
make lint    # golangci-lint run ./...
make fixtures
make standalone  # default asset lookup without build-source paths

# Safe, real-host qualification: no interactive session or model call.
omca qualify tui --json

# Human-only completion gate: two TUI launches plus an MCP model canary.
OMCA_QUALIFY_INTERACTIVE=1 omca qualify tui --host codex --interactive
```

`omca qualify tui` always creates a disposable HOME/XDG/state lane with
synthetic native MCP/Skill sentinels plus a repository-scoped managed Skill
sentinel. The automatic phase uses only host-reported, non-model introspection.
`--interactive` is deliberately
fail-closed behind a human acknowledgement because it launches the real host
TUI twice and may consume network/model quota; autonomous agents must not run
that phase.
Automatic probes use plain terminal output and a portable locale. Human TUI
launches preserve the caller's terminal and locale settings inside the same
isolated HOME/XDG lane.

Requires Go 1.22+. CI runs build+test with a 67% coverage floor, lint, a
markdown link check, and a secret-leak scan on every pull request.

## Product Model

```text
Observe -> Model -> Reconcile
```

Global native configuration is always an observable source, but it is not an
implicit parent of an OMCA-managed runtime.

```text
Native configuration -> observed and explained
Desired state        -> explicitly composed
Runtime generation   -> immutable, restartable; isolated at tier 1
```

The CLI, adapters and runtime generations are implemented. Passing build and unit
tests does not establish host qualification; the roadmap records the remaining gates.

Re-entering a worktree with `omca env` or launching through `omca run` preserves
the runtime you explicitly activated. Pending changes stay inactive. A broken
selection or host-version mismatch fails with a repair message instead of
silently reverting to bootstrap; activation and rollback remain explicit.

Rollback now checks the parent's immutable compilation host version before
switching state. Legacy generations remain launchable under their existing
version guard, but must be recompiled and activated before they can be rollback
targets; see [rollback compatibility](docs/architecture/runtime.md#rollback-compatibility-and-compiler-upgrades).

`omca run codex --mode native` preserves the complete calling environment. It
bypasses generation selection, but does not undo an inherited managed HOME or
host configuration. OMCA context markers trigger an additional warning that the
launch is not a clean native comparison, without printing their values.
