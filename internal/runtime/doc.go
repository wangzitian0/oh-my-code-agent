// Package runtime compiles and activates immutable per-worktree generations
// (docs/architecture/README.md §6's planned layout: "runtime/ # bootstrap
// and generation compilation").
//
// This PR (issue #13, PR-09, "Bootstrap generation compiler + exclusion
// manifest") implements exactly the minimal bootstrap compiler
// docs/project/roadmap.md M1 names -- the "generate a minimal bootstrap
// home and virtual user home per host" and "exclude native user-global
// Instructions, Skills, MCP, Hooks, and Plugins from the bootstrap path"
// deliverables -- not the general Runtime Compiler docs/architecture/
// README.md §9's RuntimeCompiler.Compile / §4's "Render a complete
// immutable host generation from desired state" describes. That is PR-14
// ("Full generation compiler + content-addressed store"), which resolves a
// real Desired Graph (Profiles/Bindings/Activation, PR-12) into artifacts.
// PR-09 has no Desired Graph to resolve -- there is deliberately no
// Profile/Binding/Activation loading anywhere in this package -- so
// Bootstrap takes a fixed, hardcoded policy instead (policy.go's
// bootstrapPolicy): exclude every native user-global source, include the
// repository Instructions chain, apply conservative default permissions,
// activate no MCP servers.
//
// # Why this compiler does not implement internal/plugin.HostAdapter
//
// internal/plugin's HostAdapter is the frozen M0 plugin contract for a
// future, fuller integration (Detect/Capabilities/Observe/Resolve/Compile/
// Verify/Launch). Using it here would pull in Capabilities/Resolve/Verify/
// Launch scope this issue's acceptance criteria never ask for, built around
// a real Desired Graph this milestone does not have. This package is
// instead a direct compiler: internal/observe's already-computed inventory
// plus internal/context's already-computed host/worktree detection plus
// this package's own fixed policy -- matching PR-14 being the PR that owns
// the fuller HostAdapter-based integration.
//
// # Why desiredGraphDigest is a bootstrap-policy digest, not empty
//
// domain.GenerationSpec.DesiredGraphDigest is `required` and schema-
// validated as a canonical sha256 digest (schemas/protocol/
// generation.v1alpha1.schema.json), but a bootstrap generation is
// definitionally not derived from any real Desired Graph (see above).
// Rather than leaving the field empty (the schema's digest pattern would
// reject that) or fabricating a fake Profile, this package documents the
// actual fixed policy value driving compilation as its own small, named,
// versioned fact and digests *that* -- policy.go's bootstrapPolicy /
// BootstrapPolicyDigest. A future PR-12/PR-14 reader who sees
// desiredGraphDigest on a bootstrap generation should read this comment
// (and BootstrapPolicyVersion) to understand it does not reference any real
// Profile.
//
// # Why domain.Generation was extended rather than replaced
//
// internal/domain.Generation (PR-04) had no field for "excluded source +
// reason" or "capability gap" -- GenerationHostEntry only carried Surface/
// AdapterID/AdapterVersion/Ownership/Artifacts. This package's manifest
// needs exactly that (issue #13 AC "the manifest lists every included and
// excluded source with a reason"). Rather than defining an entirely
// separate manifest type in this package (or in internal/artifact, whose
// own doc.go declares an intent to "persist" manifests, not define their
// shape), GenerationSpec gained one new, additive, optional field --
// Sources []GenerationSourceEntry (internal/domain/generation.go) --
// because schemas/protocol/*.schema.json are v1alpha1 (pre-1.0; additive
// evolution is this project's own stated norm for the ontology's "known
// unknowns" philosophy, e.g. knowledge/hosts/*/manifest.json's
// knownUnknowns array) and every already-merged required field on
// Generation/GenerationSpec is untouched, so no existing caller of
// domain.Generation is affected. This is a documented judgment call
// (issue #13's own text flags it as one, not a mandate) -- see
// GenerationSourceEntry's doc comment for why it is named Sources, not
// Exclusions.
//
// # Per-host, not per-worktree, generations in this PR
//
// docs/architecture/runtime.md §5.5 describes one generation containing
// "one artifact tree per host and surface." This PR's Bootstrap compiles
// one host at a time, matching the issue's own function-shape description
// ("given a host's HostDetection + Worktree + that host's
// []domain.Observation... produces one immutable generation"); a caller
// wanting both first-party hosts calls Bootstrap twice, into two output
// directories, today. Combining multiple hosts' artifact trees under one
// shared generation ID/directory is PR-12/PR-14 scope (a real multi-host
// Desired Graph is what would drive "two hosts in one worktree run
// deliberately different loadouts from one desired state," roadmap M2's
// exit gate) -- not invented here.
//
// # The OMCA MCP server registration (closed by issue #15 / PR-11)
//
// docs/project/roadmap.md M1's abstract deliverable list names three
// bootstrap-generation contents: conservative permission defaults, "the
// OMCA MCP server", and project-loadable Instructions. PR-09 (this
// package's original PR) left the second of those three as a documented
// scope cut: internal/mcp was still an empty doc.go stub, so there was no
// real command or protocol handler a generated config entry could point at.
// PR-11 (issue #15) built internal/mcp and `omca mcp serve`, and closed the
// gap here: compile.go's hostConfigFiles now writes an `[mcp_servers.omca]`
// entry into codex's config.toml, and a `.claude.json` carrying
// `mcpServers.omca` for claude-code, whenever BootstrapRequest.
// OMCABinaryPath is supplied (cmd/omca/env.go and cmd/omca/run.go always
// supply it in production, as the worktree's own stable PATH-shim path --
// never a snapshot of the currently-running omca binary's own resolved
// location; every test predating PR-11 leaves it empty and simply gets a
// generation with no MCP registration, exactly PR-09's original behavior).
// See request.go's OMCABinaryPath doc comment for why this value
// deliberately does NOT fold into GenerationID.
//
// # PR-14 (issue #18): the full generation compiler this doc.go anticipated
//
// Everything above describes PR-09's original scope. PR-14 ("Full
// generation compiler + content-addressed store") is the PR this doc.go's
// very first paragraph named by number: it adds compile_full.go's Compile /
// CompileRequest, resolving a real Desired Graph (Profiles/Activation/
// Exceptions -- Binding selection itself stays PR-12 scope, not performed
// here) into artifacts, exactly as promised above. Four things changed as a
// result, each documented in more depth at its own definition:
//
//   - compile.go's compileHostTree stopped taking a BootstrapRequest and
//     instead takes a hostTreeInput carrying a caller-supplied Classify
//     function and Permissions map -- the "one compiler, two entry points"
//     seam issue #18's round-2 MECE requirement asks for. Bootstrap
//     (bootstrap.go) supplies the fixed M1 policy (classify, nil
//     permissions); Compile (compile_full.go) supplies the same classify
//     function today (see that file's doc comment for the honest reason:
//     no Identity Matcher exists yet to connect a Profile-declared asset ID
//     to a discovered file path) plus the real resolved permissions
//     (mergePermissions). Both route through the package-level
//     compileHostTreeFn variable, which sharedcore_test.go's
//     TestSharedCompilerCore_BootstrapAndCompileUseSameCore uses to prove
//     mechanically that neither entry point forked its own copy of the
//     tree-walking logic.
//   - internal/domain.GenerationSpec/GenerationMetadata gained several more
//     additive fields (OntologyVersion, SourceDigest, DesiredState, Diff,
//     RiskConfirmations, ExpectedEvidence, Metadata.Invocation), closing the
//     gap between the PR-09-era field list and every line of
//     docs/architecture/runtime.md §5.3's pending-manifest field list -- see
//     internal/domain/generation.go's own doc comments for which of these
//     are genuinely populated now versus honestly reserved for PR-15/PR-22.
//   - current.go's SetCurrentGeneration/CurrentGenerationDir/
//     ReadCurrentRecord were refactored onto a shared setGenerationPointer/
//     generationPointerDir/readPointerRecord implementation (parameterized
//     by pointer name), and pending.go adds SetPendingGeneration/
//     PendingGenerationDir/ReadPendingRecord as the exact same shape for a
//     second, independent pointer -- "current" and "pending" now both exist
//     under worktree state, matching docs/architecture/runtime.md §5's
//     layout diagram. ledger.go adds an append-only, per-host JSON-Lines
//     ledger (AppendLedgerEntry/ReadLedger). None of this is PR-15's
//     Activation transaction (validate pending -> ensure source digests
//     still match -> atomically switch current -> verify -> append Ledger
//     entry, §5.4): this PR only makes "pending" and the ledger exist and
//     be plainly writable/readable. Comparing pending against current,
//     atomically switching, verifying, and rolling back all remain PR-15's
//     job -- see pending.go's own doc comment for the explicit boundary.
//   - The "Per-host, not per-worktree" section above is now historical for
//     Bootstrap specifically (it still compiles one host per call, on
//     purpose -- matching its own minimal-input, PR-09-era signature) but no
//     longer describes this package as a whole: Compile takes every host in
//     one CompileRequest and writes them into ONE Generation with multiple
//     Spec.Hosts entries and one shared manifest.json/generation directory
//     -- see compile_full.go's own doc comment for why that reading of
//     docs/architecture/runtime.md §5.5 was chosen over "one Generation per
//     host."
package runtime
