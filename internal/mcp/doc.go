// Package mcp implements the OMCA MCP server: the model-facing control
// interface docs/architecture/README.md §2/§4 and docs/architecture/
// runtime.md §6 ("MCP-first Reconciliation") describe, and
// docs/product/requirements.md FR-7 requires inside every bootstrap
// generation ("the safe baseline, selected project-loadable inputs, and the
// OMCA MCP server").
//
// # Scope: the full four-tool M4 surface
//
// docs/project/roadmap.md's M4 milestone ships the full four-tool surface,
// and this package now implements all of it: omca_status (issue #15,
// PR-11), returning the current worktree/context identity, the current
// generation ID for each managed host, and the native user-global MCP
// server/Skill exclusion counts and estimated context-cost delta versus an
// unmanaged native launch; omca_query (issue #24, PR-20), a thin MCP-shaped
// wrapper over internal/report's already-built Build/ComparePlanes/Explain
// engine that answers logical-entity, drift-card, Knowledge-evidence,
// generation, and whole-report-artifact questions about the SAME bound
// worktree/generation omca_status reports on (see query.go's package doc
// comment and QueryArguments' own doc comment for why a tool-call argument
// can never retarget either tool to a different one); and omca_propose +
// omca_stage (issue #25, PR-21), the write-proposing half of the surface:
// omca_propose (propose.go) validates a caller-supplied domain.
// RepairProposal document against the bound report's fingerprint, its
// schema, capability gates, ownership (docs/adr/0002-ownership.md),
// already-resolved policy (internal/resolve.Resolve's DENIED outcomes), and
// classifies its risk into domain.RepairConfirmation
// (docs/product/requirements.md §7) — never writing anything; omca_stage
// (stage.go) fully re-validates the same document (report fingerprint
// included, CAS-style, mirroring internal/runtime/activate.go's Activate
// re-check) and, only when it classifies as AUTO_STAGE, compiles it into a
// fresh pending generation via internal/runtime.Compile, returning a diff
// and restart_required per host — every other confirmation class is a hard
// rejection naming the class, and omca_stage never touches "current" (there
// is no in-MCP human-confirmation flow in M4; that is PR-31/M7 TUI scope).
//
// # Why a hand-rolled stdio JSON-RPC 2.0 server, not an MCP SDK dependency
//
// The Model Context Protocol's stdio transport is intentionally simple:
// newline-delimited JSON-RPC 2.0 messages over stdin/stdout, nothing else
// permitted on stdout (see server.go's Serve doc comment, verified against
// the official specification's transports.mdx). A fixed-schema server
// exposing a small, closed set of read-only tools is a small, closed
// surface — three JSON-RPC methods (initialize, tools/list, tools/call)
// plus the notifications/initialized handshake notification — well inside
// this project's stdlib-first convention used elsewhere for a
// similarly-scoped hand-rolled protocol harness (internal/qualify's own
// subprocess harness, invoke.go). Pulling in a full MCP SDK (a real, larger
// dependency, with its own concurrency model, transport abstractions, and
// versioning to track) to serve a small, closed set of fixed-schema tools
// would be exactly the kind of dependency this project's stated
// stdlib-first judgment rejects absent a compelling reason; none was found
// yet, including now that the surface has grown to four tools — issue #25's
// own round-4 audit deliberately keeps every response shape small
// (ProposeResult/StageResult carry no unbounded field) rather than reaching
// for session/concurrency machinery an SDK would provide for free.
//
// # Package shape
//
//   - server.go: the stdio JSON-RPC 2.0 message loop (Serve), the MCP
//     protocol envelope types (initialize/tools handshake), and the
//     name-to-handler Registry every registered tool's tools/list
//     definition and tools/call dispatch both come from (issue #24's round-4
//     audit: "Generalize... into a real name-to-handler registry" — and
//     issue #25's own round-4 audit generalizing it one step further: Serve
//     takes an already-built Registry, not a growing per-tool callback
//     parameter list — see Registry's own doc comment). cmd/omca/mcp.go's
//     runMCP composes the full four-tool Registry via NewRegistry(
//     StatusToolEntry(...), QueryToolEntry(...), ProposeToolEntry(...),
//     StageToolEntry(...)) and hands it to Serve.
//   - status.go: the omca_status tool's fixed response schema (StatusResult)
//     and ComputeStatus, which builds one from already-resolved inputs
//     (worktree/context identity, state directory, host list) — never from
//     ambient environment variables or the real filesystem directly, the
//     same "explicit inputs" discipline internal/context.Environment,
//     internal/observe.Request, and internal/runtime.BootstrapRequest all
//     already follow. cmd/omca/mcp.go is the one place that reads
//     OMCA_WORKTREE_ID/OMCA_CONTEXT_ID/OMCA_STATE_DIR from the process
//     environment and hands the resolved values in.
//   - query.go: the omca_query tool's response schema (QueryResult) and
//     ComputeQuery, a pure projection of an already-built internal/report.
//     Artifact (never re-deriving report-artifact traversal itself) plus a
//     shared offset/limit paging discipline every list-shaped query kind
//     uses to keep its default response small (issue #24's "size-budget
//     test bounds tool schemas and default responses" acceptance
//     criterion). cmd/omca/mcp.go supplies the ArtifactFunc that computes a
//     FRESH Artifact for every call, via the identical detect-observe-
//     compose-Build pipeline cmd/omca/reportbuild.go's buildArtifactForCLI
//     already runs for every `omca report`/`omca drift`/... CLI invocation.
//   - propose.go: the omca_propose tool's ComputePropose, the pure six-gate
//     validation-and-classification engine (schema, fingerprint, ownership,
//     capability, policy, risk) over a caller-supplied domain.
//     RepairProposal plus a ProposeContext (a fresh report.Artifact and a
//     fresh CapabilityFunc). Never writes anything; a rejected proposal
//     surfaces as a *ProposeRejectedError naming the exact gate that failed.
//   - stage.go: the omca_stage tool's ComputeStage, which reruns
//     ComputePropose in full (CAS-style re-validation, report fingerprint
//     included) and, only for an AUTO_STAGE result, calls a caller-supplied
//     CompileFunc to compile the proposal's Activation changes into a fresh
//     pending generation, then projects the result into a per-host diff
//     (internal/runtime.DiffProposedChanges) and restart_required verdict.
//     Every other confirmation class is a *StageRejectedError naming the
//     class; ComputeStage itself never performs I/O and never touches
//     "current".
package mcp
