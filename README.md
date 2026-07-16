# oh-my-code-agent

`omca` is a local-first control plane for coding-agent runtimes.

It observes native host configuration without trusting it, models the parts
that can be proven through a vendor-neutral ontology, and reconciles an
explicit desired state into an isolated runtime for each directory or Git
worktree.

The primary product outcome is a trusted, explainable report. Configuration
management is deliberately limited to capabilities that have versioned
evidence and executable qualification fixtures.

Host support is plugin-based: adapters for Claude Code and OpenAI Codex ship
first-party behind a frozen adapter contract, and other hosts join through the
same contract or remain at the knowledge/observation tier. One desired state
can give parallel hosts in the same worktree deliberately different loadouts.

## Documentation

- [Project charter](init.md): goals, approved decisions, invariants, and MVP.
- [Documentation map](docs/README.md): source-of-truth boundaries and reading order.
- [Product requirements](docs/product/requirements.md): users, workflows, and defaults.
- [Architecture](docs/architecture/README.md): components, data model, interfaces, and storage.
- [Runtime architecture](docs/architecture/runtime.md): bootstrap isolation, direnv, and immutable generations.
- [Trusted reporting](docs/architecture/reporting.md): evidence, drift, MCP tools, and debugging.
- [Ontology](docs/ontology/README.md): canonical concepts and host mappings.
- [Knowledge lifecycle](docs/knowledge/README.md): versioned third-party facts and upgrades.
- [Architecture decision records](docs/adr/): frozen isolation, ownership, credential, knowledge update, and plugin distribution decisions.
- [Roadmap](docs/project/roadmap.md): gated implementation plan.

## Product Model

```text
Observe -> Model -> Reconcile
```

Global native configuration is always an observable source, but it is not an
implicit parent of an OMCA-managed runtime.

```text
Native configuration -> observed and explained
Desired state        -> explicitly composed
Runtime generation   -> isolated, immutable, restartable
```

The repository is design-first. Implementation starts only after the v1alpha1
schemas and adapter qualification fixtures defined in the roadmap are accepted.
