# Documentation Map

This directory separates product intent, canonical vocabulary, changing host
facts, implementation architecture, and delivery tracking. A document must not
silently become authoritative outside its declared boundary.

## Source-of-truth Order

When documents appear to conflict, resolve them by subject rather than by a
single global priority:

| Subject | Authoritative document |
|---|---|
| Product goals, approved decisions, and invariants | [`init.md`](../init.md) |
| User workflows, defaults, and acceptance requirements | [`product/requirements.md`](product/requirements.md) |
| Canonical concepts and cross-host mappings | [`ontology/README.md`](ontology/README.md) |
| Third-party evidence, versioning, and update policy | [`knowledge/README.md`](knowledge/README.md) |
| Component, data, interface, and storage architecture | [`architecture/README.md`](architecture/README.md) |
| Bootstrap, isolation, generations, and restart behavior | [`architecture/runtime.md`](architecture/runtime.md) |
| Drift, evidence, report UX, MCP queries, and debugging | [`architecture/reporting.md`](architecture/reporting.md) |
| Frozen cross-cutting decisions (isolation, ownership, credentials, knowledge update, plugin distribution) | [`adr/`](adr/) |
| Implementation order and milestone exit gates | [`project/roadmap.md`](project/roadmap.md) |

The ontology defines meaning. Knowledge Packs define versioned host facts.
Product requirements define desired behavior. Architecture explains how the
implementation satisfies those contracts. The roadmap does not redefine any of
them.

## Reading Order

1. [Project charter](../init.md)
2. [Product requirements](product/requirements.md)
3. [Architecture overview](architecture/README.md)
4. [Runtime architecture](architecture/runtime.md)
5. [Trusted reporting](architecture/reporting.md)
6. [Ontology](ontology/README.md)
7. [Knowledge lifecycle](knowledge/README.md)
8. [Architecture decision records](adr/)
9. [Roadmap](project/roadmap.md)

## Change Rules

- Product behavior changes update the charter or Product Requirements first.
- Canonical concept changes update the Ontology before adapter code.
- New or changed host behavior enters through a Knowledge Candidate and fixture,
  not an unreviewed edit to adapter code.
- Runtime behavior changes update the relevant architecture document and its
  acceptance fixtures.
- A milestone may link to requirements but must not duplicate them.
- Public documentation, schemas, code identifiers, configuration keys, and CLI
  output are English unless localization is the explicit subject.
- Generated reports and Knowledge Packs record their schema and source document
  versions.
- A frozen cross-cutting decision (isolation, ownership, credentials,
  knowledge update, plugin distribution) lives in [`docs/adr/`](adr/) as one
  ADR per decision. An accepted ADR changes by a new ADR superseding it, not
  by an in-place edit.

## Status Vocabulary

```text
draft       under active design; not an implementation contract
accepted    reviewed contract for implementation
qualified   behavior proven for a host version by executable fixtures
deprecated  retained for migration but not new use
retired     historical explanation only
```

Each future design document should declare one of these statuses near its title.
