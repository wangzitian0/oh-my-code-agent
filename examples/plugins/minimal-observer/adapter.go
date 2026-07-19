// Command minimal-observer is the reference example
// docs/plugin/authoring-guide.md walks through: the smallest HostAdapter a
// third party can write, wired to a real host process over the M6
// out-of-process transport (internal/plugin/transport), that still passes
// the full conformance suite (internal/plugin/conformance.Run).
//
// It is entirely self-contained and synthetic. "demo-cli" is not a real
// program; it exists only as the embedded testhost/marker.json this file
// reads at startup. This adapter never probes, execs, or depends on any
// real software installed on the machine it runs on -- copy this directory
// as a starting point and nothing about it assumes your machine looks like
// the author's.
package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
)

// markerJSON is the adapter's own shipped "detectable marker"
// (docs/plugin/authoring-guide.md's "detect" step): a real host adapter
// would look for its host's own installed binary or config directory; this
// synthetic example ships the equivalent of that evidence embedded in its
// own binary instead of depending on anything actually installed, so the
// example detects the exact same "host" on every machine, every time, with
// no setup step for a reader to get wrong.
//
//go:embed testhost/marker.json
var markerJSON []byte

// hostMarker is markerJSON's shape.
type hostMarker struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Concept names this adapter declares capabilities for. conceptFile is the
// one concept it actually supports end to end (Tier 2's honest "observe
// only" shape: see the doc comment on Capabilities). conceptMCP stands in
// for a concept a real host might have that this adapter simply does not
// -- every operation on it reports plugin.ErrUnsupportedOperation, exactly
// as an adapter should for a concept "the host has no corresponding
// operation or concept" for (docs/knowledge/README.md §5).
const (
	conceptFile = "file"
	conceptMCP  = "mcp"
)

// Adapter is the minimal-observer HostAdapter: it detects one synthetic,
// self-declared host, inventories files under whatever roots it is asked to
// observe, and is honest about not supporting anything beyond that -- the
// smallest adapter that is still a genuine, conformant plugin.HostAdapter
// rather than a stub.
//
// Compare internal/plugin/conformance/fake.go's FakeAdapter: that is the
// contract's own in-process reference implementation, and this adapter
// deliberately mirrors its structure (one supported concept, one
// deliberately unsupported concept, read-only Observe) because it is the
// closed-book proof that this shape is correct, not because either file
// copied the other by accident.
type Adapter struct {
	host plugin.HostInstance
}

var _ plugin.HostAdapter = (*Adapter)(nil)

// NewAdapter parses the embedded marker once and returns a ready Adapter.
// It panics on a malformed marker: that would mean this example's own
// embedded fixture is broken, a bug in the example itself, never a
// third-party-adapter runtime condition to recover from gracefully.
func NewAdapter() *Adapter {
	var marker hostMarker
	if err := json.Unmarshal(markerJSON, &marker); err != nil {
		panic(fmt.Sprintf("minimal-observer: embedded testhost/marker.json is malformed: %v", err))
	}
	return &Adapter{
		host: plugin.HostInstance{
			HostID:   "demo-observer-host",
			Surface:  "cli",
			Version:  marker.Version,
			Platform: goruntime.GOOS + "-" + goruntime.GOARCH,
		},
	}
}

// ID returns this adapter's own identity (distinct from the host ID it
// declares support for -- contract.go's AdapterID doc comment).
func (a *Adapter) ID() plugin.AdapterID { return "minimal-observer" }

// Detect reports the one synthetic host this adapter always ships evidence
// for. A real adapter would instead look for its host's actual installed
// binary or config directory (still without executing anything) and return
// zero instances when it is genuinely absent; this example always finds its
// one embedded marker, which is the whole point of shipping it embedded
// rather than reading it from somewhere on disk.
func (a *Adapter) Detect(_ context.Context, _ plugin.DetectRequest) ([]plugin.HostInstance, error) {
	return []plugin.HostInstance{a.host}, nil
}

func (a *Adapter) isDetected(host plugin.HostInstance) bool {
	return host == a.host
}

// capabilities is this adapter's fixed, Tier-2-appropriate capability
// declaration (docs/knowledge/README.md §5 Capability Vocabulary; §12
// Governance): conceptFile is discoverable and can be resolved/compiled into
// a report-only artifact, but ReconcileMode OBSERVED means OMCA reports
// this concept and never writes it to any real host -- the defining trait of
// an observation-tier adapter (as opposed to a qualified, write-capable
// Tier 1 one). conceptMCP is UNSUPPORTED end to end: this synthetic host has
// no such concept, and every operation on it says so honestly rather than
// pretending.
func (a *Adapter) capabilities() map[string]plugin.CapabilityEntry {
	return map[string]plugin.CapabilityEntry{
		conceptFile: {
			Discover:      plugin.CapabilityExact,
			Parse:         plugin.CapabilityUnsupported,
			Normalize:     plugin.CapabilityUnsupported,
			Resolve:       plugin.CapabilityPartial,
			Compile:       plugin.CapabilityPartial,
			Verify:        plugin.CapabilityPartial,
			ReconcileMode: plugin.ReconcileObserved,
		},
		conceptMCP: {
			Discover:      plugin.CapabilityUnsupported,
			Parse:         plugin.CapabilityUnsupported,
			Normalize:     plugin.CapabilityUnsupported,
			Resolve:       plugin.CapabilityUnsupported,
			Compile:       plugin.CapabilityUnsupported,
			Verify:        plugin.CapabilityUnsupported,
			ReconcileMode: plugin.ReconcileObserved,
		},
	}
}

