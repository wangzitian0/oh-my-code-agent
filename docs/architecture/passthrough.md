# Candidate environment passthrough

Status: draft

This is the preview-only first increment of
[issue #99](https://github.com/wangzitian0/oh-my-code-agent/issues/99).
It does not activate passthrough rules or change existing runtime behavior.
The credential constraints in [ADR 0003](../adr/0003-credential-handling.md)
continue to apply.

## Declaration and inspection

A worktree may contain `.omca/passthrough.yaml`:

```yaml
apiVersion: omca.dev/v1alpha1
kind: PassthroughPolicy
passthrough:
  env:
    ASDF_DATA_DIR:
      category: runtime
      sourceEnv: HOST_ASDF_DATA_DIR
    UV_CACHE_DIR:
      category: cache
      sourceEnv: HOST_UV_CACHE_DIR
```

Run `omca passthrough preview`, or use
`omca passthrough preview --file candidate.yaml` for an explicit candidate.
The command emits versioned JSON with `mode: preview-only`, `applied: false`,
and sorted entries containing target/source **names**, declared category,
nonempty source presence, and `qualification: unqualified`. It never prints,
evaluates, exports or stores environment values, reads referenced paths, runs
a host, initializes state, or activates a generation. Exit 0 means the
candidate structure is valid; it does not mean the sources exist or that the
host/tool supports the declaration. Missing and empty sources have
`sourcePresent: false`. Malformed input exits nonzero without echoing input.

`omca doctor` independently checks the current worktree's default candidate.
Missing policy or a valid but unqualified candidate is WARN; invalid policy is
FAIL. Existing doctor checks still run independently. A candidate never earns
an OK compatibility finding from environment presence alone.

Only the explicit `apiVersion`, `kind` and `passthrough.env` fields are accepted.
Each entry requires `category` (`cache` or `runtime`) and `sourceEnv` (an uppercase
shell environment name). No literal value, path expression, command, wildcard,
identity category, extra document or unknown field is accepted. Policy reads
are bounded to 64 KiB and reject final symlinks and nonregular files. Default
lookup also rejects a symlinked `.omca` directory. There is no policy discovery
outside the selected worktree or implicit personal-home fallback.

## Classification is intent, not evidence

| Class | Candidate preview | Future activation requirement |
|---|---|---|
| Identity / credentials | Rejected | Separate identity-bound, qualified reference mechanism under ADR 0003 |
| Cache | Declaration only | Proof that the selected directory does not also contain credentials or host configuration |
| Runtime data | Declaration only | Exact tool/host evidence and isolation regression fixtures |
| Mixed or hard-coded HOME state | Unsupported / unqualified | A separately qualified mechanism; no blanket restoration of HOME |

HOME, host configuration roots, OMCA-managed variables, executable search paths,
known credential/configuration variables and credential-like names are reserved
as both targets and sources. `CARGO_HOME` is deliberately unavailable in this
cache-only contract: declaring a whole tool home to be a cache does not prove
that its contents are safe. Cargo registry sharing remains unqualified pending
a narrower, evidenced mechanism. The example variable names above illustrate
candidate intent, not a claim that asdf or uv has been qualified by this PR.

The reserved-name checks are conservative mistakes prevention, not a complete
secret detector. Arbitrary names can conceal sensitive or mixed data. Presence
checks do not inspect those values and cannot establish their trustworthiness.
No rules, including apparently harmless ones, are consumed by `omca env`,
`omca run`, the compiler, or the shim in this increment.

## Remaining qualification gates

Activation requires a separately reviewed design for source trust/provenance,
identity binding, generation digest and manifest contracts, conflict handling,
rollback and host/tool evidence. Fake-host isolation must precede bounded real
qualification, including native Git identity exclusion and keyring behavior.
Until then #99 and its related #47/#55/#65 work remain open. A tool that requires
hard-coded native HOME access without a qualified redirect remains unsupported;
OMCA does not patch arbitrary tool internals to make the declaration work.

HOME virtualization is convention-level isolation against accidental global
configuration inheritance, not an execution sandbox. Preview adds no filesystem,
network or process enforcement and copies no native identities or credentials.
