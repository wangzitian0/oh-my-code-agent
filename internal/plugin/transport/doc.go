// Package transport is the M6 out-of-process plugin transport (issue #29,
// "PR-25 · Out-of-process plugin transport"): it lets a HostAdapter live in a
// separate OS process from the omca core and still speak the exact same
// frozen v1 contract (internal/plugin.HostAdapter), one JSON-RPC 2.0 method
// per interface method, over the adapter subprocess's own stdin/stdout.
//
// This is not a new wire convention invented for this package: it mirrors
// internal/mcp/server.go's already-established stdio JSON-RPC 2.0 framing
// (newline-delimited messages, one request answered before the next is
// read) rather than a second, differently-shaped transport living in the
// same codebase.
//
// Two halves:
//
//   - Serve (server.go) wraps ANY plugin.HostAdapter implementation — a
//     first-party in-process adapter or a genuine third-party one — behind
//     the wire protocol. An external adapter binary's own main() calls this
//     directly against os.Stdin/os.Stdout.
//   - RemoteAdapter (client.go) is the client side: it itself implements
//     plugin.HostAdapter by shelling out to an already-built external binary
//     and speaking the same wire protocol back. Because RemoteAdapter is
//     just another plugin.HostAdapter, it can be driven through
//     internal/plugin/conformance.Run exactly like an in-process adapter —
//     that is the mechanism that proves the transport preserves contract
//     parity across the process boundary, rather than a second,
//     transport-specific conformance suite.
//
// A "handshake" pseudo-method (not part of the HostAdapter interface itself)
// lets RemoteAdapter learn the remote adapter's own plugin.PluginManifest at
// construction time, without the core ever needing the external binary's
// source compiled in — the core only needs the binary's path. Contract
// version compatibility is then checked the exact same way an in-process
// adapter's manifest is: internal/plugin.Registry.Register's existing
// CompatibleContractVersion check, not a second version-negotiation scheme
// invented here.
package transport
