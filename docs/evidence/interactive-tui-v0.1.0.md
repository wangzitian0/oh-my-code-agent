# Interactive TUI Qualification Evidence — v0.1.0

Date: 2026-08-18  
Platform: macOS 15.3.2, arm64  
Command under test: candidate `omca qualify tui --json`

> **Superseded for Claude Code as of PR #109.** Every Claude Code row below was
> measured against the pre-#109 shim, which virtualized `HOME` unconditionally.
> PR #109 made virtualization conditional and set `CanVirtualizeHome: false`
> for `claude-code`, so on any build carrying that flag the host launches on
> the real user home and its native user-global MCP registrations and Skills
> are no longer excluded. The `PASS` on the Claude Code MCP-exclusion row does
> not describe current behavior and must be re-measured before it is cited
> again. Codex rows are unaffected — it remains Tier 1. See
> [ADR 0006](../adr/0006-host-tiers.md).

## Scope

This artifact records only the safe automatic phase. It did not open an
interactive host session, call a model, authenticate, or consume model quota.
The command created a disposable HOME/XDG/worktree/state lane containing native
user-global MCP/Skill entries named `omca-native-sentinel` plus a
repository-scoped Skill named `omca-managed-sentinel`. The native canaries must
be absent and the managed repository Skill must be present in every applicable
host-reported inventory.

The relevant real native configuration roots were content-snapshotted before
and after the command. The automated run reported no differences, and an
independent shell-side content digest over the same roots also remained equal.
Raw native content, host output, credentials, and absolute scratch paths are
intentionally not stored here.

