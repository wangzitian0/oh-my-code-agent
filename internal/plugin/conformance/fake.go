package conformance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
)

// Concept names FakeAdapter declares capabilities for. ConceptOK is the
// ordinary path; ConceptUnsupported and ConceptBlocked exist specifically so
// Run can prove the ErrUnsupportedOperation and ErrCapabilityDenied error
// paths against a concrete, correctly-behaved implementation
// (docs/knowledge/README.md §5 capability vocabulary; §5 reconcile modes).
const (
	ConceptOK          = "skill"
	ConceptUnsupported = "legacy-macro"
	ConceptBlocked     = "enterprise-policy"
)

// Compile-time proof that FakeAdapter implements the frozen contract.
var _ plugin.HostAdapter = (*FakeAdapter)(nil)

// FakeAdapter is a well-behaved, in-memory HostAdapter. It detects one fixed
// HostInstance, declares capabilities for three concepts covering the
// ordinary path plus both taxonomy error paths, and its Observe method reads
// the given roots read-only — no write, no exec — which is what makes it a
// true positive when run through Run's zero-write/zero-exec proof.
//
// It is exported so PR-06's fixture harness and future real adapters'
// own tests can use it directly (e.g. as a stand-in dependency, or as a
// reference for "what does a correct Observe look like").
type FakeAdapter struct {
	id   plugin.AdapterID
	host plugin.HostInstance
}

// NewFakeAdapter returns a FakeAdapter that always detects one fixed
// HostInstance.
func NewFakeAdapter() *FakeAdapter {
	return &FakeAdapter{
		id: "conformance-fake",
		host: plugin.HostInstance{
			HostID:   "codex",
			Surface:  "cli",
			Version:  "0.144.0",
			Platform: "darwin-arm64",
		},
	}
}

// ID returns the fake's adapter identity.
func (f *FakeAdapter) ID() plugin.AdapterID { return f.id }

// Detect always returns the fake's one fixed HostInstance.
func (f *FakeAdapter) Detect(_ context.Context, _ plugin.DetectRequest) ([]plugin.HostInstance, error) {
	return []plugin.HostInstance{f.host}, nil
}

func (f *FakeAdapter) isDetected(host plugin.HostInstance) bool {
	return host == f.host
}

func (f *FakeAdapter) capabilities() map[string]plugin.CapabilityEntry {
	return map[string]plugin.CapabilityEntry{
		ConceptOK: {
			Discover: plugin.CapabilityExact, Parse: plugin.CapabilityExact,
			Normalize: plugin.CapabilityExact, Resolve: plugin.CapabilityExact,
			Compile: plugin.CapabilityPartial, Verify: plugin.CapabilityPartial,
			ReconcileMode: plugin.ReconcilePatched,
		},
		ConceptUnsupported: {
			Discover: plugin.CapabilityUnsupported, Parse: plugin.CapabilityUnsupported,
			Normalize: plugin.CapabilityUnsupported, Resolve: plugin.CapabilityUnsupported,
			Compile: plugin.CapabilityUnsupported, Verify: plugin.CapabilityUnsupported,
			ReconcileMode: plugin.ReconcileObserved,
		},
		ConceptBlocked: {
			Discover: plugin.CapabilityExact, Parse: plugin.CapabilityExact,
			Normalize: plugin.CapabilityExact, Resolve: plugin.CapabilityExact,
			Compile: plugin.CapabilityExact, Verify: plugin.CapabilityExact,
			ReconcileMode: plugin.ReconcileBlocked,
		},
	}
}

// Capabilities reports the fake's fixed per-concept capability entries for
// the detected host, or ErrNotDetected for any other HostInstance.
func (f *FakeAdapter) Capabilities(_ context.Context, host plugin.HostInstance) (plugin.CapabilityManifest, error) {
	if !f.isDetected(host) {
		return plugin.CapabilityManifest{}, plugin.ErrNotDetected
	}
	return plugin.CapabilityManifest{Host: host, Concepts: f.capabilities()}, nil
}

