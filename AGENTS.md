# AGENTS.md

Operational guidance for AI agents working in this repository. This file
complements, and never duplicates, the design authorities documented in
[`init.md`](init.md) (product charter, approved decisions, invariants) and
[`docs/README.md`](docs/README.md) (documentation map and reading order) —
read those first. This file is for practical, hard-won working conventions
that don't belong in either.

## Testing against real host CLIs (codex, claude)

This project's whole point is to observe and isolate real coding-agent
hosts, so fixture/synthetic testing eventually needs a real-machine check.
When it does:

- Snapshot the real native home locations *before* testing (e.g. `find
  ~/.codex ~/.claude -maxdepth 2 -exec md5 {} \; > before.txt`), then diff
  after. Do this before starting, not reconstructed after the fact from
  mtimes — mtime-based inference conflates unrelated concurrent activity
  (another real session on the same machine) with anything the test itself
  might have touched, and can't actually distinguish the two.
- Never start a real interactive or model-calling session. Stick to
  `--version`, `--help`, `doctor` / `doctor --json`, `login status`, and
  other documented, non-interactive introspection commands.
- Always drive the real binary through omca's own isolation (the PATH shim
  or `omca run`), never by hand-setting HOME/CODEX_HOME/CLAUDE_CONFIG_DIR to
  the real native locations directly.
- Default to recording UNKNOWN rather than guessing when a safe,
  non-interactive proof isn't available for some claim.

## Scope discipline: what's worth chasing

HOME virtualization (`docs/architecture/runtime.md` §7.1) is a blunt
mechanism — it closes the specific `$HOME/.agents/skills`-style native
config leak the isolation invariant exists to prevent, but as a side effect
it also breaks anything else on the machine that happens to read `$HOME`
for unrelated reasons: asdf's own shim dispatch, a host's own
install-integrity self-check, a host spawning another asdf-managed tool as
a subprocess, and so on. That tail is long and will keep producing new,
superficially plausible bugs to chase.

Before spending more than a quick look on one of these, ask: **does it
affect whether Instructions, Skills, MCP servers, Hooks, or Permissions are
correctly observed, included/excluded, or activated** (the MVP Concepts
list in `init.md`)? If yes, it's in scope. If no — some host's own
unrelated internal assumption about `$HOME` broke, but the actual
harness-concept management is unaffected — note it (a code comment, a
tracking issue) and stop there. Chasing host-runtime compatibility issues
that never touch the actual concept surface this project manages is
explicitly not a goal here (see `init.md`'s Non-goals: this project does
not promise to cover every host concept or translate every executable
integration automatically).

## Known gotchas

- **`gh` commands inside a `Monitor`/background script must pass `-R
  wangzitian0/oh-my-code-agent` explicitly.** The session's Bash cwd resets
  to the parent `infra2-harness` monorepo after tool calls (submodule
  setup); a bare `gh pr checks <n>` silently resolves against
  infra2-harness's own unrelated CI checks instead of this repo's. (`gh
  api` takes the repo in the endpoint path itself, not a `-R` flag — that's
  only for `gh pr`/`gh issue` subcommands.)
- **Any background agent dispatch that runs `git checkout`/`git branch`/
  writes files in a repo the main session also has a live checkout of must
  use worktree isolation.** Without it, a background agent can silently
  switch the shared working directory to its own branch mid-session,
  colliding with whatever the foreground session is doing there.
- **This repo only allows squash/rebase merges** (`allow_merge_commit:
  false`). After any rebase-merge, run `git fetch && git rebase
  origin/main` (not `merge`) on every downstream branch before re-checking
  mergeability — a stacked branch otherwise shows CONFLICTING/DIRTY against
  the new main even though its content is identical to what was already
  merged.
- **Merging is human-only by default.** A standing "merge without asking if
  there are no conflicts, CI is green, and Copilot comments are addressed"
  authorization has been granted before, but only for the specific session
  it was granted in — it does not carry forward to a new session. Ask
  again, or wait for the repo owner to merge, unless explicitly
  re-authorized in the current session.
