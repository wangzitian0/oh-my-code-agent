package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
)

// CanonicalDigest returns a stable "sha256:<hex>" digest for v. Object keys
// are sorted before hashing (encoding/json.Marshal sorts map[string]any keys
// lexicographically), so two documents that differ only in the key order
// they were authored or decoded in produce the same digest. Array order is
// preserved because it is semantically meaningful (docs/architecture/README.md
// §5.4, §9: generation and provenance digests must be reproducible from
// committed inputs).
func CanonicalDigest(v any) (string, error) {
	// Round-trip through a generic representation so a typed struct and an
	// equivalent freeform map (e.g. decoded from differently-ordered JSON)
	// canonicalize identically.
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("canonical digest: marshal: %w", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return "", fmt.Errorf("canonical digest: normalize: %w", err)
	}
	canon, err := json.Marshal(generic)
	if err != nil {
		return "", fmt.Errorf("canonical digest: canonicalize: %w", err)
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// canonicalDigestPattern matches exactly the shape CanonicalDigest produces:
// the algorithm prefix and 64 lowercase hex characters, no more, no less.
var canonicalDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// IsCanonicalDigest reports whether s has the "sha256:<64 lowercase hex>"
// shape CanonicalDigest produces. Protocol documents that tie one document
// to another by digest (Report.spec.fingerprint, RepairProposal.spec
// .reportFingerprint, Generation.spec.desiredGraphDigest, and the various
// evidence/knowledge digests in schemas/protocol) should fail closed on a
// malformed reference rather than silently comparing garbage strings.
func IsCanonicalDigest(s string) bool {
	return canonicalDigestPattern.MatchString(s)
}
