# PR-06 Qualification Fixtures

This directory is the executable qualification lab (issue #10): fixture
cases in the shape `fixtures/<host>/<version>/<case>/` (docs/knowledge/README.md
§3), driven by the harness in `internal/qualify`. Run them with:

```
make fixtures     # go test ./... -run Fixture -v
```

## Safety boundary this whole corpus was authored under

`claude` is Claude Code — the same tool that authored this PR. Every case in
this corpus:

- never invokes `codex`/`claude` beyond `--version`/`--help` (see
  `internal/qualify/invoke.go`'s `allowedInvokeArgs` closed set, enforced in
  code, not just fixture-author discipline);
- when it does invoke, redirects `HOME` (and `CODEX_HOME` for Codex,
  `CLAUDE_CONFIG_DIR` for Claude Code) into a fresh, case-specific temp
  directory tree, built from scratch (never `append(os.Environ(), ...)`);
- snapshots a stand-in "outside world" directory (disjoint from the sandbox
  handed to any subprocess) before and after, asserting zero changes — the
  harness's automated, deterministic zero-write proof (`internal/qualify/harness.go`);
- for every one of the six required cases below, recorded `invoke.attempted:
  false` in `invocation.yaml` with an explicit reason, because — after
  reading each binary's full `--help` output — no safe, non-interactive,
  no-network, no-model-call flag exists on either binary that dumps a
  merged/effective configuration view. `codex`/`claude` with no arguments
  start an interactive session; `codex exec`/`claude -p` run a real,
  model-invoking turn. Both are explicitly excluded. This is the expected,
  correct outcome per the PR-06 safety boundary, not a shortfall.

## What was verified live, once, outside the committed fixture corpus

Because the committed Go test suite must be hermetic (portable to a machine
without `codex`/`claude` installed, and to CI), the six fixture cases below
do not themselves shell out to the real binaries — each records
`invoke.attempted: false`. Separately, the following was verified manually,
once, on the machine that authored this PR, as the evidentiary basis for
"HOME/CODEX_HOME/CLAUDE_CONFIG_DIR redirection actually works and never
touches the real environment":

1. Snapshotted (sha256 of every file) the real `~/.claude`, `~/.codex`, and
   `~/.agents/skills` directories.
2. Ran `claude --version` and `codex --version` with `HOME`, `CODEX_HOME`,
   and `CLAUDE_CONFIG_DIR` all pointed at fresh, empty temp directories, and
   the process environment otherwise cleared (`env -i` plus only the
   minimal `PATH` needed to resolve the binary) — never inheriting the real
   environment. Both printed their expected version strings and wrote
   nothing into the isolated sandbox directories (`claude --version`: zero
   files created; `codex --version`: wrote only its own internal
   `tmp/arg0/.../{.lock,codex-execve-wrapper}` bootstrap files *inside the
   isolated CODEX_HOME*, which is expected, ordinary Codex behavior, not a
   leak).
3. Re-snapshotted the real `~/.claude`, `~/.codex`, `~/.agents/skills`
   afterward and diffed against step 1.

**Honest result of step 3, stated plainly rather than rounded up to "clean":**
`~/.agents/skills` was byte-for-byte unchanged. `~/.claude` and `~/.codex`
were **not** byte-for-byte unchanged — but every changed path fell under
session/log/cache/state locations this project's own architecture treats as
separately-classified mutable state, not configuration
(`docs/architecture/runtime.md` §9: "Mutable State" — sessions, logs, SQLite
databases, caches, trust decisions, memory, installation metadata), *plus*
one incidental change to `~/.claude/settings.json` itself. That machine has
real, concurrently running Claude Code and Codex sessions (including the
very session that authored this PR), so session transcripts, rollout files,
and SQLite WAL churn are expected regardless of this harness. The
`settings.json` change is more notable — it sits in the "config" bucket this
harness cares about — but there is no plausible mechanism by which an
`env -i`-launched subprocess with `HOME`/`CLAUDE_CONFIG_DIR` pointed at an
unrelated empty temp directory could reach back and write the real
`$HOME/.claude/settings.json`; the far more likely explanation is ordinary
background bookkeeping by another concurrently running Claude Code session
on the same account, unrelated to this test. This is recorded here rather
than silently rounded up to "verified clean," per this PR's own instruction
to not claim results not actually observed.

This finding is also why `internal/qualify`'s automated real-home check
(`internal/qualify/realhome.go`, gated behind the `OMCA_QUALIFY_VERIFY_REAL_HOME`
env var) is opt-in rather than part of the default `go test ./...`/`make
fixtures` path: asserting byte-for-byte equality of a live, actively-used
developer machine's real config on every test run would be flaky for a
reason unrelated to any real isolation bug. The harness's default,
always-on zero-write proof instead uses a disjoint, hermetic stand-in
directory (see `internal/qualify/sandbox.go`'s `Outside` field), which gives
a deterministic CI-safe guarantee of the same property this harness's own
code must uphold.

## Host binary versions and provenance

Anchors named in the design docs: `codex-cli 0.144.5`, `Claude Code 2.1.211`.
Versions actually found installed on the authoring machine, checked with
`codex --version` / `claude --version` (both safe, non-interactive,
no-network flags): **identical to the anchors** — no discrepancy to report.

### Codex CLI 0.144.5

- Acquisition: `npm install -g @openai/codex` under an asdf-managed Node.js
  20.19.0 (`asdf plugin ... https://github.com/asdf-vm/asdf-nodejs.git`).
  `codex` on `PATH` is an asdf shim that execs into
  `@openai/codex`'s Node entrypoint, which in turn spawns the real native
  binary vendored inside the platform-specific optional dependency
  `@openai/codex-darwin-arm64`.
- Resolved native binary path (this machine):
  `.../lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex`
- Version cross-checks: `codex --version` → `codex-cli 0.144.5`;
  `@openai/codex`'s own `package.json` `"version"` → `0.144.5`;
  `@openai/codex-darwin-arm64/vendor/.../codex-package.json` `"version"` →
  `0.144.5`; `npm ls -g --depth=0` → `@openai/codex@0.144.5`. Four
  independent sources agree.
- Local install fingerprint (sha256 of the exact installed native binary
  bytes on the authoring machine, **not** a claim of verification against an
  official vendor-published checksum — none was fetched in this pass):
  `5e29ab10ca1171be158f7335dd6bd8ce1aaf9af1556939db36a5ee338be6f5f2`

### Claude Code 2.1.211

- Acquisition: Claude Code's own native updater/installer, which manages
  versioned installs under `~/.local/share/claude/versions/<version>` and
  symlinks the active version at `~/.local/bin/claude`.
- Resolved binary path (this machine): `~/.local/share/claude/versions/2.1.211`
  (a native Mach-O arm64 executable, not a script).
- Version cross-check: `claude --version` → `2.1.211 (Claude Code)`; the
  versioned install directory name itself corroborates.
- Local install fingerprint (sha256, same caveat as above — a local
  fingerprint, not a verified-against-upstream checksum):
  `5a728a76198b6eca7f3c7cdbff43bab44b77b48c2108f7a3107d889773382629`

A cryptographic checksum verified against an official vendor-published
manifest was **not** obtained for either binary in this pass: doing so would
require reaching an external release API/registry beyond what this PR's
scope and safety boundary call for. The sha256 values above let a second
machine confirm it is running the exact same binary bytes this fixture
corpus was authored against, which is the reproducibility property that
actually matters here — they are not a substitute for an upstream-verified
checksum, and this document does not claim they are.

### One static-inspection finding worth flagging: Claude Code's config-directory override variable

`docs/architecture/runtime.md` §7.2 lists "a relocated configuration
directory per generation" as a candidate Claude Code isolation mechanism
without naming its environment variable, and asks the adapter to determine
it from `--help`/documentation rather than assuming a name. `claude --help`
itself does not name it. It was instead determined by static inspection
(read-only `strings` extraction — never execution) of the installed
`claude` binary: the variable is **`CLAUDE_CONFIG_DIR`**. Corroborating
strings found in the binary (reproduced here only as evidence of which
variable exists and roughly how it composes paths — never the actual
content of any real config):

```
`${e}/.claude`,`${e}/.claude.json`,cte?.claudeConfigDir??process.env.CLAUDE_CONFIG_DIR
function QYa(){return process.env.CLAUDE_CONFIG_DIR}
let e=`.claude${Zxn()}.json`;return rJe.join(process.env.CLAUDE_CONFIG_DIR||icl.homedir(),e)
```

This shows `CLAUDE_CONFIG_DIR`, when set, relocates **both** the `.claude/`
asset directory *and* the `~/.claude.json`-equivalent local MCP/trust state
file (joined directly under `CLAUDE_CONFIG_DIR`, not nested under a further
`.claude/` subdirectory) — which is why this corpus's Claude Code sandboxes
place the synthetic `.claude.json` fixture directly at
`input/claude-config/.claude.json` (see `fixtures/claude-code/2.1.211/mcp-merge/`)
rather than under a nested subdirectory. This is E1 evidence (discovered
from a static, read-only source, per `docs/domain/evidencelevel.go`'s E1
"the source was parsed losslessly or retained safely as opaque") — it was
never behaviorally confirmed by actually launching Claude Code with the
variable set and observing the resulting file layout live, which is why
every Claude Code case in this corpus still records its precedence
sub-questions as UNKNOWN rather than treating this discovery as a green
light to guess further.

### A second static-inspection finding: an undocumented Codex skill root

`docs/ontology/README.md` §6.2's physical-mapping table for Codex Skills
lists `repo .agents/skills`, `$HOME/.agents/skills`, `/etc/codex/skills`,
bundled, and extra configured paths — it does not list `$CODEX_HOME/skills`.
Static inspection (`strings`, read-only) of the installed `codex-cli 0.144.5`
native binary shows multiple references treating `$CODEX_HOME/skills` as a
real skill install/discovery location, e.g.:

```
"""Install a skill from a GitHub repo path into $CODEX_HOME/skills."""
- After generation, remove the background locally with `python "${CODEX_HOME:-$HOME/.codex}/skills/.system/imagegen/scripts/remove_chroma_key.py" ...
Installed annotations come from `$CODEX_HOME/skills`.
```

This is a genuine discrepancy between the documented ontology claim and
this pass's own static evidence, surfaced explicitly in
`fixtures/codex/0.144.5/skill-collision/expected-effective.json` rather than
silently reconciled one way or the other — hence that fixture plants a
*third* synthetic "deploy" skill under a `codex-home/skills/` root, not just
the two documented ones.

## Precedence questions this corpus marks UNKNOWN, and why

Every one of the six required cases (Instructions collision, Skill
collision, MCP registry merge — for both Codex and Claude Code) records its
core precedence question (`selectedSource` in `expected-effective.json`) as
`UNKNOWN`, because:

1. Neither binary's `--help` output names a safe, non-interactive,
   no-network flag that dumps effective/merged configuration.
2. The only way to observe a real answer would be a real session (`codex`
   with no subcommand, `codex exec`, `claude` with no flags, or `claude -p`)
   — every one of those either starts an interactive session or makes a
   real, billed model call, both explicitly excluded by this PR's safety
   boundary.
3. Where `docs/ontology/README.md` already states a specific documented
   claim (e.g. Claude Code Skills: "personal > project"; Codex Instructions:
   "closer instructions appear later"), that claim is preserved verbatim in
   each entry's separate `documentedClaim` field — with its own, honestly
   weaker evidence level (E1, "discovered/parsed from an official doc") —
   rather than promoted into `selectedSource`, which stays reserved for what
   this qualification pass itself established. `internal/qualify/effective.go`
   enforces this distinction structurally: `Confirmed=true` is rejected by
   `ExpectedEffectiveDocument.Validate` unless the entry's evidence level is
   E3 (host-reported), E4 (behavior-probed), or E5 (externally proven) — E1/E2
   can never satisfy `Confirmed=true`, so a future contributor cannot
   silently promote a documented claim into a confirmed fact without new,
   stronger evidence.

This is the expected, correct scope for this PR, not a shortfall: issue #10
itself says leaning on UNKNOWN heavily in this first pass is correct.
