// Package knowledge resolves immutable host Knowledge Packs and update
// candidates (docs/knowledge/README.md).
//
// PR-07 (issue #11) implements loading and version-range resolution:
//
//   - pack.go: LoadPack reads one Knowledge Pack document, validates it with
//     domain.ValidateHostKnowledge, rejects a floating (e.g. "latest")
//     version reference, and content-addresses it with
//     domain.CanonicalDigest.
//   - semver.go: a minimal MAJOR.MINOR.PATCH comparator sufficient for
//     docs/knowledge/README.md §4's versionRange syntax
//     (space-separated ">=X.Y.Z"/"<X.Y.Z" comparator terms, ANDed together).
//   - repository.go: Repository loads every Pack under a directory tree and
//     Resolve matches one detected host+surface+exact-version to at most
//     one Pack — or an honest "no qualified pack" Resolution when none (or
//     more than one, ambiguously) matches, never an optimistic guess
//     (docs/knowledge/README.md §11: "No matching Pack means
//     observation-only behavior for unresolved operations. A more
//     permissive older Pack is never applied optimistically to a new
//     version.").
//
// # A known doc/code discrepancy this package resolves one way, explicitly
//
// docs/knowledge/README.md §3 shows a Knowledge Pack directory holding six
// split files (manifest.yaml, capabilities.yaml, discovery.yaml,
// precedence.yaml, evidence.yaml, migrations.yaml). The existing
// domain.HostKnowledge Go type (internal/domain/hostknowledge.go) and the
// existing schemas/protocol/hostknowledge.v1alpha1.schema.json both model
// one *combined* JSON document instead — metadata, evidence, capabilities,
// precedencePrograms, and knownUnknowns together, with no split-file merge
// logic anywhere in the codebase. This package follows the code and schema
// (a single combined document per pack, loaded from one manifest.json per
// pack directory) rather than the docs' split-file layout, because that is
// what the pre-existing, already-merged HostKnowledge type and schema
// actually implement. See the PR-07 pull request description for the fuller
// discussion; this is a known, unresolved tension worth a follow-up, not a
// silently invented reconciliation.
//
// A second, smaller doc/code tension: docs/knowledge/README.md §4's worked
// example is YAML. domain.HostKnowledge, however, carries only `json`
// struct tags (no `yaml` tags) — and gopkg.in/yaml.v3's default, tag-free
// field matching lowercases each Go field name for comparison (VersionRange
// -> "versionrange"), which does *not* match a camelCase YAML key like
// "versionRange". Parsing the docs' own worked example as YAML directly into
// domain.HostKnowledge would therefore silently leave multi-word fields
// empty. This package loads Knowledge Pack files as JSON instead, matching
// domain.HostKnowledge's actual tags and the existing
// internal/domain/testdata/hostknowledge-valid.json convention.
package knowledge