// Capabilities reports the fixed capability manifest above for the detected
// host, or plugin.ErrNotDetected for any other HostInstance
// (docs/plugin/authoring-guide.md; internal/plugin/contract.go's own
// ErrNotDetected doc comment).
func (a *Adapter) Capabilities(_ context.Context, host plugin.HostInstance) (plugin.CapabilityManifest, error) {
	if !a.isDetected(host) {
		return plugin.CapabilityManifest{}, plugin.ErrNotDetected
	}
	return plugin.CapabilityManifest{Host: host, Concepts: a.capabilities()}, nil
}

// Observe inventories every regular file under the given roots, read-only:
// it opens and hashes file content but never writes, creates, removes, or
// executes anything (docs/knowledge/README.md §10 item 10; the contract's
// own Observe doc comment). This is the method a real adapter spends most of
// its Tier 2 life in -- Discover/Observe is exactly what an observation-only
// adapter is for.
func (a *Adapter) Observe(_ context.Context, req plugin.ObserveRequest) (plugin.ObservationSet, error) {
	if !a.isDetected(req.Host) {
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
				Provenance:     "minimal-observer:Observe",
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

// unsupported reports whether concept is not conceptFile at all -- the one
// concept this adapter's capability manifest ever rates above UNSUPPORTED.
func unsupportedConcept(concept string) bool {
	return concept != conceptFile
}

// Resolve computes a trivial HostEffectiveState for conceptFile, or returns
// plugin.ErrUnsupportedOperation for any other concept (docs/plugin/
// authoring-guide.md's error-taxonomy walkthrough).
func (a *Adapter) Resolve(_ context.Context, req plugin.ResolveRequest) (plugin.HostEffectiveState, error) {
	if !a.isDetected(req.Host) {
		return plugin.HostEffectiveState{}, plugin.ErrNotDetected
	}
	if unsupportedConcept(req.Concept) {
		return plugin.HostEffectiveState{}, plugin.ErrUnsupportedOperation
	}
	selected := "none"
	if len(req.Observations.Observations) > 0 {
		selected = req.Observations.Observations[0].Source
	}
	return plugin.HostEffectiveState{
		Host:           req.Host,
		Concept:        req.Concept,
		SelectedSource: selected,
		Evidence:       []string{"minimal-observer:Resolve"},
	}, nil
}

// Compile renders one trivial, report-only Artifact for conceptFile. Its
// Ownership is "observed" (docs/architecture/README.md §10): OMCA reports
// this artifact but this adapter's ReconcileMode (OBSERVED) means the core
// never persists it to a real host. Any other concept returns
// plugin.ErrUnsupportedOperation.
func (a *Adapter) Compile(_ context.Context, req plugin.CompileRequest) (plugin.ArtifactSet, error) {
	if !a.isDetected(req.Host) {
		return plugin.ArtifactSet{}, plugin.ErrNotDetected
	}
	if unsupportedConcept(req.Concept) {
		return plugin.ArtifactSet{}, plugin.ErrUnsupportedOperation
	}
	artifact := plugin.Artifact{
		Path:            fmt.Sprintf("observed/%s/%s.json", req.Host.HostID, req.Concept),
		AdapterID:       a.ID(),
		KnowledgeDigest: "sha256:minimal-observer-no-knowledge-pack",
		MappingRelation: "1:1",
		Ownership:       "observed",
		Content:         []byte(fmt.Sprintf(`{"concept":%q,"source":%q}`, req.Concept, req.Desired.SelectedSource)),
	}
	return plugin.ArtifactSet{Host: req.Host, Artifacts: []plugin.Artifact{artifact}}, nil
}

// Verify reports trivial "ok" evidence for every artifact it is handed.
// Any concept other than conceptFile returns plugin.ErrUnsupportedOperation.
func (a *Adapter) Verify(_ context.Context, req plugin.VerifyRequest) (plugin.EvidenceSet, error) {
	if !a.isDetected(req.Host) {
		return plugin.EvidenceSet{}, plugin.ErrNotDetected
	}
	if unsupportedConcept(req.Concept) {
		return plugin.EvidenceSet{}, plugin.ErrUnsupportedOperation
	}
	evidence := make([]plugin.Evidence, 0, len(req.Artifacts.Artifacts))
	for _, artifact := range req.Artifacts.Artifacts {
		evidence = append(evidence, plugin.Evidence{
			ArtifactPath: artifact.Path,
			Method:       "digest-compare",
			Result:       "ok",
		})
	}
	return plugin.EvidenceSet{Host: req.Host, Evidence: evidence}, nil
}

// Launch reports success without starting any real process -- there is no
// real "demo-cli" host to start. docs/plugin/authoring-guide.md's "known
// gaps" section explains why this example cannot honestly report
// plugin.ErrUnsupportedOperation for every concept's Launch the way it does
// for Resolve/Compile/Verify: internal/plugin/conformance.Run's
// runResolveCompileVerifyLaunch sub-check requires at least one concept
// whose full Resolve/Compile/Verify/Launch chain succeeds. Any concept
// other than conceptFile still returns plugin.ErrUnsupportedOperation here,
// for whatever it is worth given conformance.Run never actually calls
// Launch against an unsupported concept today.
func (a *Adapter) Launch(_ context.Context, req plugin.LaunchRequest) error {
	if !a.isDetected(req.Host) {
		return plugin.ErrNotDetected
	}
	if unsupportedConcept(req.Concept) {
		return plugin.ErrUnsupportedOperation
	}
	return nil
}
