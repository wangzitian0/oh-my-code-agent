# ADR 0003: Credential Handling

Status: accepted

## Context

Authentication and credential state is not ordinary configuration: it is
identity-shared, high-sensitivity, and often bound to a native, untrusted home
directory (`docs/architecture/runtime.md` §8). Isolating a runtime by giving
it a fresh virtual `HOME`/`CODEX_HOME` (`runtime.md` §7.1) must not silently
imply "log in again from scratch" for every generation, nor may it imply
"copy the native token cache in so it keeps working" — the first breaks the
product's usability, the second breaks the isolation boundary and could import
untrusted native state (init.md decision 14; `runtime.md` §2 threat model).
Init.md's invariants are explicit: "secrets do not enter reports, plans,
manifests, or model context" and native global configuration is never an
implicit parent. This ADR freezes the rule that resolves the tension for every
adapter, and separately addresses Claude Code's specific account/OAuth sharing
question raised in `runtime.md` §7.2.

## Decision

1. **Auth/credential state is never copied into a generation.** No adapter may
   write a native `auth.json`, OAuth token, API key file, keyring export, SSH
   key, or cloud-credential file into a generation's artifact tree. Generation
   manifests may store a *reference* to where a credential lives (e.g., "OS
   keyring, service X, account Y") but never the secret material itself
   (init.md invariant; `runtime.md` §12 "credentials are references or
   isolated state, not generated config").

2. **Fallback order.** When a managed runtime needs a credential, adapters
   resolve it in this fixed order, stopping at the first source that
   satisfies the need:

   ```text
   OS credential store
     -> direnv-provided secret reference
     -> identity-specific runtime login
     -> explicit, reviewed migration
   ```

   This is the same order `runtime.md` §8 already specifies; this ADR freezes
   it as the accepted, non-negotiable order for every adapter rather than
   guidance that could vary per host:

   - *OS credential store*: the platform keyring/credential manager, queried
     per isolated home when the platform and mechanism have been qualified
     (see consequence below on the M5 qualification gate).
   - *direnv-provided secret reference*: an API key or secret reference
     exported into the environment by the worktree's `.envrc`, never written
     to disk by OMCA.
   - *identity-specific runtime login*: the host's own native login flow, run
     once per isolated identity/generation family, producing state classified
     under ADR 0002 as `external` (owned by the host's own credential
     mechanism, not by OMCA).
   - *explicit, reviewed migration*: a human-approved, one-time, logged import
     of a specific credential when none of the above apply. This is a
     deliberate exception path, not a default, and must be reviewable
     (mirrors init.md decision 13's advisory-not-automatic posture and the
     non-goal against building a secret manager).

3. **Prohibited regardless of order.** Automatic copying or broad symlinking
   of native `auth.json`, token caches, keyrings, `.ssh`, or cloud credential
   directories into an isolated home is prohibited outright (`runtime.md` §8).
   A narrow, fixture-backed symlink allowlist (ADR 0002 `passthrough`/
   `external` classification, `runtime.md` §9) is the only sanctioned
   exception, and it must never amount to a broad native-home symlink that
   defeats isolation.

4. **Claude Code account/OAuth state.** Claude Code shares account and OAuth
   state, project trust decisions, and parts of the MCP registry through one
   mutable native user state file (`runtime.md` §7.2). This ADR fixes two
   constraints regardless of which isolation mechanism the adapter proves:

   - Account and OAuth state is identity-shared credential state. It is never
     copied into a generation, and isolation must not force a fresh login for
     every generation.
   - If the native state file cannot be shared safely across isolated homes
     (i.e., sharing it would leak other native-global state alongside the
     credential), the identity gets an explicit login flow instead — the
     adapter degrades to the third rung of the fallback order rather than
     forcing an unsafe share or a broken login loop.

   Which safe-sharing mechanism the adapter uses (a narrowly scoped
   credential-only extract, an OS-keyring-backed token, or a proven partial
   share) remains an open qualification question tracked by the Claude Code
   adapter's own fixtures (`runtime.md` §7.2); this ADR does not pick that
   mechanism, it only fixes the two constraints above as non-negotiable
   regardless of which mechanism qualifies.

## Alternatives Considered

- **Copy native credentials into the generation for convenience.** Rejected:
  directly contradicts the invariant that secrets never enter generated
  config, and would make generation manifests — which are content-addressed
  and potentially shared for debugging (`runtime.md` §5.3, §11) — a secret
  leak surface.
- **Symlink the native credential store/home broadly into every isolated
  home.** Rejected: `runtime.md` §8 prohibits this explicitly; a broad symlink
  reintroduces exactly the untrusted-native-parent problem isolation exists to
  remove, and is indistinguishable in risk from not isolating at all for
  anything reachable through that symlink.
- **Require a fresh interactive login for every new generation.** Rejected:
  `runtime.md` §7.2 states isolation "must not force a fresh login for every
  generation" — generations are created often (any desired-state change,
  `runtime.md` §5.3), so per-generation login would make the product
  unusable and would push users toward disabling isolation.
- **Build an OMCA-owned secret manager/vault.** Rejected: explicit non-goal in
  init.md ("Build a hosted SaaS, secret manager, marketplace, or background
  fleet manager in v1"). OMCA references and orchestrates existing credential
  sources; it does not become one.
- **Treat OS-keyring reuse across isolated homes as always safe by default.**
  Rejected: `runtime.md` §8 requires this to be qualified per platform before
  the first Codex managed milestone; assuming safety without qualification
  risks silently leaking a credential across identity boundaries the isolation
  model is supposed to respect.

## Consequences

- Every adapter's credential resolution code must implement the four-rung
  fallback order verbatim; a new host adapter that skips a rung (e.g., goes
  straight to explicit migration) is non-conformant with this ADR.
- Generation manifests, reports, and any MCP-exposed data must be reviewed to
  guarantee they carry only credential *references*, never material — this is
  testable via the existing secret-redaction fixture requirement
  (`docs/knowledge/README.md` §10, item 10: "secret redaction and proof that
  observation did not execute content").
- M5's deliverable to "qualify OS-keyring or identity-specific authentication
  for isolated homes" (roadmap) is the gate that determines, per platform,
  whether rung one (OS credential store) is actually available; until
  qualified for a given platform, adapters fall through to rung three
  (identity-specific runtime login) for that platform.
- The Claude Code adapter's fixtures must demonstrate, before Claude Code
  launches become `managed` rather than `observed`, that its chosen mechanism
  satisfies both fixed constraints in Decision item 4 — this is an explicit
  qualification checklist item, not an assumption.
- Any future explicit reviewed migration path must be logged and attributable,
  since it is the one rung that involves a human-approved exception rather
  than a mechanical resolution.
