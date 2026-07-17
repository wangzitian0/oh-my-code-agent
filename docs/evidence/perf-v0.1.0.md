# Performance Evidence — v0.1.0 (M1 exit)

Status: committed evidence, reviewed per release (round-2 acceptance-criteria
addendum on issue #15/PR-11: "the strict numbers live in the committed
evidence, reviewed per release" — this file, not a `go test` assertion, is
where the reference-machine targets are checked).

This file is produced by `make perf` (`internal/perf`, PR-11), run once on
the reference machine described below. `make perf` prints two structurally
different measurements, and this file keeps them in the same two clearly
labeled sections `internal/perf/doc.go` documents — a synthetic-fixture
number and a real-environment number are never averaged or merged into one
figure.

## Reference machine

Recorded via `sw_vers`, `uname -a`, and `sysctl` on the machine this PR's
evidence was measured on — nothing here is estimated or assumed.

| Fact | Value |
|---|---|
| OS | macOS 15.3.2 (Darwin kernel 24.3.0, BuildVersion 24D81) |
| Architecture | arm64 |
| CPU | Apple M4 Pro |
| Logical CPUs | 14 (`sysctl hw.ncpu`) |
| Memory | 24 GiB (`sysctl hw.memsize` = 25769803776 bytes) |
| Go toolchain | go1.22.12 darwin/arm64 |

## Host CLI versions detected on this machine

Detected by `internal/context.DetectHost`'s own `--version` probe (the same
read-only, non-interactive, no-network invocation this project's whole test
suite already relies on — see `internal/context/host.go`'s `versionArgs` doc
comment) — not asserted or hand-typed.

