<!-- WS_STATIC_START adapter=rules-v2 inputs=6384b908ecdc3120968b92bb184e472fdfe6a51b4bc85284faaa31498f3d86d7 -->
<!-- Generated file: do not edit by hand. These rules are maintained in the owner's rule source and re-rendered here. -->

## Engineering discipline

- **Measure the physical system first.** Inspect the system with read-only probes (`time cmd`, process chains, file locks) before proposing architecture. Never combine validation and mutation in one command. Measure, read the result, then decide.
- **Green does not prove truth.** The tested system writes its own unit tests, CI, and issue states. Cross-check critical conclusions against two external sources not written by this repository.
- **Tests must be falsifiable.** Never hide assertions inside `if` branches or accept tautologies. Source-text `indexOf` matches against prose are not integration tests. Avoid duplicate test function names that shadow earlier tests. Make every new test fail under a relevant mutation; fake tests are CRITICAL merge blockers.
- **Falsifiable documentation and assertions:** State test requirements, error messages, and specifications in concrete ASD-STE100 terms without vague qualifiers.
- **One source of truth:** Keep one authoritative definition per core fact. Repeated hardcoding and scattered configuration invite drift.
- **Clean up during migration:** After the new mechanism is live and equivalence is proved, remove its predecessor, obsolete files, and dead code in the same change. Define contracts first and derive CI from them.
- **Deletion can leave guards green and empty:** A guard for an old structure can stop checking anything after deletion. Check each guard and remove it or redirect it to the new structure; green tests alone do not prove safe deletion.
- **Fix an owner hold by deletion:** Delete the rule or gate branch that holds non-production work. Do not add logic that reads a command to permit it. Such logic lets unsafe commands through.
- **Define guard scope from what it must govern, not from today's passing tree.** Let the guard fail on existing violations, then repair them. A guard never seen failing is not yet evidence of protection.
- **Worktree self-sufficiency:** Every worktree must resolve its dependencies, toolchain, skills, and configuration internally. Tools and tests must not navigate upward with `../..` to locate files in parent checkouts.
- **Physical context verification:** Static file existence does not prove host runtime context injection. Run non-interactive probes (`-p` or headless exec) with low-cost models to verify visible specification text across root and deep subdirectories.
- **Host verification scope:** Official host CLIs are the primary execution environment. Regular testing covers Antigravity 2.0 project workspaces (`.agents/skills/` and project rules); standalone desktop chat apps without directory awareness are excluded.
- **Local and remote test segregation:** Inner-loop verification runs only focused tests on changed files (`pytest <file>::<test> -x`). Full-suite regression runs on remote CI, or locally at most once before merge.
- **Bounded file reads:** Pass `StartLine` and `EndLine` (`<= 800` lines per call) when reading existing files with `read_file` or `view_file`.

## Delivery and merge

- **ASD-STE100 specification for all artifacts:** Write committed artifacts, rules, code, comments, commits, and docs in ASD-STE100 English. Keep instructions `<= 20` words and descriptions `<= 25` words in active voice without vague qualifiers. Operational skills (`skills/**/SKILL.md`) follow the target repository's language policy.
- **Deliverable format:** Deliver a mergeable PR or a traceable issue, rather than a process-only report. Use issues and PRs as the collaboration bus. Search for an existing similar issue before creating one; update it if it exists.
- **Fail fast left to right:** Put the cheapest and likeliest failure checks first.
- **Review standing authorization:** Resolve a review thread after verifying in source code (not redacted diffs) that it is fixed or obsolete. Never resolve actionable or unverified feedback. Turn false reports into falsifiable invariant tests.
- **Weighted review gates:** Each repository defines its own severity weights and blocking thresholds. Read literal `severity: <level>` tags; do not infer severity from prose.
- **Merge when ready:** Once all merge conditions pass, merge and continue from the latest main rather than piling up divergent branches.
- **Conflict-free merge window:** Merge conflict-free PRs (`mergeable=MERGEABLE`) within 24 hours of `main` without rebasing; post-merge CI validates `main`. Rebase on conflicts or after 24 hours.
- **Delivery state invariant:** Never declare complete, done, or finished before merge to main and a verified Business Reality Probe (`ws-delivery-status`). State labels are `'Draft'`, `'Local verified'`, `'In review'`, `'Stage 1 Landed'`, and `'VERIFIED_COMPLETE'`.
- **Anti-exclusion and reality invariant:** Exclusion is not resolution. Never exclude failing entities or add exemptions to manufacture green checks. A task is complete only when a physical reality probe confirms non-empty execution.
- **Three-stage deployment prerequisite:** Stage 1 (Merge to main) is a non-bypassable physical prerequisite for Stage 2 and Stage 3. If HEAD is not on main, fail closed and refuse any deployment request.
- **Deploy provenance invariant:** Before dispatching any deploy workflow, physically verify that the target ref or tag contains current branch commits (`git merge-base --is-ancestor HEAD <target>`). Never deploy an older tag as completion evidence for current work.

## Production reservation

The single definition of the production reservation. Other tiers, skills and gates point here.

- **Only a production change returns to the owner.** A production change alters what production runs or stores. Examples: a deploy, promote or apply to production; a production secret write; a manual production data write or deletion. Also a production DNS, route or host change, and a release that cannot be recalled. Code that reaches production with no later owner approval, for example on a schedule, counts too. Each Repo tier names its production paths.
- **Approval:** the owner approves the exact commit SHA or release tag of each production change, and is present when it runs. A "deploy" reply covers only the release that it names. Immediately before a production dispatch, get a live reply from the owner.
- **Self-guard:** an edit that removes, narrows or redefines the production reservation needs the owner's approval of its head SHA. This includes an edit that makes fewer production runs wait for the owner. Examples: the owner-approval check of a deploy path, a production job classification, the settings of a production environment. It also includes an edit that removes or weakens a pin of this section or its dev_env pin test. The same holds for the required check that runs the test.
- **Everything else is the agent's:** in a repository that the owner owns, every merge, staging and canary deploy, and release tag. Also rule text, skills, gate code, data and tests, required-check workflows and release code. Before you merge gate or release code, a read-only reviewer that did not write it checks the head. The review states whether the gate gets stricter, stays equal or gets looser. A looser edit in the self-guard list follows the self-guard.
- **Every other hold is a defect:** a rule, skill, label or gate exit code that holds non-production work for the owner. Do the work, then fix the hold. The dev_env check `ws_owner_scope` fails on it.

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
  false`). Independent conflict-free (`MERGEABLE`) PRs within 24 hours of
  `main` merge without rebasing. On a **stacked** downstream branch after its
  upstream PR merges, run `git fetch && git rebase origin/main` (not `merge`)
  so the stacked branch does not show `CONFLICTING`/`DIRTY` against `main`.
- **Merge authority is the owner's standing grant recorded at the workspace
  layer**; once this repo's own gate holds (`MERGEABLE` within 24 hours of
  `main`, CI green, Copilot threads addressed, squash/rebase only), the agent
  merges. A merge that reaches a production deployment is a production change (Root section `Production reservation`).
<!-- WS_STATIC_END -->
