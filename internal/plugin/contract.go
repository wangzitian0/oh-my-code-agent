package plugin

import "context"

// AdapterID uniquely identifies one host adapter plugin (e.g. "claude-code",
// "codex"). It is the plugin's own identity, distinct from the canonical
// host IDs it declares support for in PluginManifest.Hosts.
type AdapterID string

// InvocationContext is carried by every contract request so no
// implementation reads a floating "latest" fact implicitly
// (docs/architecture/README.md §9: "Every request that can affect output
// carries an explicit Invocation Context, Adapter version, Knowledge digest,
// source fingerprint, and current generation ID.").
type InvocationContext struct {
	WorktreeID   string
	Cwd          string
	Trust        string
	GenerationID string
}

// HostInstance identifies one concrete installation of a host surface an
// adapter detected: the canonical host ID, the surface, the exact native
// version, and platform (docs/architecture/README.md §5.1 "host instance";
// docs/knowledge/README.md §4 metadata: host, surface, platforms).
type HostInstance struct {
	HostID   string
	Surface  string
	Version  string
	Platform string
}

// DetectRequest scopes a Detect call to one invocation.
type DetectRequest struct {
	Invocation InvocationContext
}

// CapabilityEntry records the capability level for each pipeline operation on
// one concept, plus the resulting reconcile mode
// (docs/knowledge/README.md §4 capabilities block: discover, parse,
// normalize, resolve, compile, verify, reconcileMode).
type CapabilityEntry struct {
	Discover      Capability
	Parse         Capability
	Normalize     Capability
	Resolve       Capability
	Compile       Capability
	Verify        Capability
	ReconcileMode ReconcileMode
}

// CapabilityManifest is one HostAdapter's declared capability per concept for
// one HostInstance (docs/knowledge/README.md §4).
type CapabilityManifest struct {
	Host     HostInstance
	Concepts map[string]CapabilityEntry
}

// ObserveRequest scopes one Observe call to a detected HostInstance and the
// filesystem roots to inventory (docs/architecture/README.md §4 "Native
// Observer: inventory known sources without executing discovered assets").
type ObserveRequest struct {
	Invocation InvocationContext
	Host       HostInstance
	Roots      []string
}

// Observation is one physical representation the adapter found, in the shape
// of the Observed Graph (docs/architecture/README.md §5.1: host instance,
// source, scope, representation, trust, ownership, raw/parsed digest,
// provenance, opaque fields).
type Observation struct {
	Source         string
	Scope          string
	Representation string
	Trust          string
	Ownership      string
	Digest         string
	Provenance     string
	Opaque         bool
}

// ObservationSet is everything one Observe call found for one HostInstance.
type ObservationSet struct {
	Host         HostInstance
	Observations []Observation
}

// ResolveRequest asks the adapter to compute host-effective state for one
// concept of one invocation (docs/architecture/README.md §5.2 Effective
// Graph: "project × host × surface × version × concept × invocation
// context").
type ResolveRequest struct {
	Invocation   InvocationContext
	Host         HostInstance
	Concept      string
	Observations ObservationSet
}

// HostEffectiveState is what one host is expected or confirmed to load for
// one invocation: the resolver program, selected source, ignored sources,
// constraints, and evidence (docs/architecture/README.md §5.2).
type HostEffectiveState struct {
	Host           HostInstance
	Concept        string
	SelectedSource string
	IgnoredSources []string
	Constraints    []string
	Evidence       []string
}

// CompileRequest asks the adapter to render native artifacts for one concept
// of one generation (docs/architecture/README.md §5.4 Generation Graph).
type CompileRequest struct {
	Invocation InvocationContext
	Host       HostInstance
	Concept    string
	Desired    HostEffectiveState
}

// Artifact is one generated native file, in the shape of a Generation Graph
// edge: Adapter ID, Knowledge digest, mapping relation, and ownership
// (docs/architecture/README.md §5.4).
type Artifact struct {
	Path            string
	AdapterID       AdapterID
	KnowledgeDigest string
	MappingRelation string
	Ownership       string
	Content         []byte
}

// ArtifactSet is everything one Compile call rendered for one generation.
type ArtifactSet struct {
	Host      HostInstance
	Artifacts []Artifact
}

// VerifyRequest asks the adapter to gather verification evidence for
// artifacts it compiled (docs/architecture/README.md §3 "Verify and Record
// Evidence"; §9 AssuranceEngine.Verify).
type VerifyRequest struct {
	Invocation InvocationContext
	Host       HostInstance
	Concept    string
	Artifacts  ArtifactSet
}

// Evidence is one piece of verification evidence for one artifact.
type Evidence struct {
	ArtifactPath string
	Method       string
	Result       string
}

// EvidenceSet is everything one Verify call gathered.
type EvidenceSet struct {
	Host     HostInstance
	Evidence []Evidence
}

// LaunchRequest asks the adapter to start the host process for one
// generation (docs/architecture/README.md §2: "Runtime Generation -> Coding
// Agent Host").
type LaunchRequest struct {
	Invocation InvocationContext
	Host       HostInstance
	Concept    string
	Artifacts  ArtifactSet
}

// HostAdapter is the frozen v1 plugin contract every host adapter implements
// (docs/architecture/README.md §9). Adapters own physical host semantics;
// they do not compose Profiles or classify cross-host Drift. Every method is
// designed to be equally expressible as a direct in-process call (v1) or as
// the same contract spoken over stdio by an out-of-process plugin (M6).
type HostAdapter interface {
	// ID returns this adapter's identity.
	ID() AdapterID
	// Detect finds installed host instances for one invocation without
	// executing any discovered asset.
	Detect(context.Context, DetectRequest) ([]HostInstance, error)
	// Capabilities reports the capability and reconcile mode this adapter
	// declares for one detected HostInstance, per concept.
	Capabilities(context.Context, HostInstance) (CapabilityManifest, error)
	// Observe inventories native and runtime sources for one HostInstance.
	// Observe must never write to the filesystem and must never execute a
	// discovered asset (docs/knowledge/README.md §10 item 10).
	Observe(context.Context, ObserveRequest) (ObservationSet, error)
	// Resolve computes host-effective state for one concept from an
	// ObservationSet.
	Resolve(context.Context, ResolveRequest) (HostEffectiveState, error)
	// Compile renders native artifacts for one concept of desired state.
	Compile(context.Context, CompileRequest) (ArtifactSet, error)
	// Verify gathers evidence for artifacts this adapter compiled.
	Verify(context.Context, VerifyRequest) (EvidenceSet, error)
	// Launch starts the host process for one generation.
	Launch(context.Context, LaunchRequest) error
}
