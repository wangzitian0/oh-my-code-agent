// Package artifact persists content-addressed manifests, reports, and provenance.
//
// PR-14 (issue #18) added the "current"/"pending" generation pointers and
// the append-only ledger (internal/runtime's current.go/pending.go/
// ledger.go) to internal/runtime rather than here, deliberately following
// PR-09's own precedent of keeping generation-manifest persistence
// (current.go) in internal/runtime instead of moving it to this
// still-unimplemented package. This package remains reserved for a future
// PR that actually needs a persistence layer distinct from
// internal/runtime's own -- there is no such need yet.
package artifact
