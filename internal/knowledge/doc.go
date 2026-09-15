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
// Production packs use a combined JSON manifest matching domain.HostKnowledge
// and its schema. docs/knowledge/README.md documents this concrete format
// alongside its conceptual split layout. Default embeds the reviewed manifests;
// explicit directory loading validates candidates with the same parser.
package knowledge
