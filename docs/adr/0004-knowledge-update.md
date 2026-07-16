# ADR 0004: Knowledge Pack Update and Supersede Flow

Status: accepted

## Context

Adapter code implements parsing and compilation, but the actual facts about a
host's version, discovery roots, precedence, and capability level change
frequently as vendors ship releases; those facts live in a separate,
versioned Knowledge Pack rather than being embedded as floating claims in
adapter code (`docs/knowledge/README.md` §1). Every generation records which
Knowledge Pack it resolved against (`docs/architecture/runtime.md` §5.3
"Knowledge Pack IDs and digests"), so historical generations must remain
explainable even after Knowledge advances (`knowledge/README.md` §2). This
requires Packs to behave like immutable, append-only evidence rather than a
mutable "current facts" document, and requires a firm answer for what happens
when an installed host version falls outside every Pack OMCA has qualified —
both questions are already answered descriptively in `knowledge/README.md`
§1, §3, §7, §11 but not yet frozen as an accepted decision this ADR now
provides.

## Decision

1. **Packs are immutable after publication.** A Knowledge Pack, once published
   at `knowledge/hosts/<host>/<surface>/<version-or-range>/`, is never edited
   in place (`knowledge/README.md` §1, §3). Its manifest, capabilities,
   discovery, precedence, and evidence files are fixed for that Pack ID.

2. **Corrections publish a new Pack and mark the old one superseded.** A
   correction, expanded evidence, or a new host release never rewrites an
   existing Pack's content. It publishes a new Pack (new ID/version range) and
   transitions the old Pack's lifecycle state to `SUPERSEDED`
   (`knowledge/README.md` §7 lifecycle table). Historical generations keep
   referencing the superseded Pack's exact digest, so past behavior remains
   reproducible and explainable (`knowledge/README.md` §2, §11: "every
   generation stores the Pack itself or a durable content-addressed copy").

3. **An installed host version outside every qualified Pack's range fails
   closed to observation-only.** At launch, OMCA resolves detected host
   binary + exact version + surface + platform + invocation context to at
   most one immutable Knowledge Pack ID and digest (`knowledge/README.md`
   §11). If no published Pack's version range covers the installed version:

   - every operation for that host/version is treated as `UNKNOWN` and
     degrades to `observed`/`OBSERVED` reconcile mode (ADR 0002; capability
     vocabulary in `knowledge/README.md` §5);
   - **no older or adjacent Pack is applied optimistically.** A more
     permissive Pack for a nearby version range is never extrapolated onto an
     unqualified version, even if the versions look similar
     (`knowledge/README.md` §11: "A more permissive older Pack is never
     applied optimistically to a new version");
   - a floating `latest` selector may be used to *discover* a candidate Pack
     but can never be recorded as the Knowledge dependency of an actual
     generation (`knowledge/README.md` §7).

   This is a fail-closed rule, not a best-effort fallback: an unqualified host
   version gets less capability (observation only), never silently-assumed
   capability borrowed from whatever Pack happens to be adjacent.

4. **Updates flow through reviewed pull requests, never silent runtime
   updates.** The update workflow is: poll allowlisted official sources,
   detect a change, create a Knowledge Candidate, diff facts and affected
   capabilities, run qualification fixtures, open a PR, require maintainer
   review, then publish the immutable Pack (`knowledge/README.md` §8).
   Automation may create the candidate and PR; it may not merge the PR or
   promote a capability level without maintainer review (`knowledge/README.md`
   §8, §12).

5. **Two-tier governance applies to which hosts get this treatment
   proactively.** Tier 1 (Claude Code, Codex): maintainers keep Knowledge
   fresh and fixtures green as an ongoing obligation. Tier 2 (every other
   host): Packs may go `DUE` or `STALE` without blocking a release; promoting
   a Tier 2 capability requires an adapter plugin with fixtures
   (`knowledge/README.md` §12).

## Alternatives Considered

- **Edit Packs in place when a fact turns out to be wrong.** Rejected: this
  would silently change what a historical generation's recorded Pack digest
  means, breaking reproducibility ("same inputs plus the same Knowledge digest
  produce the same generation digest," init.md invariant) and making past
  debugging depend on a repository state that no longer exists
  (`knowledge/README.md` §11).
- **Optimistically extrapolate the nearest older Pack onto an unqualified new
  host version (e.g., semver-nearest fallback).** Rejected: an untested
  assumption of compatibility is exactly the risk Knowledge Packs exist to
  remove evidence-free guessing from; `knowledge/README.md` §11 states this
  explicitly as prohibited, and init.md's invariant "unknown behavior cannot
  be promoted to managed by an LLM" generalizes to: unknown behavior cannot be
  promoted to managed by an unqualified guess of any kind.
- **Let automation auto-merge low-risk Knowledge Candidates.** Rejected:
  `knowledge/README.md` §8 and §12 require maintainer review for every publish
  and every capability promotion; "low risk" is itself a judgment a
  deterministic pipeline should not make unsupervised, matching init.md
  decision 15 ("Third-party host knowledge changes only through repository
  pull requests approved by maintainers").
- **A single floating `latest` Pack pointer per host instead of versioned,
  immutable ranges.** Rejected: a floating pointer cannot be the recorded
  Knowledge dependency of a reproducible generation and would make "what did
  OMCA believe about this host when it built generation X" unanswerable
  (`knowledge/README.md` §7, §11).
- **Fail open (assume best-known behavior) for an unqualified host version
  instead of failing closed to observation-only.** Rejected: failing open
  would let OMCA write configuration based on unproven assumptions about a
  host it has never qualified, directly contradicting the observation-first
  posture in init.md's Product Statement ("Observe -> Model -> Reconcile") and
  the invariant that unknown behavior cannot be promoted to managed.

## Consequences

- Every generation must store either the Knowledge Pack itself or a durable
  content-addressed copy, independent of the current repository HEAD
  (`knowledge/README.md` §11), so historical explanation never depends on a
  Pack still existing at its original path.
- The Knowledge repository grows monotonically (new Packs supersede old ones
  rather than replacing their content), which is an accepted storage
  trade-off in exchange for reproducibility; `RETIRED` status
  (`knowledge/README.md` §7) is the eventual, explicit, documented mechanism
  for pruning history, not silent deletion.
- Adapters must implement the "no matching Pack -> observation-only, no
  optimistic extrapolation" rule as a hard branch, not a warning, since it is
  load-bearing for the M0 exit gate ("unknown precedence returns UNKNOWN,"
  roadmap).
- Knowledge Candidate reports must show old/new version ranges, affected
  capabilities, and which generations would become stale
  (`knowledge/README.md` §9), giving maintainers what they need to review a
  publish or a supersession responsibly.
- Tier 2 hosts can accumulate `DUE`/`STALE` Packs without blocking releases,
  but any capability promotion for them still requires the same fixture-backed
  review as Tier 1 — the two-tier split changes urgency, not rigor.
