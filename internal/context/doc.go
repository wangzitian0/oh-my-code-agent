// Package context resolves worktree, repository, identity, and invocation
// context (docs/architecture/README.md §4, "Context Detector: Resolve
// worktree, repository, identities, host, surface, cwd, trust, and explicit
// overrides.").
//
// PR-07 (issue #11) implements the first slice of that component:
//
//   - Worktree detection (worktree.go): resolve the Git worktree containing
//     an arbitrary directory to one stable, content-addressed ID, without
//     shelling out to git — a pure filesystem walk up to the nearest `.git`
//     entry keeps this hermetically testable and gives an identical result
//     regardless of which subdirectory of the worktree it is invoked from.
//   - Host detection (host.go): for the two first-party hosts (codex,
//     claude-code), resolve the real installed binary's path and exact
//     version, plus the native home locations
//     (docs/architecture/runtime.md §7.1, §7.2) an adapter must inventory —
//     CODEX_HOME / CLAUDE_CONFIG_DIR (or their unset-env-var default) and
//     the shared $HOME/.agents/skills root. Host detection deliberately
//     stops at locations, not contents: walking what is actually inside
//     those homes (Instructions, Skills, MCP registrations, ...) without
//     executing anything is internal/observe's job (docs/architecture/README.md
//     §3's "Observe Native and Runtime Sources" is the pipeline stage after
//     "Detect Context -> Resolve Host and Knowledge Pack").
//
// Both are driven through an explicit Environment (environment.go) rather
// than reading os.Getenv/os.Environ implicitly, and every host binary
// invocation this package makes is a hard-timeout, args-fixed-to-["--version"]
// subprocess call — never an interactive session or a model-calling command
// (the same safety boundary internal/qualify's RunInvocation enforces for a
// different reason: qualify sandboxes a fixture's HOME away from the real
// one to prove zero writes, while this package deliberately targets the
// real installation, since detecting "what is actually installed" is the
// whole point — see host.go's doc comment for why that changes how the
// subprocess environment is built).
package context
