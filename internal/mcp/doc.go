// Package mcp implements the OMCA MCP server: the model-facing control
// interface docs/architecture/README.md §2/§4 and docs/architecture/
// runtime.md §6 ("MCP-first Reconciliation") describe, and
// docs/product/requirements.md FR-7 requires inside every bootstrap
// generation ("the safe baseline, selected project-loadable inputs, and the
// OMCA MCP server").
//
// # Scope: M1 status-only stub
//
// docs/project/roadmap.md's M4 milestone is where the full four-tool
// surface (omca_status, omca_query, omca_propose, omca_stage) ships. This
// package implements exactly one of those four tools today: omca_status,
// returning the current worktree/context identity, the current generation
// ID for each managed host, and the native user-global MCP server/Skill
// exclusion counts and estimated context-cost delta versus an unmanaged
// native launch (issue #15, "Status-only MCP stub... satisfying charter
// FR-7 until M4 completes the tool set"). omca_query/omca_propose/
// omca_stage are explicitly out of scope here — calling them is
// indistinguishable, from this server's perspective, from calling any other
// unknown method, and gets the standard JSON-RPC "method not found" error.
//
// # Why a hand-rolled stdio JSON-RPC 2.0 server, not an MCP SDK dependency
//
// The Model Context Protocol's stdio transport is intentionally simple:
// newline-delimited JSON-RPC 2.0 messages over stdin/stdout, nothing else
// permitted on stdout (see server.go's Serve doc comment, verified against
// the official specification's transports.mdx). A fixed-schema server
// exposing exactly one read-only tool is a small, closed surface — three
// JSON-RPC methods (initialize, tools/list, tools/call) plus the
// notifications/initialized handshake notification — well inside this
// project's stdlib-first convention used elsewhere for a similarly-scoped
// hand-rolled protocol harness (internal/qualify's own subprocess harness,
// invoke.go). Pulling in a full MCP SDK (a real, larger dependency, with its
// own concurrency model, transport abstractions, and versioning to track)
// to serve one fixed-schema, no-argument tool would be exactly the kind of
// dependency this project's stated stdlib-first judgment rejects absent a
// compelling reason; none was found for a stub this narrow. A future PR
// that grows this into the full four-tool, paged, Artifact-URI-capable
// surface docs/architecture/runtime.md §6 describes should revisit this
// call once the protocol surface actually needs session/concurrency
// machinery an SDK would provide for free.
//
// # Package shape
//
//   - server.go: the stdio JSON-RPC 2.0 message loop (Serve) and the MCP
//     protocol envelope types (initialize/tools handshake).
//   - status.go: the omca_status tool's fixed response schema (StatusResult)
//     and ComputeStatus, which builds one from already-resolved inputs
//     (worktree/context identity, state directory, host list) — never from
//     ambient environment variables or the real filesystem directly, the
//     same "explicit inputs" discipline internal/context.Environment,
//     internal/observe.Request, and internal/runtime.BootstrapRequest all
//     already follow. cmd/omca/mcp.go is the one place that reads
//     OMCA_WORKTREE_ID/OMCA_CONTEXT_ID/OMCA_STATE_DIR from the process
//     environment and hands the resolved values in.
package mcp
