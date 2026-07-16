package qualify

import (
	"fmt"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// CaseOutput is everything about one fixture case that must be reproducible
// from its committed input/, invocation.yaml, and expected-effective.json —
// deliberately excluding the live InvocationResult (RunInvocation's stdout/
// exit code), because that depends on whether the real host binary happens
// to be installed on the machine running the test (docs/project/roadmap.md
// M0 exit gate: "all fixture outputs are reproducible from committed
// inputs"; a machine without codex/claude installed must still be able to
// reproduce the same digest as one with them installed).
type CaseOutput struct {
	Manifest     InvocationManifest
	Observations []domain.Observation
	Effective    ExpectedEffectiveDocument
}

// Digest returns a stable content digest of c, suitable for the
// "make fixtures twice produces identical digests" reproducibility proof
// (issue #10 acceptance criteria). It reuses domain.CanonicalDigest, the
// same canonicalization internal/domain already uses for
// Generation.spec.desiredGraphDigest and friends, rather than inventing a
// second hashing convention.
func (c CaseOutput) Digest() (string, error) {
	digest, err := domain.CanonicalDigest(c)
	if err != nil {
		return "", fmt.Errorf("qualify: CaseOutput.Digest: %w", err)
	}
	return digest, nil
}
