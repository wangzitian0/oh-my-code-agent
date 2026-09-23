# Evidence Ceiling Table

Status: draft

## 1. Purpose

`docs/architecture/reporting.md` §4 defines E0-E5. PR-22 (issue #26) scopes
E0-E3 only. This table is the anti-drift rule the round-2 audit of that issue
added: for every `host x concept` cell OMCA reports on, it names **which
native introspection surface actually exists today**, **which Evidence Level
is honestly reachable as a result**, and **why** — grounded only in evidence
already committed elsewhere in this repository (`fixtures/README.md`,
`knowledge/hosts/*/*/*/manifest.json`), never in what a surface might
plausibly offer.

This file is the reviewable artifact: raising a cell's Ceiling requires new
committed evidence (a fixture, a manifest capability change, a documented
introspection command) landing in the same pull request that edits this
table, exactly like a Knowledge Pack capability promotion
(`docs/knowledge/README.md` §8, "Automation may create the candidate ... It
may not ... promote capability levels without maintainer review"). Lowering a
cell is always allowed unilaterally — an honest downgrade never needs
external permission.

`internal/assurance/ceiling.go`'s `Ceilings` table is this document's
machine-readable mirror; `internal/assurance`'s tests fail if the two ever
name different Ceiling values for the same cell (`ceiling_test.go`'s
`TestCeilings_MatchThisDoc`). Edit both together.

## 2. How to read a row

- **Introspection surface**: the safe, non-interactive, no-network,
  no-model-call command or interface this repo has actually documented for
  this cell, or "none documented" when
  `fixtures/README.md` already establishes that none exists.
- **Resolve capability**: the concept's `capabilities.<concept>.resolve`
  value in the host's current committed Knowledge Pack manifest — the E2 gate
  (`docs/knowledge/README.md` §5; `internal/effective/merge.go`'s
  `capabilityQualified`).
- **Ceiling**: the strongest Evidence Level any OMCA-reported conclusion for
  this cell may honestly carry today, `max(reachable via resolve, reachable
  via introspection)`, never inferred past either.
- **Why**: the specific committed finding this Ceiling is grounded in.

## 3. Table

| Host | Concept | Introspection surface | Resolve capability | Ceiling | Why |
|---|---|---|---|---|---|
| `codex` | `instruction` | none documented | `EXACT` | **E2** | `knowledge/hosts/codex/cli/0.154.0/manifest.json` declares `capabilities.instruction.resolve: EXACT`, promoted from `UNKNOWN` by issue #127: `internal/qualify/instruction_discovery_test.go` is an executable fixture reproducing `codex-rs/core/src/agents_md.rs`'s documented algorithm (project root = nearest ancestor with a `project_root_markers` hit, default `.git`; `AGENTS.override.md` then `AGENTS.md` then configured fallbacks, first hit per directory, collected root-to-cwd inclusive; never walks past the project root) against a real, isolated temp directory tree. `discover` stays `PARTIAL`: the fixture proves the merge/precedence algorithm, not every discovery-root edge case. Older manifest versions (0.144, 0.146-0.147, 0.153.4) are unchanged and remain `UNKNOWN`. |
| `codex` | `skill` | none documented | `UNKNOWN` | **E1** | Same manifest gate (`capabilities.skill.resolve: UNKNOWN`). `fixtures/README.md`'s static-inspection finding that `$CODEX_HOME/skills` is an undocumented fourth discovery root is itself only E1 (read-only `strings` extraction, never behaviorally confirmed) — it *adds* a known unknown rather than raising this cell. `knowledge/hosts/codex/cli/0.144/manifest.json`'s `knownUnknowns[0]` names this explicitly. |
| `codex` | `mcp_server` | none documented | `UNKNOWN` | **E1** | Same manifest gate (`capabilities.mcp_server.resolve: UNKNOWN`) and the same `fixtures/README.md` finding: no safe flag dumps merged MCP registration state. |
| `claude-code` | `instruction` | none documented | `UNKNOWN` | **E1** | `knowledge/hosts/claude-code/cli/2.1/manifest.json` declares `capabilities.instruction.resolve: UNKNOWN`. `claude --help` was read in full; no safe merged-configuration-dump flag exists (`fixtures/README.md`). The documented claim "enterprise > personal > project > bundled" stays a `documentedClaim` at E1. Issue #127 deliberately did **not** promote this cell, unlike the codex and pi rows: its fixture (`internal/qualify/instruction_discovery_test.go`) proves the ancestor-directory-chain axis (`CLAUDE.md`/`CLAUDE.local.md` from cwd upward, unbounded, concatenated root-to-cwd), but `fixtures/claude-code/2.1.211/instructions-collision` adjudicates a *different* axis — user-level `~/.claude/CLAUDE.md` versus project-level — for which code.claude.com/docs/en/memory itself states "There is no hard precedence rule between levels". One coarse per-concept `resolve` flag cannot express "proven on one axis, unproven on another", so promoting it would have made `internal/effective` pick a winner for an ordering the vendor does not define. |
| `claude-code` | `skill` | none documented | `UNKNOWN` | **E1** | Same manifest gate. **Cross-reference issue #47** (open): whether `CLAUDE_CONFIG_DIR` fully relocates the Skill discovery root — and so whether a generation's user-global Skill exclusion actually holds — is E1 static-inspection evidence only (`fixtures/README.md`'s "Claude Code's config-directory override variable" finding, read-only `strings` extraction, never a live launch). This table's E1 ceiling is exactly what issue #47 needs to close before it can rise: any report claim about Claude Code Skill isolation must not exceed E1 until that issue lands stronger evidence. |
| `claude-code` | `mcp_server` | none documented | `UNKNOWN` | **E1** | Same manifest gate. Same issue #47 cross-reference: `CLAUDE_CONFIG_DIR`'s relocation of the `~/.claude.json`-equivalent MCP/trust state file is the same E1 static-inspection finding, not behaviorally confirmed. |
| `pi` | `instruction` | none documented | `EXACT` | **E2** | `knowledge/hosts/pi/cli/0.86.1/manifest.json` declares `capabilities.instruction.resolve: EXACT`, promoted from `UNKNOWN` by issue #127: `internal/qualify/instruction_discovery_test.go` is an executable fixture reproducing the discovery order measured in infra2-harness's `handover.context-and-gates.md` (2026-09-21: reads both `AGENTS.md` and `CLAUDE.md`, unbounded upward from cwd, a symlinked pair not double-loaded, a non-symlinked `AGENTS.md`/`CLAUDE.md` pair in the same directory both counting) against a real, isolated temp directory tree with real symlinks. pi's non-interactive `-p` mode still performs a model call and stays outside the safety boundary; `knowledge/hosts/pi/cli/0.85/manifest.json` is unchanged and remains `UNKNOWN`. |
| `pi` | `skill` | none documented | `UNKNOWN` | **E1** | Same manifest gate. The pack's `knownUnknowns` record that duplicate-name resolution across the four skill roots is unproven and that root-level `.md` skill discovery is a declared adapter gap; no native interface dumps the discovered skill list without launching a session. |
| `pi` | `mcp_server` | none documented | `UNSUPPORTED` | **E1** | pi has no declarative native MCP registry: MCP servers are implemented through extensions (executable code). The pack declares `capabilities.mcp_server` discover/resolve `UNSUPPORTED`, and the adapter deliberately does not translate extension code into `mcp_server` entities, so this cell's ceiling is the observation tier's E1. |
| `codex` | `host` (binary identity/version) | `codex --version` | n/a (not a Knowledge Pack concept) | **E3** | `codex --version` is safe (non-interactive, no network, no model call — verified against `codex --help`, `fixtures/README.md`'s "Safety boundary" section), already implemented as the only invocation `internal/context/host.go`'s `probeVersion` ever makes, and its output is a native, host-reported answer to "which version is installed" — exactly E3's definition (`docs/architecture/reporting.md` §4: "a native status, environment, debug, or introspection interface confirmed it"). This is a claim about the host binary itself, not about any of the three ontology concepts above, so it does not raise their rows. |
| `claude-code` | `host` (binary identity/version) | `claude --version` | n/a (not a Knowledge Pack concept) | **E3** | Same reasoning as `codex` / `host`, via `claude --version` (`fixtures/README.md`'s host binary version section; `internal/context/host.go`'s `probeVersion`). |
| `pi` | `host` (binary identity/version) | `pi --version` | n/a (not a Knowledge Pack concept) | **E3** | Same reasoning as the `codex`/`claude-code` host rows, via `pi --version` (safe: non-interactive, no network, no model call; prints a bare `MAJOR.MINOR.PATCH` line, verified against the installed 0.85.1 release; `internal/context/host.go`'s `probeVersion`). |

## 4. What would raise a cell

- An `instruction`/`skill`/`mcp_server` row rises to **E2** only when that
  concept's Knowledge Pack `resolve` capability is promoted to `EXACT` or
  `COMPATIBLE` with committed qualification evidence
  (`docs/knowledge/README.md` §5, §10) — a Knowledge Pack change, not an
  `internal/assurance` code change.
- An `instruction`/`skill`/`mcp_server` row rises to **E3** only when a safe,
  non-interactive, no-network, no-model-call native introspection command is
  *found* on the real binary (re-reading `--help`, or a new subcommand
  shipped in a later host version) and proven in a committed fixture,
  mirroring how `internal/context/host.go`'s `probeVersion` already proves
  `--version` is safe. No such command is known to exist today for any of the
  six concept cells above.
- The `claude-code` / `skill` and `claude-code` / `mcp_server` rows
  specifically track issue #47's exit gate; closing it is exactly the kind of
  evidence that would justify raising them.

## 5. What this table does not cover

E4 (`BEHAVIOR_PROBED`) and E5 (`EXTERNALLY_PROVEN`) are out of this PR's
scope (issue #26's own title: "Evidence E0-E3 + verify"). This table never
names an E4/E5 ceiling for any cell — canary probing is a separate,
later capability (`docs/architecture/reporting.md` §13) this table does not
anticipate or reserve room for.
