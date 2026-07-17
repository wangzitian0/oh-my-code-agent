// Package shim implements the non-recursive PATH shim and the exec-replace
// launch discipline docs/architecture/runtime.md §4 describes ("The shim
// locates the real host binary without recursively invoking itself, selects
// the current generation, injects host-specific environment, and uses exec
// so signal and exit behavior remain native.") and issue #14 (PR-10) makes
// an acceptance criterion.
//
// # Why this is its own package, not code inside cmd/omca
//
// This is the one production code path in the whole module that both (a)
// resolves a binary purely from an env-var-supplied PATH/shim-dir pair with
// no host detection of its own, and (b) calls syscall.Exec — replacing the
// calling process's image, never returning on success. Isolating it here
// keeps that hazard contained to a small, independently unit-testable
// surface, and keeps the shim's own hot path free of internal/context's
// heavier host-detection machinery (spawning `--version` probes, walking
// native homes) that this package deliberately does not need: everything a
// shim invocation requires — which real binary, which generation, which env
// var to set — was already decided once by `omca env`/`omca run` and handed
// forward through environment variables and the on-disk "current" pointer
// (internal/runtime's SetCurrentGeneration/CurrentGenerationDir), not
// recomputed on every `codex`/`claude` invocation. That split is also what
// keeps steady-state shim overhead inside the roadmap M1 exit gate's "tens
// of milliseconds, not seconds" budget.
//
// # The non-recursion design decision
//
// The classic shim bug (asdf/rbenv/pyenv-style tools all have documented
// history of it): resolving "the real binary" via a PATH lookup that still
// includes the shim's own directory finds the shim again, because
// `omca env` always prepends the shim directory to PATH — infinite
// recursion the moment the shim tries to exec what it thinks is the real
// binary.
//
// docs/architecture/runtime.md §4's export list gives the shim exactly one
// PATH-shaped input to work with beyond the ambient PATH itself:
// OMCA_SHIM_DIR. This package resolves the real binary by taking the
// ambient PATH the shim process actually received and filtering out every
// entry that resolves (symlink-evaluated) to OMCA_SHIM_DIR before doing a
// plain, first-match PATH search (resolve.go) — rather than the
// alternative the issue also sanctions, baking a separate
// OMCA_REAL_CODEX_PATH/OMCA_REAL_CLAUDE_PATH env var per host at `omca env`
// time. The PATH-filtering approach was chosen because it needs no new,
// undocumented environment variable beyond the five docs/architecture/
// runtime.md §4 already names, and because the identical filtering logic is
// independently useful to `omca run` (FilterOutDir): before that command
// even calls internal/context.DetectHost, it must strip the shim directory
// out of the ambient PATH too, or a `omca run codex` typed inside an
// already-managed shell would detect and try to exec the shim itself as
// "the codex binary."
//
// # Exec, not fork+wait
//
// ExecReplace is the one call to syscall.Exec in this project. On success
// it never returns: the calling process's image becomes the real host
// binary, so the OS delivers every subsequent signal (SIGINT, SIGTERM, ...)
// directly to it, and its exit code becomes the shim process's own exit
// code with no forwarding logic required. os/exec.Cmd.Run, by contrast,
// leaves the shim as a separate parent process and would require this
// package to reimplement signal forwarding and exit-code propagation by
// hand — exactly what docs/architecture/runtime.md §4 says exec exists to
// avoid.
package shim
