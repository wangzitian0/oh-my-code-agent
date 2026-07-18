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
| `codex` | `instruction` | none documented | `UNKNOWN` | **E1** | `knowledge/hosts/codex/cli/0.144/manifest.json` declares `capabilities.instruction.resolve: UNKNOWN`, so the E2 gate never opens. `fixtures/README.md`'s "Precedence questions this corpus marks UNKNOWN, and why" establishes no safe, non-interactive, no-network, no-model-call flag exists on `codex` that dumps effective/merged configuration; `codex --help` was read in full and no such flag was found. The documented claim "closer instructions appear later" (`docs/ontology/README.md` §6.2) stays a `documentedClaim` at E1, never promoted to `selectedSource`. |
| `codex` | `skill` | none documented | `UNKNOWN` | **E1** | Same manifest gate (`capabilities.skill.resolve: UNKNOWN`). `fixtures/README.md`'s static-inspection finding that `$CODEX_HOME/skills` is an undocumented fourth discovery root is itself only E1 (read-only `strings` extraction, never behaviorally confirmed) — it *adds* a known unknown rather than raising this cell. `knowledge/hosts/codex/cli/0.144/manifest.json`'s `knownUnknowns[0]` names this explicitly. |
| `codex` | `mcp_server` | none documented | `UNKNOWN` | **E1** | Same manifest gate (`capabilities.mcp_server.resolve: UNKNOWN`) and the same `fixtures/README.md` finding: no safe flag dumps merged MCP registration state. |
| `claude-code` | `instruction` | none documented | `UNKNOWN` | **E1** | `knowledge/hosts/claude-code/cli/2.1/manifest.json` declares `capabilities.instruction.resolve: UNKNOWN`. `claude --help` was read in full; no safe merged-configuration-dump flag exists (`fixtures/README.md`). The documented claim "enterprise > personal > project > bundled" stays a `documentedClaim` at E1. |
| `claude-code` | `skill` | none documented | `UNKNOWN` | **E1** | Same manifest gate. **Cross-reference issue #47** (open): whether `CLAUDE_CONFIG_DIR` fully relocates the Skill discovery root — and so whether a generation's user-global Skill exclusion actually holds — is E1 static-inspection evidence only (`fixtures/README.md`'s "Claude Code's config-directory override variable" finding, read-only `strings` extraction, never a live launch). This table's E1 ceiling is exactly what issue #47 needs to close before it can rise: any report claim about Claude Code Skill isolation must not exceed E1 until that issue lands stronger evidence. |
| `claude-code` | `mcp_server` | none documented | `UNKNOWN` | **E1** | Same manifest gate. Same issue #47 cross-reference: `CLAUDE_CONFIG_DIR`'s relocation of the `~/.claude.json`-equivalent MCP/trust state file is the same E1 static-inspection finding, not behaviorally confirmed. |
| `codex` | `host` (binary identity/version) | `codex --version` | n/a (not a Knowledge Pack concept) | **E3** | `codex --version` is safe (non-interactive, no network, no model call — verified against `codex --help`, `fixtures/README.md`'s "Safety boundary" section), already implemented as the only invocation `internal/context/host.go`'s `probeVersion` ever makes, and its output is a native, host-reported answer to "which version is installed" — exactly E3's definition (`docs/architecture/reporting.md` §4: "a native status, environment, debug, or introspection interface confirmed it"). This is a claim about the host binary itself, not about any of the three ontology concepts above, so it does not raise their rows. |
| `claude-code` | `host` (binary identity/version) | `claude --version` | n/a (not a Knowledge Pack concept) | **E3** | Same reasoning as `codex` / `host`, via `claude --version` (`fixtures/README.md`'s host binary version section; `internal/context/host.go`'s `probeVersion`). |

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