// Observe inventories the given roots read-only: it opens and reads file
// content to compute a digest, but never writes, creates, removes, or
// executes anything. This is the property Run's zero-write/zero-exec proof
// depends on.
func (f *FakeAdapter) Observe(_ context.Context, req plugin.ObserveRequest) (plugin.ObservationSet, error) {
	if !f.isDetected(req.Host) {
		return plugin.ObservationSet{}, plugin.ErrNotDetected
	}

	var observations []plugin.Observation
	for _, root := range req.Roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path) // read-only: no write, no exec
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			sum := sha256.Sum256(data)
			observations = append(observations, plugin.Observation{
				Source:         path,
				Scope:          "user",
				Representation: "file",
				Trust:          "observed",
				Ownership:      "external",
				Digest:         "sha256:" + hex.EncodeToString(sum[:]),
				Provenance:     "conformance-fake:Observe",
				Opaque:         true,
			})
			return nil
		})
		if err != nil {
			return plugin.ObservationSet{}, fmt.Errorf("observe %s: %w", root, err)
		}
	}
	return plugin.ObservationSet{Host: req.Host, Observations: observations}, nil
}

// Resolve computes a trivial HostEffectiveState for a supported concept, or
// returns ErrUnsupportedOperation when the concept's Resolve capability is
// CapabilityUnsupported.
func (f *FakeAdapter) Resolve(_ context.Context, req plugin.ResolveRequest) (plugin.HostEffectiveState, error) {
	if !f.isDetected(req.Host) {
		return plugin.HostEffectiveState{}, plugin.ErrNotDetected
	}
	entry, ok := f.capabilities()[req.Concept]
	if !ok || entry.Resolve == plugin.CapabilityUnsupported {
		return plugin.HostEffectiveState{}, plugin.ErrUnsupportedOperation
	}
	return plugin.HostEffectiveState{
		Host:           req.Host,
		Concept:        req.Concept,
		SelectedSource: "fixture:" + req.Concept,
		Evidence:       []string{"conformance-fake:Resolve"},
	}, nil
}

// Compile renders one trivial artifact for a supported, non-blocked concept.
// It returns ErrUnsupportedOperation when the concept's Compile capability is
// CapabilityUnsupported, and ErrCapabilityDenied when the concept's reconcile
// mode is ReconcileBlocked.
func (f *FakeAdapter) Compile(_ context.Context, req plugin.CompileRequest) (plugin.ArtifactSet, error) {
	if !f.isDetected(req.Host) {
		return plugin.ArtifactSet{}, plugin.ErrNotDetected
	}
	entry, ok := f.capabilities()[req.Concept]
	if !ok || entry.Compile == plugin.CapabilityUnsupported {
		return plugin.ArtifactSet{}, plugin.ErrUnsupportedOperation
	}
	if entry.ReconcileMode == plugin.ReconcileBlocked {
		return plugin.ArtifactSet{}, plugin.ErrCapabilityDenied
	}
	artifact := plugin.Artifact{
		Path:            fmt.Sprintf("generated/%s/%s.json", req.Host.HostID, req.Concept),
		AdapterID:       f.id,
		KnowledgeDigest: "sha256:fake-knowledge-digest",
		MappingRelation: "1:1",
		Ownership:       "managed",
		Content:         []byte(fmt.Sprintf(`{"concept":%q,"source":%q}`, req.Concept, req.Desired.SelectedSource)),
	}
	return plugin.ArtifactSet{Host: req.Host, Artifacts: []plugin.Artifact{artifact}}, nil
}

// Verify reports trivial "ok" evidence for every artifact it is given.
func (f *FakeAdapter) Verify(_ context.Context, req plugin.VerifyRequest) (plugin.EvidenceSet, error) {
	if !f.isDetected(req.Host) {
		return plugin.EvidenceSet{}, plugin.ErrNotDetected
	}
	evidence := make([]plugin.Evidence, 0, len(req.Artifacts.Artifacts))
	for _, artifact := range req.Artifacts.Artifacts {
		evidence = append(evidence, plugin.Evidence{
			ArtifactPath: artifact.Path,
			Method:       "static-resolver",
			Result:       "ok",
		})
	}
	return plugin.EvidenceSet{Host: req.Host, Evidence: evidence}, nil
}

// Launch reports success without starting any real process (this is a
// fake). It returns ErrCapabilityDenied when the requested concept's
// reconcile mode is ReconcileBlocked, and ErrNotDetected for an undetected
// host.
func (f *FakeAdapter) Launch(_ context.Context, req plugin.LaunchRequest) error {
	if !f.isDetected(req.Host) {
		return plugin.ErrNotDetected
	}
	if entry, ok := f.capabilities()[req.Concept]; ok && entry.ReconcileMode == plugin.ReconcileBlocked {
		return plugin.ErrCapabilityDenied
	}
	return nil
}
