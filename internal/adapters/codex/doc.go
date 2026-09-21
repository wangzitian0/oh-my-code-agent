// Package codex is the reserved home of the OpenAI Codex host adapter
// plugin. It is empty: no adapter is implemented here yet.
//
// Codex's host semantics currently live in hardcoded switches across the
// core (docs/architecture/README.md §9.1 inventories them), not behind
// internal/plugin's HostAdapter contract. The package exists so that
// internal/plugin/importboundary_test.go has a real path to enforce the
// core-must-not-import-adapters boundary against before there is anything
// to import, and so the eventual adapter lands where the architecture says
// it should.
//
// The previous doc comment said this package "implements the OpenAI Codex
// host adapter plugin," which read as a statement of fact about a package
// containing no code. Reserved-but-empty is the honest version, and it is
// the same correction ADR 0006 made for a claim the code had stopped
// backing.
package codex
