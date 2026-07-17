// Package observe discovers and inventories coding-agent sources without
// executing them (issue #12, PR-08: "minimal observation"; issue #20,
// PR-16: "Deep observation").
//
// Scope: this package covers six concepts — Instructions, Skills, MCP
// server registrations, Hooks, Policy/trust state, and Plugins/Extensions —
// across every scope docs/ontology/README.md §2 names a physical source for
// in §6.1 (Claude Code) and §6.2 (Codex): user-global native homes,
// repository/workspace roots, the root-to-cwd nested `directory` scope
// chain, machine/managed (`system`) locations, Claude Code's `local` scope
// (CLAUDE.local.md), and caller-supplied `session`-scoped facts — for the
// two first-party hosts, codex and claude-code. coverage.go's Coverage()
// function is this package's own explicit, per-dimension self-report of
// exactly how completely each of the resulting 12 (host, concept) cells is
// covered, rather than a single blended claim.
//
// PR-08 originally deferred system/directory/session/local/Hooks/Plugins
// scope, plus the Agents concept and connector/cloud-side MCP sources — see
// git history for this file's PR-08-era wording. PR-16 closes every one of
// those except Agents (never named in issue #20's required-concept list)
// and connectors (not a filesystem-discoverable source at all; ontology
// itself calls them out as a distinct control-plane concern). A few
// narrower gaps remain, each a deliberate, documented choice rather than a
// silent omission — see rules.go/system.go's per-cell doc comments for the
// specifics (e.g. Codex's Plugin discovery root is this package's own
// unconfirmed convention, not a vendor-documented path; Claude Code's
// system-scope MCP and "enterprise" Skills have no independently confirmed
// physical filename this pass could find) and coverage.go, which marks
// every such cell UNKNOWN or PARTIAL rather than EXACT.
//
// # Pipeline position
//
// internal/context locates things (DetectHost's HostDetection.NativeHomes
// and DetectWorktree's Worktree.Root); this package walks what is inside
// those locations. See internal/context/host.go's NativeHome doc comment:
// "Host detection reports the location itself, not its contents — walking
// what is inside it is internal/observe's job." A typical caller composes
// the two directly:
//
//	report, _ := hostcontext.Detect(ctx, cwd, hostcontext.RealEnvironment())
//	for _, hd := range report.Hosts {
//	    obs, _ := observe.Observe(observe.Request{
//	        Detection:    hd,
//	        WorktreeRoot: report.Worktree.Root,
//	    })
//	}
//
// # Safety properties
//
//  1. Zero write, zero exec. This package never calls os.WriteFile,
//     os.MkdirAll, os.Remove, or any exec.Command — it does not even import
//     os/exec (see zerowrite_test.go's static import-boundary check). Every
//     filesystem call it makes is read-only: os.Stat/os.Lstat, os.ReadFile,
//     filepath.WalkDir. This is proven dynamically too, against a sandboxed
//     copy of a realistic host home layout built from this repo's own
//     fixtures/ corpus (internal/qualify's Sandbox and snapshot-diff
//     machinery, reused rather than reinvented per qualify/doc.go: "PR-08's
//     real observation code is expected to reuse Sandbox, snapshot/diff, and
//     the Observation-building helpers rather than re-deriving them").
//  2. Determinism. Observe never reads a clock, never relies on map
//     iteration order, and always explicitly sorts its output by
//     Metadata.ID before returning, so running it twice over an unchanged
//     fixture tree yields byte-identical JSON (determinism_test.go).
//  3. Unknown fields preserved opaquely. Observe does not attempt a full
//     semantic parse of any source: a JSON-shaped MCP registration file
//     (Claude Code's .claude.json / .mcp.json) is decoded losslessly into a
//     generic map (nothing cherry-picked, nothing dropped) and every other
//     source (Instructions markdown, Codex's TOML config, SKILL.md) is
//     retained as its raw text — both cases populate
//     ObservationSpec.OpaqueVendorFields rather than a hand-modeled struct,
//     and are marked EvidenceLevelParsed (E1): "parsed losslessly or
//     retained safely as opaque."
//  4. Redaction-safe output path. Observe itself returns raw-ish content
//     (inside OpaqueVendorFields) — redaction is a documented, mandatory
//     step at the boundary, not something Observe does internally: a
//     caller persisting or reporting an Observation MUST pass it through
//     internal/domain/redact (redact.Value/redact.JSON) first. This mirrors
//     redact's own package doc ("the redaction rules every OMCA output path
//     must apply before a document leaves the process"). redact_test.go
//     proves a realistic secret-bearing fixture (an MCP server's env block)
//     never survives that real call path end-to-end, for both the
//     structural (parsed JSON key-name) and shape-based (opaque TOML/text)
//     redaction routes this package's two content-handling branches
//     exercise.
//  5. Every record is complete. Every Observation Observe returns carries
//     Source{Kind,Path,Digest}, Scope{Kind,Root}, RawDigest, ParsedDigest,
//     and EvidenceLevel — validated with domain.ValidateObservation in
//     tests. Evidence level is always E0 (EvidenceLevelDiscovered: the path
//     was found but its content could not be read — e.g. a permission
//     error) or E1 (EvidenceLevelParsed: content was read and retained,
//     parsed or opaque) — never a higher level, since this package proves
//     nothing about precedence, host-reported state, or runtime behavior.
//  6. Credential material is never read, structurally rather than by
//     after-the-fact redaction (PR-16, issue #20's hard safety rule).
//     rules.go's sourceRule.discoverOnly marks a source this package knows,
//     from its physical location and documented purpose alone, may mix
//     permission/trust state with credential material (e.g. Codex's
//     $CODEX_HOME/auth.json) — walk.go's observeFile never calls
//     os.ReadFile for such a source, regardless of whether the file is
//     actually readable; the resulting record is always E0
//     (discovered-but-unopened), the same conservative "treat it
//     conservatively rather than guessing safe" outcome issue #20 asks for.
//     A source this package DOES read in full (e.g. Claude Code's
//     .claude.json/settings.json, which mix legitimate non-secret config
//     with trust/permission fields) instead relies on safety property 4
//     above (the internal/domain/redact boundary) — redact_test.go proves
//     that path end-to-end for both.
//  7. Session-scoped facts are caller-supplied, never self-read. Observe
//     never reads the real process's argv or environment (see the Request
//     doc comment); session.go's SessionInput lets a caller that DOES have
//     legitimate access to those (e.g. a future CLI command) hand an
//     already-resolved fact to Observe for inventory, keeping this
//     package's own "every path/fact it processes comes from the Request
//     struct, nothing ambient" invariant intact even for the `session`
//     scope.
package observe
