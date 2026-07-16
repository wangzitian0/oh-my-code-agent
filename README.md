# oh-my-code-agent

Local-first control plane for observing, modeling, and safely reconciling
coding-agent harnesses.

## Documentation

- [Coding Agent Harness Ontology](docs/ontology/README.md): canonical concepts,
  scopes, composition rules, and host mappings.
- [End-to-end product design](init.md): control-plane architecture, versioned
  host knowledge, configuration contracts, drift UX, interfaces, assurance,
  and delivery milestones.

The ontology is the vocabulary contract. Versioned Knowledge Packs describe
third-party behavior, while host adapters preserve provenance and capability
limits instead of forcing every product into one last-write-wins model.
