// Package mcp implements the OMCA MCP server: the model-facing control
// interface docs/architecture/README.md §2/§4 and docs/architecture/
// runtime.md §6 ("MCP-first Reconciliation") describe, and
// docs/product/requirements.md FR-7 requires inside every bootstrap
// generation ("the safe baseline, selected project-loadable inputs, and the
// OMCA MCP server").
//
// # Scope: the read surface (omca_status + omca_query)
//
// docs/project/roadmap.md's M4 milestone is where the full four-tool
// surface (omca_status, omca_query, omca_propose, omca_stage) ships. This
// package implements the two read-only tools of that set: omca_status
// (issue #15, PR-11), returning the current worktree/context identity, the
// current generation ID for each managed host, and the native user-global
// MCP server/Skill exclusion counts and estimated context-cost delta versus
// an unmanaged native launch; and omca_query (issue #24, PR-20), a thin
// MCP-shaped wrapper over internal/report's already-built Build/
// ComparePlanes/Explain engine that answers logical-entity, drift-card,
// Knowledge-evidence, generation, and whole-report-artifact questions about
// the SAME bound worktree/generation omca_status reports on (see query.go's
// package doc comment and QueryArguments' own doc comment for why a
// tool-call argument can never retarget either tool to a different one).
// omca_propose/omca_stage remain out of scope — calling either is
// indistinguishable, from this server's perspective, from calling any other
// unknown method, and gets the standard JSON-RPC "method not found" error.
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
// versioning to track) to serve a couple of fixed-schema tools would be
// exactly the kind of dependency this project's stated stdlib-first
// judgment rejects absent a compelling reason; none was found yet. A future
// PR that grows this into the full four-tool surface docs/architecture/
// runtime.md §6 describes should revisit this call once the protocol
// surface actually needs session/concurrency machinery an SDK would
// provide for free.
//
// # Package shape
//
//   - server.go: the stdio JSON-RPC 2.0 message loop (Serve), the MCP
//     protocol envelope types (initialize/tools handshake), and the
//     name-to-handler toolRegistry every registered tool's tools/list
//     definition and tools/call dispatch both come from (issue #24's round-4
//     audit: "Generalize... into a real name-to-handler registry" —
//     PR-11's original single hardcoded `if p.Name != toolNameStatus` check
//     is gone, so PR-21's two additional tools are a matter of appending to
//     newToolRegistry, not touching this dispatch code again).
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
package mcp
