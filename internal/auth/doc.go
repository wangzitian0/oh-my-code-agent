// Package auth implements ADR 0003's credential fallback DECISION logic and
// docs/architecture/runtime.md §9's mutable-state classification for the
// first-party host adapters (issue #27, PR-23). It never performs a real
// interactive login and never exercises the real OS credential store — both
// are scoped out of this PR by its own round-3 pre-dispatch safety audit
// (see LoginQualificationIssueURL's doc comment) and deferred to a one-time,
// human-performed manual qualification step.
//
// # What this package proves
//
//   - Decide (fallback.go) implements ADR 0003 decision item 2's four-rung
//     fallback order (OS credential store -> direnv-provided secret
//     reference -> identity-specific runtime login -> explicit, reviewed
//     migration) as real, tested branching logic: given an Environment and a
//     KeyringProbe, it picks the first rung that is actually satisfied and
//     explains why. Only rungs 1-3 are ever chosen automatically; rung 4
//     (ProposeMigration) is a human-invoked, logged exception path, never a
//     default Decide can reach on its own (ADR 0003: "a deliberate exception
//     path, not a default").
//   - InvocationPlan/Invoke (invoke.go) build and can execute the exact
//     native login command rung 3 would run (e.g. `codex login`,
//     `claude auth login` — verified against real `codex --help`/
//     `claude auth --help` output, never by running either command for
//     real). Every automated test in this package invokes only a small,
//     hermetic fake binary written by the test itself (mirroring
//     internal/qualify's own fake-binary fixture precedent, e.g.
//     TestRunInvocationRunsIsolatedFakeBinary), on a PATH the test
//     constructs from scratch — never the real installed codex/claude CLI
//     this repository's own CI or developer machine may have. Nothing in
//     this package's production code path calls Invoke against a real host
//     binary; no cmd/omca command wires this rung to a live CLI surface
//     yet, precisely so a real login can never be triggered by an
//     automated run of this code.
//   - mutablestate.go classifies every mutable state class docs/
//     architecture/runtime.md §9 names (sessions, SQLite, logs, caches,
//     trust, memory, installation metadata) for both first-party hosts,
//     grounded in a read-only directory-structure listing of the
//     maintainer's real ~/.codex and ~/.claude (filenames and directory
//     names only — no file content was ever read, matching fixtures/
//     README.md's own static-inspection evidentiary standard for a finding
//     that has not been behaviorally qualified).
//   - allowlist.go implements docs/architecture/runtime.md §9's "sharing
//     state through symlinks is allowed only for an explicit allowlist
//     backed by fixtures. A broad symlink to the native host home defeats
//     isolation": the only way any native-home path can ever be symlinked
//     into a generation is by appearing, byte-for-byte, as an entry in the
//     closed Allowlist slice — there is no code path that accepts an
//     arbitrary caller-supplied path to link instead, and
//     TestPlanAllowlistedSymlinks_RejectsBroadNativeHomeLink /
//     TestValidateAllowlist_RejectsBroadEntry prove a broad (empty, ".", or
//     "..").-shaped entry is refused before it ever reaches the filesystem.
//
// # OS-keyring qualification is deferred, not implemented
//
// KeyringProbe (keyring.go) is a mockable interface only. This package
// deliberately does not ship a real macOS Keychain (or any other platform)
// implementation: the maintainer's own safety scoping for this PR requires
// any real OS-credential-store probe to be a genuinely non-destructive,
// read-only existence check the maintainer can be present for if it
// triggers an OS permission prompt, and an autonomous implementation cannot
// self-certify that condition for an unattended `go test` run. Rung 1 is
// therefore hardcoded unqualified for every platform (KeyringQualification
// always returns Qualified: false) until a human performs that qualification
// out-of-band — see LoginQualificationIssueURL. The decision-logic BRANCH
// for "rung 1 satisfied" is still real and tested (TestDecide_Rung1...), just
// exercised through a mock KeyringProbe a test constructs, never the real
// store.
package auth
