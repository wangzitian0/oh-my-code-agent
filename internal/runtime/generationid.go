package runtime

import (
	"fmt"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// generationIDInputs is the deterministic subset of BootstrapRequest that
// actually determines what gets compiled -- explicitly excluding Now and
// Parent, neither of which changes the generated artifact tree's content.
// This is the concrete shape GenerationID digests; its own doc comment and
// generationid_test.go's determinism/sensitivity tests are the proof for
// issue #13 AC "Rebuilding from identical inputs yields the identical
// generation ID (content-addressed)."
// BootstrapRequest.OMCABinaryPath is deliberately excluded here, alongside
// Now and Parent: see request.go's OMCABinaryPath doc comment. It is always
// the worktree's own stable PATH-shim path, never a snapshot of the
// currently-running OMCA binary's own resolved location, so it never
// differs between two calls compiling "the same" generation and has no
// business affecting content-addressed identity -- folding it in here would
// only reintroduce the churn problem the stable-shim-path design exists to
// avoid (TestGenerationID_StableAcrossOMCABinaryPathChange proves this).
type generationIDInputs struct {
	Host            string   `json:"host"`
	HostVersion     string   `json:"hostVersion"`
	Worktree        string   `json:"worktree"`
	BootstrapPolicy string   `json:"bootstrapPolicy"`
	Observations    []string `json:"observations"`
}

// observationFingerprint is the deterministic string folded into
// generationIDInputs.Observations for one Observation: its logical identity
// (Metadata.ID, which already encodes host:concept:path) plus both content
// digests, so the generation ID changes if a source's identity, concept, or
// content changes.
func observationFingerprint(o domain.Observation) string {
	return fmt.Sprintf("%s|%s|%s", o.Metadata.ID, o.Spec.RawDigest, o.Spec.ParsedDigest)
}

// GenerationID computes the content-addressed ID for the bootstrap
// generation req describes, using domain.CanonicalDigest -- the one stable
// digest function this project uses everywhere (internal/domain/digest.go).
// It reads only the fields generationIDInputs names: two BootstrapRequest
// values that differ only in Now, Parent, or OMCABinaryPath produce the
// identical ID (proven by TestGenerationID_DeterministicAcrossCalls and
// TestGenerationID_StableAcrossOMCABinaryPathChange), while changing any
// observation's content, adding or removing an observation, or targeting a
// different host/version/worktree changes it (proven by
// TestGenerationID_Sensitive*). fingerprints is sorted before digesting so
// two Observe results that are semantically identical but happened to be
// built in a different slice order (Observe itself already returns a
// stably sorted slice per observe/doc.go point 2, but this function does
// not depend on a caller upholding that contract) still produce the same
// ID.
func GenerationID(req BootstrapRequest) (string, error) {
	if err := req.validate(); err != nil {
		return "", err
	}

	policyDigest, err := BootstrapPolicyDigest()
	if err != nil {
		return "", fmt.Errorf("runtime: GenerationID: %w", err)
	}

	fingerprints := make([]string, 0, len(req.Observations))
	for _, o := range req.Observations {
		fingerprints = append(fingerprints, observationFingerprint(o))
	}
	sort.Strings(fingerprints)

	digest, err := domain.CanonicalDigest(generationIDInputs{
		Host:            req.Detection.Host,
		HostVersion:     req.Detection.Version,
		Worktree:        req.Worktree.ID,
		BootstrapPolicy: policyDigest,
		Observations:    fingerprints,
	})
	if err != nil {
		return "", fmt.Errorf("runtime: GenerationID: %w", err)
	}
	// "generation:" prefix mirrors internal/context/worktree.go's identical
	// Worktree.ID convention ("worktree:" + digest): a self-describing,
	// stable logical ID rather than a bare digest string.
	return "generation:" + digest, nil
}