| Host | Version | Resolved path |
|---|---|---|
| codex | codex-cli 0.144.5 | `/Users/SP14016/.asdf/shims/codex` (asdf-managed Node package, see `fixtures/README.md`'s acquisition-method notes for why this indirection matters to interpreting the timing numbers below) |
| claude-code | 2.1.212 (Claude Code) | `/Users/SP14016/.local/bin/claude` (native updater/installer) |

## Reference-machine targets (round-2 addendum)

```text
steady-state       <= 100ms
first bootstrap    <= 2s
```

These targets apply to the **synthetic-fixture** measurement below — the
number that isolates OMCA's own detect+observe+compile-or-reuse overhead
from the wrapped host binary's own startup latency (`internal/perf/doc.go`:
"it never measures the wrapped host binary's own startup time... explicitly
out of scope"). Both targets are met with wide margin; see the table below.

## 1. Synthetic-fixture measurement (`MeasureSynthetic`)

Hermetic: a fresh temp-directory fixture containing 30 fake native
user-global MCP servers and 20 fake Skills (matching PR-09's own
`TestBootstrap_Codex_30MCPServersAnd20Skills_NoneLeak` fixture size), and a
fake `codex --version` script standing in for the real binary — no real
`~/.codex`/`~/.claude` is ever read, and no real host binary is ever
invoked. Command: `make perf` (`go test ./internal/perf/... -run TestPerf -v`).

| Phase | What it measures | n | min | mean | p95 | max | Target | Result |
|---|---|---|---|---|---|---|---|---|
| First bootstrap | DetectWorktree + DetectHost (fake `--version`) + Observe + `EnsureGeneration` (must compile) + `SetCurrentGeneration` | 5 | 19.2ms | 39.7ms | 111.6ms | 111.6ms | <= 2s | **PASS** (~50x margin on mean) |
| Steady state | identical sequence, `EnsureGeneration` takes the no-recompile reuse path | 20 | 15.6ms | 21.9ms | 30.4ms | 30.4ms | <= 100ms | **PASS** (4.6x margin on mean) |
| Shim entry (supplementary) | `internal/shim.Build` alone: resolve-current-generation + prepare-exec-args, no detect/observe/compile | 20 | 0.28ms | 0.53ms | 1.27ms | 1.27ms | (no separate target; well inside the roadmap's "tens of milliseconds" framing) | — |

Steady-state entry total (the number the M1 exit gate's "shim plus
bootstrap overhead... stays inside the launch budget" line names) is
steady-state-bootstrap + shim-entry: **~22.4ms mean**, roughly 4.5x under
the 100ms reference target, on synthetic data sized to match the
30-MCP/20-skill fixture PR-09 established as this project's own "the
mechanism scales" proof point.

## 2. Real-environment measurement (`MeasureRealEnvironment`)

This is the round-2 addendum's own required line item: "on the developer's
actual machine and worktrees... record native vs managed startup time and
context cost before/after." `internal/context.DetectHost`'s real
`--version` probe and `internal/observe.Observe`'s real, read-only walk ran
against this machine's actual, real `~/.codex` and `~/.claude` — safe by
construction (see `internal/perf/doc.go` and the safety boundary this PR's
task text names). Every compiled generation landed under a scoped temp
directory, never the real `$XDG_STATE_HOME/omca`.

This machine genuinely has real user-global sources to measure against —
not a fabricated "dozens" scenario:

| Host | Native MCP servers registered (single config source) | Native Skills discovered |
|---|---|---|
| codex | 9 (`$CODEX_HOME/config.toml`'s `[mcp_servers.*]` tables) | 26 (`$HOME/.agents/skills` + `$CODEX_HOME/skills`, the latter empty on this machine) |
| claude-code | 3 (`~/.claude.json`'s top-level `mcpServers`) | 43 (`~/.claude/skills` + `$HOME/.agents/skills`) |

The exclusion count `omca_status`/`omca doctor`/`omca env` all report is
**1** native MCP *configuration source* per host, not 9 or 3 — see
`internal/mcp/status.go`'s `HostStatus.ExcludedMCPServers` doc comment for
why: `internal/observe`'s `mcp_server` concept is file-level (one
observation per registration file, not per registered server inside it),
so a generation's manifest records "this one file was excluded," and that
is genuinely all the manifest can honestly claim without re-reading the
real native file's content at report time (which this project deliberately
never does — see that same doc comment). The Skill count (26 / 43) has no
such ceiling: `internal/observe` emits one observation per discovered
`SKILL.md` file, so it is a true per-Skill count, and both numbers are
comfortably "dozens" on their own.

| Host | Phase | n | min | mean | p95 | max |
|---|---|---|---|---|---|---|
| codex | First bootstrap | 3 | 110.3ms | 125.5ms | 137.3ms | 137.3ms |
| codex | Steady state | 5 | 102.3ms | 136.7ms | 177.6ms | 177.6ms |
| codex | Shim entry | 5 | 0.30ms | 0.70ms | 1.55ms | 1.55ms |
| claude-code | First bootstrap | 3 | 75.6ms | 77.6ms | 81.3ms | 81.3ms |
| claude-code | Steady state | 5 | 78.2ms | 83.9ms | 91.7ms | 91.7ms |
| claude-code | Shim entry | 5 | 0.25ms | 0.31ms | 0.36ms | 0.36ms |

These real-environment steady-state numbers (103-178ms) are noticeably
higher than the synthetic-fixture number (16-30ms) and sit above the 100ms
reference target — **this is expected and is not a regression to chase**:
the dominant cost here is the real `--version` subprocess's own startup
latency (codex in particular resolves through an asdf Node shim, itself a
multi-hop process spawn — see `fixtures/README.md`'s acquisition-method
notes), which `internal/perf/doc.go` and this PR's own safety boundary
explicitly name as "native and outside OMCA's control to measure or
optimize." The synthetic-fixture measurement above is the one that isolates
OMCA's own code cost from that external, host-binary-dependent latency, and
is therefore the one the reference-machine targets apply to. Shim-entry
overhead (0.25-1.55ms) — the part of a *subsequent* PATH-shim invocation
that never re-runs detect/observe/compile — stays sub-millisecond on real
data too, confirming the "tens of milliseconds, not seconds" roadmap
framing describes the shim's own contribution accurately.

### Context-cost delta (native vs. managed), this machine, this run

| Host | Excluded native MCP config source(s) | Excluded native Skill(s) | Estimated context-cost delta | Method | Confidence |
|---|---|---|---|---|---|
| codex | 1 | 26 | 4100 tokens | 1 x ~200 tokens/source + 26 x ~150 tokens/description | estimate, not measured |
| claude-code | 1 | 43 | 6650 tokens | 1 x ~200 tokens/source + 43 x ~150 tokens/description | estimate, not measured |

**Native baseline vs. managed, in words**: an unmanaged native launch on
this machine would have loaded all 9 (codex) / 3 (claude-code) registered
MCP servers and all 26 (codex) / 43 (claude-code) discovered Skills into
every session, unconditionally. A managed bootstrap launch loads **zero** of
them (`internal/runtime`'s M1 bootstrap policy excludes every native
user-global source unconditionally — see
`internal/runtime/bootstrap_codex_test.go`'s own 30-MCP/20-skill
"none leak" proof, mirrored on real data here). The estimated context-cost
delta above is the token-budget expression of that same zero-vs-native
gap — see the "estimated context-cost delta" method below for how the
200/150-token-per-item constants were chosen and why the result is labeled
an estimate, never a measurement.

## Context-cost estimate: method and confidence

`internal/mcp.EstimateContextCost` multiplies two fixed, documented
per-item token averages (`internal/mcp/status.go`) by the exclusion counts
above:

- **~200 tokens per excluded native MCP configuration source.** Chosen from
  a small manual sample of real MCP tool-schema JSON this project's own
  fixtures and this machine's real `~/.claude.json`/`~/.codex/config.toml`
  contain — a single registered server's JSON-RPC `tools/list` schema
  entry (name, description, JSON Schema `inputSchema`) commonly runs
  150-300 tokens; 200 is a round middle estimate, not a corpus-derived
  average.
- **~150 tokens per excluded Skill.** Chosen from a small manual sample of
  this repository's and this machine's real `SKILL.md` frontmatter-plus-
  summary text (the part a host typically loads into context to *decide
  whether* to invoke a Skill, not the full skill body) — commonly
  100-200 tokens.

**Confidence: estimate, not measured** (`internal/mcp.
ConfidenceEstimateNotMeasured`) — no real MCP tool-schema or Skill
description text was tokenized for this measurement; the numbers above are
exclusion counts multiplied by fixed constants, not a token count produced
by an actual tokenizer run against real excluded content. This project's
`domain.EvidenceLevel` (E0-E5) vocabulary describes how strongly a claim
about *native host behavior* was established and does not fit a modeling
assumption about token economics, so this PR deliberately uses the plainer,
equally-honest "estimate, not measured" label instead of forcing an E-level
that would overstate what was actually established — see
`internal/mcp/status.go`'s `ConfidenceEstimateNotMeasured` doc comment for
the full reasoning. A future PR that actually tokenizes real excluded
content (impossible today without reading native config at report time,
which this project deliberately avoids — see the exclusion-count table
above) could raise this to a real measurement.

## Reproducing this evidence

```bash
make perf
```

Synthetic numbers reproduce deterministically in shape (fixture size, phase
count) though not in exact timing (real wall-clock, subject to machine
load). Real-environment numbers depend on this machine's actual installed
`codex`/`claude` and real native configuration; a machine with neither
installed will print "not installed" for both hosts and skip that section's
numbers entirely — reported honestly, never fabricated (see
`internal/perf/doc.go`'s "UNKNOWN is safer than a guessed number" note).
