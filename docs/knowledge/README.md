# Host Knowledge Lifecycle

Status: draft

## 1. Boundary

The [Ontology](../ontology/README.md) defines stable canonical vocabulary. A
Knowledge Pack defines changing facts about one host surface and version range.

```text
Ontology
  concept: skill
  meaning: discoverable procedural capability

Knowledge Pack
  host: codex
  surface: cli
  version: 0.144.x
  user discovery root: $HOME/.agents/skills
  duplicate behavior: keep both
  evidence: official docs + executable fixture
```

Adapter code implements parsing and compilation. It must consume a Knowledge
Pack rather than embedding floating vendor facts without evidence.

## 2. Goals

- Preserve host version, surface, platform, scope, precedence, and trust facts.
- Separate stable ontology changes from frequent vendor releases.
- Make every managed capability traceable to primary evidence and fixtures.
- Detect when a newly installed host exceeds qualified behavior.
- Produce reviewable update pull requests rather than silent runtime updates.
- Keep historical generations explainable after Knowledge advances.

## 3. Repository Layout

```text
knowledge/
├── sources.yaml
└── hosts/
    └── <host>/
        └── <surface>/
            └── <version-or-range>/
                ├── manifest.yaml
                ├── capabilities.yaml
                ├── discovery.yaml
                ├── precedence.yaml
                ├── evidence.yaml
                └── migrations.yaml

fixtures/
└── <host>/
    └── <version>/
        └── <case>/
            ├── input/
            ├── invocation.yaml
            ├── expected-observations.json
            ├── expected-effective.json
            └── expected-generation/
```

Knowledge Packs are immutable after publication. Corrections publish a new Pack
and mark the old one superseded; they do not rewrite history.

## 4. Knowledge Pack Contract

```yaml
apiVersion: omca.dev/v1alpha1
kind: HostKnowledge

metadata:
  id: codex:cli:0.144
  host: codex
  surface: cli
  versionRange: ">=0.144.0 <0.145.0"
  platforms: [darwin-arm64]
  observedAt: 2026-07-16
  recheckAfter: 2026-08-16
  status: FRESH

evidence:
  - id: codex-environment-variables
    kind: official-doc
    url: https://learn.chatgpt.com/docs/config-file/environment-variables
    digest: sha256:...
  - id: codex-user-skill-fixture
    kind: executable-fixture
    path: fixtures/codex/0.144/user-skill-discovery
    digest: sha256:...

capabilities:
  skill:
    discover: EXACT
    parse: EXACT
    normalize: EXACT
    resolve: EXACT
    compile: PARTIAL
    verify: PARTIAL
    reconcileMode: PATCHED
    verificationMethods: [static-resolver, host-list]

precedencePrograms:
  - id: codex.skills.discovery
    identity: skill-name-plus-source
    operator: KEEP_BOTH
    fixture: codex-user-skill-fixture

knownUnknowns:
  - whether every app surface shares the CLI discovery cache
```

## 5. Capability Vocabulary

Each `host × surface × version × concept × operation` uses one relation:

| Relation | Meaning |
|---|---|
| `EXACT` | Semantics and representation are proven for the declared operation. |
| `COMPATIBLE` | Canonical behavior is compatible but native representation differs. |
| `PARTIAL` | Only declared fields or scenarios are supported. |
| `OPAQUE` | Location and content digest are preserved without semantic interpretation. |
| `UNKNOWN` | Primary evidence does not establish behavior. |
| `UNSUPPORTED` | The host has no corresponding operation or concept. |

The resulting reconcile mode is independent:

```text
MANAGED
PATCHED
OBSERVED
OPAQUE
BLOCKED
```

There is no host-wide “supported” flag.

## 6. Evidence Priority

Use the narrowest primary source that establishes the claim:

1. Official machine-readable schema or native effective-state output.
2. Official product documentation.
3. Official source repository at a pinned revision.
4. Executable fixture against a pinned official binary.
5. Maintainer-confirmed observation with a stored artifact.
6. Community source as a discovery lead only.

A community post cannot promote `UNKNOWN` to a managed capability. A runtime
probe can do so only when its host version, platform, invocation, input, and
golden output are stored.

## 7. Lifecycle States

| State | Behavior |
|---|---|
| `FRESH` | Evidence and installed version remain inside the qualification window. |
| `DUE` | Recheck date passed; already qualified versions remain usable. |
| `STALE` | New version or evidence changed; no expansion of write behavior. |
| `CONFLICTED` | Primary sources or fixtures disagree; affected operations are blocked. |
| `SUPERSEDED` | A newer Pack replaces this one; historical generations still reference it. |
| `RETIRED` | No new generation uses the Pack; historical explanation remains available. |

A floating `latest` selector may discover candidates but can never be recorded
as the Knowledge dependency of a generation.

## 8. Update Workflow

```text
poll allowlisted official sources
  -> detect document, schema, source, release, or binary change
  -> create KnowledgeCandidate
  -> diff facts and affected capabilities
  -> run qualification fixtures
  -> classify failures and known unknowns
  -> open repository pull request
  -> maintainer review
  -> publish immutable Pack
  -> adapters opt in to the new version range
```

Automation may create the candidate and pull request. It may not merge the pull
request or promote capability levels without maintainer review.

## 9. Candidate Report

A Knowledge Candidate must show:

```text
changed upstream sources and digests
old and new host version range
changed discovery roots
changed precedence or merge behavior
affected concepts and operations
fixture results
generations that would become stale
write capabilities that would be blocked or expanded
new known unknowns
required adapter code changes
```

The report is grouped by changed fact and capability, not repeated for every
project using the host.

## 10. Qualification Suite

Before a capability becomes writable, fixtures cover at least:

1. user, project, local, CLI, managed, and system sources together;
2. same-name Instruction, Skill, MCP, Agent, or Hook collisions;
3. unknown fields, comments, and ordering during round trip;
4. source mutation between Plan and activation;
5. a host version just inside and just outside the declared range;
6. missing native effective-state introspection;
7. enterprise constraints that reject a local value;
8. multiple projects sharing one global source;
9. alternate cwd, profile, trust state, and environment flags;
10. secret redaction and proof that observation did not execute content;
11. bootstrap isolation from user-global native sources; and
12. restart activation and rollback of a generated runtime.

## 11. Runtime Resolution

At launch, OMCA resolves:

```text
detected host binary and exact version
+ surface
+ platform
+ invocation context
-> one immutable Knowledge Pack ID and digest
```

No matching Pack means observation-only behavior for unresolved operations. A
more permissive older Pack is never applied optimistically to a new version.

Every generation stores the Pack itself or a durable content-addressed copy so
future debugging does not depend on the current repository head.

## 12. Governance

- Repository maintainers approve Knowledge PRs.
- Evidence URLs must be allowlisted official domains or pinned official source repositories.
- Generated candidates identify automation and collection time.
- Credentials, proprietary configuration content, and personal paths are removed from fixtures.
- A Pack may narrow capability without migration, but expanding write capability requires explicit review evidence.
- Historical Pack removal requires a documented retention decision.
