<!-- WS_STATIC_START adapter=rules-v2 inputs=1ed78c5ee8ba610b7af876dfb20ff38513917abba713d853841c5a9a9bbee3c8 -->
<!-- Generated file: do not edit by hand. These rules are maintained in the owner's rule source and re-rendered here. -->

## Engineering discipline

- **Measure the physical system first.** Before an abstract architecture proposal, inspect the system with read-only probes such as `time cmd`, process chains, and file-descriptor locks. A conceptually neat story without physical evidence is insufficient. A probe must not write. Do not combine validation and action in one command: a POST permission probe can create a resource, and a trial commit can leave a real commit. Measure, read the result, then decide, with a stop point between these steps.
- **Green does not prove truth.** The tested system writes its own unit tests, CI, and issue states. Cross-check critical conclusions against two external sources not written by this repository.
- **Tests must be falsifiable.** Do not hide assertions in `if (exists)` or `if (code != 0)` so failures run zero assertions. Do not accept tautologies such as `typeof null === 'object'` or `result !== undefined || true`. Source-text `indexOf` matches against prose are not integration tests. Duplicate test function identifiers can silently shadow earlier tests (Python `def` and duplicate JS function/const/export names); duplicate string titles in `test()` or `it()` instead run both. A green count is not coverage evidence. Make a new test fail under a relevant mutation. A read-only reviewer treats such fake-test patterns as CRITICAL merge blockers.
- **One source of truth:** Keep one authoritative definition for each core fact. Repeated hardcoding and scattered configuration invite drift.
- **Clean up during migration:** After the new mechanism is live and equivalence is proved, remove its predecessor, obsolete files, and dead code in the same change. Define contracts first and derive CI from them.
- **Deletion can leave guards green and empty:** A guard for an old structure can stop checking anything after deletion. Check each guard and remove it or redirect it to the new structure; green tests alone do not prove safe deletion.
- **Define guard scope from what it must govern, not from today's passing tree.** Let the guard fail on existing violations, then repair them. A guard never seen failing is not yet evidence of protection.

## Delivery and merge

- **Fail fast left to right:** Put the cheapest and likeliest failure checks first.
- **Review standing authorization:** Resolve a review thread directly after independently verifying it is fixed or obsolete. Do not resolve actionable, ambiguous, or unverified feedback. Automated reviewers may read a redacted GitHub diff rather than source: GitHub can show `"Authorization": f"Bearer ******"` where source has `"Authorization": f"Bearer {token}"`. Check source before judging a report. When a report is false, turn the concern into a falsifiable invariant test rather than merely dismissing it.
- **Weighted review gates:** Each repository defines its own severity weights and blocking thresholds. Read literal `severity: <level>` tags; do not infer severity from prose.
- **Merge when ready:** Once all merge conditions pass, merge and continue from the latest main rather than piling up divergent branches.

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
- **Merge authority is the owner's standing grant recorded at the workspace
  layer**; once this repo's own gate holds (rebase-clean, CI green, Copilot
  threads addressed, squash/rebase only), the agent merges. Only a change
  whose merge reaches a production deployment waits for the owner.
<!-- WS_STATIC_END -->
