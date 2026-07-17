// Package observe discovers and inventories coding-agent sources without
// executing them (issue #12, PR-08: "minimal observation").
//
// Scope: this package covers exactly the user-global and repository
// (workspace-scoped) physical sources for three concepts — Instructions,
// Skills, and MCP server registrations — for the two first-party hosts,
// codex and claude-code, per the physical mappings documented in
// docs/ontology/README.md §6.1 (Claude Code) and §6.2 (Codex) and
// docs/architecture/runtime.md §7.1/§7.2's "the adapter must inventory at
// least" lists. System sources (/etc/codex, managed policy), the
// root-to-cwd nested "directory" scope chain, session-scoped sources (CLI
// flags, session state), and Hooks/Plugins/Agents are explicitly out of
// scope here — issue #20 (PR-16, "Deep observation") covers those later.
// This is a deliberate, documented scope cut, not an oversight: the issue's
// own acceptance criteria ask for "just enough lossless inventory... to
// explain every exclusion the bootstrap makes," not full precedence
// resolution or the deep per-directory walk.
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
package observe