The qualification client follows the current official Codex app-server
handshake and scopes `skills/list` to the exact scratch cwd. The current
[app-server reference](https://developers.openai.com/codex/app-server) documents
`initialize`/`initialized`, `cwds`, and `forceReload`; the current
[Skills reference](https://developers.openai.com/codex/skills) documents
repository `.agents/skills` discovery.

## Results

| Host | Version | Check | Evidence | Result |
|---|---:|---|---|---|
| Codex CLI | 0.147.0 | `mcp list --json` reports `omca`; native sentinel absent | E3 host-reported | PASS |
| Codex CLI | 0.147.0 | app-server `skills/list` for the exact cwd reports 7 Skills; managed repository sentinel present; native sentinels absent | E3 host-reported | PASS |
| Codex CLI | 0.147.0 | matching Knowledge Pack `codex:cli:0.146-0.147` | E2 | PASS |
| Codex CLI | 0.147.0 | initial/restart TUI plus `omca_status` model canary | E0 | UNKNOWN — human gate not run |
| Claude Code | 2.1.228 | `mcp list` reports `omca` connected; native sentinel absent | E3 host-reported | PASS |
| Claude Code | 2.1.228 | matching Knowledge Pack `claude-code:cli:2.1` | E2 | PASS |
| Claude Code | 2.1.228 | managed repository Skill inclusion plus native Skill exclusion | E1 | UNKNOWN — no safe non-interactive Skill inventory exposed |
| Claude Code | 2.1.228 | initial/restart TUI plus `omca_status` model canary | E0 | UNKNOWN — human gate not run |
| Both | — | relevant real native roots unchanged during probe window | internal byte snapshot + independent content digest | PASS |

An earlier 2026-07-30 run established the same automatic MCP/Skill boundary for
Codex 0.146.0 and the MCP boundary for Claude Code 2.1.220. Together the two
exact Codex observations bound the conservative `>=0.146.0 <0.148.0` pack.

The automatic command exits non-zero because UNKNOWN is not completion. Every
automatic host result includes the unrun human TUI/model gate, and Claude Code
also retains its Skill-inventory UNKNOWN. A Codex-only automatic run therefore
cannot report a false complete result even though all automatic checks pass.

## Current-host recheck — 2026-09-15

The safe automatic lane was repeated at `2026-09-15T06:14:31Z` on macOS arm64
with Codex 0.153.4 and Claude Code 2.1.267. Codex MCP inventory contained only
`omca`; its Skills inventory contained seven entries, including the managed
repository sentinel and excluding native sentinels. Claude reported `omca`
connected and excluded its native MCP sentinel. The native configuration
before/after snapshot was unchanged (`realNativeStateClean=true`). No
interactive session or model call was attempted.

Before this repair, Codex rejected generated configuration because
`approval_policy="untrusted"` is no longer supported. The shared compiler now
omits that setting and emits an untrusted-project entry for the exact worktree,
following the [official approval migration](https://learn.chatgpt.com/docs/agent-approvals-security).
The read-only sandbox default remains. The documentation supports the command
approval semantics; the executed probe proves configuration loading and
inventory isolation, not approvals during a model turn. Regression fixtures
cover escaped project paths and the absence of an overriding approval policy.

The new exact-version `codex:cli:0.153.4` pack records these facts with all
reconciliation modes still `OBSERVED`. Earlier packs and their historical
evidence remain unchanged. Full and bootstrap cache identities include the
host compiler revision. Diagnostic truncation now retains the tail of a host
failure so startup estimates cannot conceal the actual configuration error.

Both hosts still report the human TUI/restart/model canary as UNKNOWN. Claude
Skill inventory remains UNKNOWN. Overall completion remains false and the
automatic command exits 1 as designed; passing these inventory checks does not
complete the interactive MVP.

## Codex 0.154.0 candidate — 2026-09-15

The installed Codex upgraded after the earlier recheck. The automatic lane ran
at `2026-09-15T07:59:01Z` using reviewed OMCA `fed8928` on darwin-arm64.
Its [complete JSON artifact](codex-0.154.0-safe-qualification.json) records the
`knowledge-pack=UNKNOWN` before adding the 0.154.0 Knowledge Pack, passing
MCP and Skill isolation, and clean native snapshots. Codex itself was already
version 0.154.0 in this run. Invocation and synthetic inputs are the same `omca qualify
tui --json` lane described above: only `--version`, `mcp list --json`, and the
app-server `initialize`/`initialized`/`skills/list` exchange; no model turn.

The candidate binary repeated the Codex-only probe at
`2026-09-15T09:28:35Z`; its [post-update JSON artifact](codex-0.154.0-candidate-qualification.json)
records `knowledge-pack=PASS`, MCP/Skills PASS, unchanged native snapshots, and
the human gate still UNKNOWN. The manifest pins this newer artifact's bytes;
the earlier artifact remains the evidence for the original missing-pack finding.

Candidate review:

- Version range: add `>=0.154.0 <0.154.1`; retain every historical pack unchanged.
- Sources: re-read the official configuration, MCP, Skills, and app-server
  references. The new manifest records SHA-256 digests of the fetched Markdown
  bytes. Earlier sources had no digest baseline, so no upstream content diff is
  claimed. The observed binary version changed from 0.153.4 to 0.154.0.
- Discovery: no change observed in the tested repository-Skill inclusion and
  native-Skill/MCP exclusion cases. Collision precedence remains unverified.
- Capability impact: Skill, MCP, instruction, and permission modes all stay
  `OBSERVED`; no write authority expands. Only inventory and configuration-load
  checks have host-reported proof. No adapter change is required for these cases.
- Generations: old generations retain their pinned knowledge and host version;
  the existing launch guard requires an explicit rebuild after a host upgrade.
- Regression gate: the default repository must resolve 0.154.0 to its exact pack
  while leaving 0.154.1 and 0.155.0 unqualified and every mode `OBSERVED`.
- Unknowns: both hosts' initial/restart TUI and model canaries remain unrun;
  Claude 2.1.272 has no safe Skill inventory. Overall completion remains false.

## pi 0.86.1 candidate — 2026-09-21

pi 0.86.1 (darwin-arm64) is covered by the `pi:cli:0.86.1` observation pack.
The 0.85.1 → 0.86.1 diff touches no concept on omca's observation surface
(`docs/skills.md` is byte-identical; 0.86.x changes are extension-SDK shapes,
provider cache warming, `/bug`, `PI_RADIUS_GATEWAY`), so capability facts and
knownUnknowns carry over verbatim from `pi:cli:0.85`. Read-only probes
(`pi --version`, `pi --help`) left every native `*.json`/`*.jsonl` byte
unchanged ([artifact](pi-0.86.1-candidate-qualification.json), digest pinned
in the pack). The interactive TUI stays unattempted (observation tier; human
gates open), and `PI_CODING_AGENT_DIR` relocation remains a declared
knownUnknown until a behavioral proof lands.

## Human completion procedure

The remaining E4 proof is:

```bash
OMCA_QUALIFY_INTERACTIVE=1 omca qualify tui --host codex --interactive
OMCA_QUALIFY_INTERACTIVE=1 omca qualify tui --host claude-code --interactive
```

For each host the harness launches the isolated TUI twice. A human must verify
that `/mcp` contains only managed entries, `/skills` includes
`omca-managed-sentinel` while excluding `omca-native-sentinel`, and a minimal
model prompt can call `omca_status` and return the managed host/generation. This
has not yet been run and must not be represented as complete.

## Safety finding during harness development

An early cleanup implementation called `chmod` on every scratch entry before
removal. Codex creates scratch-local symlinks back to its installed native
binary; on macOS, `chmod` followed those links and removed the target's
executable bit. The installed Codex binary was immediately restored to mode
`0755`, `codex --version` succeeded afterward, and a regression test now proves
that scratch cleanup never chmods an external symlink target. A repeated real
qualification run preserved mode `0755` before and after.

`claude doctor` was also rejected as an automatic qualification primitive:
despite being non-interactive, version 2.1.220 probes Keychain write capability
and reports virtual-HOME installation warnings. `omca qualify tui` uses the
narrow `mcp list` surface instead and never invokes `doctor`.
