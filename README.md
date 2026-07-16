# oh-my-code-agent

Local-first control plane for coding-agent harnesses.

## Documentation

- [Coding Agent Harness Ontology](docs/ontology/README.md): canonical concepts,
  scopes, composition rules, and host mappings.
- [Initial product design](init.md): product goals, runtime views, drift, and
  delivery milestones.

The ontology is the vocabulary contract. Host adapters must preserve source
provenance and version-specific behavior instead of forcing every product into
one last-write-wins configuration model.
