# Deferred: Generation compilation

`docs/knowledge/README.md` §3 names `expected-generation/` in every fixture
case's directory shape. Runtime Generation compilation (turning a Desired
Graph + Knowledge Pack into a compiled, content-addressed artifact tree) is
M2 work (`docs/project/roadmap.md`, M2 deliverable "Compile complete
content-addressed generations with per-host artifact trees") — no compiler
exists yet in this repository (`internal/reconcile`, `internal/observe` are
still `doc.go` stubs as of PR-06).

This directory intentionally holds no fabricated compiled artifacts. Once
PR-08+ lands a real Compile implementation, this case should gain a genuine
`expected-generation/` tree and this note should be replaced.
