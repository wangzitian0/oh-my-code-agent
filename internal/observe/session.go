package observe

import (
	"fmt"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// SessionInput is one session-scoped fact (docs/ontology/README.md §2's
// `session` scope: "One invocation or conversation: CLI flags, environment,
// runtime UI") a caller already resolved and wants inventoried alongside
// everything else Observe discovers on disk. Observe never reads the real
// process's argv or environment itself (request.go's Observe doc comment:
// "Observe never reads an environment variable, the real process
// environment, or any ambient state itself; every path it walks comes from
// this struct") — a session-scoped fact is fundamentally not a filesystem
// path this package could discover by walking a directory, so the only way
// to keep that invariant AND still inventory session sources (issue #20's
// explicit acceptance criterion) is for the caller — which DOES have
// legitimate access to the real invocation's argv/env, e.g. a future `omca
// observe` CLI command — to hand the already-resolved fact to Observe
// directly, the same way Detection is a caller-already-performed result
// rather than something Observe re-derives.
type SessionInput struct {
	// Concept is the ontology concept this session-scoped fact overrides or
	// supplies, e.g. conceptMCPServer for Codex's `-c mcp_servers.foo.command=...`
	// or Claude Code's `--mcp-config`. Must be one of this package's known
	// concept IDs (knownConcepts in rules.go); anything else is a caller
	// error.
	Concept string
	// Kind is "flag" or "env" — the two session-scoped source kinds
	// docs/ontology/README.md §2 names ("CLI flags, environment").
	Kind string
	// Name is the flag or environment variable name, e.g. "--mcp-config" or
	// "CODEX_HOME".
	Name string
	// Value is the literal value supplied. Like every other value this
	// package retains, it is NOT redacted here — internal/domain/redact at
	// the output boundary is responsible for that (doc.go's safety-
	// properties point 4), exactly as for file content.
	Value string
}

// validSessionInputKinds is the closed set of SessionInput.Kind values.
var validSessionInputKinds = map[string]bool{"flag": true, "env": true}

// buildSessionObservation constructs one domain.Observation for a
// SessionInput, mirroring walk.go's buildObservation shape (Source, Scope,
// Disposition, EvidenceLevel, digests) but for a caller-supplied fact
// instead of a file this package read from disk itself. EvidenceLevel is
// always EvidenceLevelParsed (E1): a SessionInput's Value is handed to this
// package already fully resolved, so there is no separate "discovered but
// unreadable" (E0) state a session-scoped fact could be in — Value is either
// supplied or the caller simply omits the SessionInput entirely.
func buildSessionObservation(host, hostVersion, surface string, in SessionInput) (domain.Observation, error) {
	if !knownConcepts[in.Concept] {
		return domain.Observation{}, fmt.Errorf("observe: buildSessionObservation: SessionInput.Concept %q is not a concept this package knows (%v)", in.Concept, sortedConceptNames())
	}
	if !validSessionInputKinds[in.Kind] {
		return domain.Observation{}, fmt.Errorf("observe: buildSessionObservation: SessionInput.Kind %q must be %q or %q", in.Kind, "flag", "env")
	}
	if in.Name == "" {
		return domain.Observation{}, fmt.Errorf("observe: buildSessionObservation: SessionInput.Name is required")
	}

	raw := in.Value
	rawDigest, err := domain.CanonicalDigest(raw)
	if err != nil {
		return domain.Observation{}, fmt.Errorf("observe: buildSessionObservation: %w", err)
	}
	opaque := map[string]any{"content": raw}
	parsedDigest, err := domain.CanonicalDigest(opaque)
	if err != nil {
		return domain.Observation{}, fmt.Errorf("observe: buildSessionObservation: %w", err)
	}

	obs := domain.Observation{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Observation",
		Metadata: domain.Metadata{
			ID: fmt.Sprintf("%s:%s:session:%s:%s", host, in.Concept, in.Kind, in.Name),
		},
		Spec: domain.ObservationSpec{
			Host:    domain.ObservationHost{ID: host, Version: hostVersion},
			Surface: surface,
			Concept: in.Concept,
			Source: domain.ObservationSource{
				Kind:   in.Kind,
				Path:   in.Name,
				Digest: rawDigest,
			},
			Scope:              domain.ObservationScope{Kind: "session"},
			Disposition:        domain.DispositionDiscovered,
			EvidenceLevel:      domain.EvidenceLevelParsed,
			RawDigest:          rawDigest,
			ParsedDigest:       parsedDigest,
			OpaqueVendorFields: opaque,
		},
	}
	if err := domain.ValidateObservation(obs); err != nil {
		return domain.Observation{}, fmt.Errorf("observe: buildSessionObservation: built an invalid Observation for %s/%s: %w", in.Kind, in.Name, err)
	}
	return obs, nil
}

// sortedConceptNames returns knownConcepts' keys sorted, for a stable,
// deterministic error message (buildSessionObservation's caller-error path
// above) rather than one whose wording varies across runs with Go's
// randomized map iteration order.
func sortedConceptNames() []string {
	names := make([]string, 0, len(knownConcepts))
	for c := range knownConcepts {
		names = append(names, c)
	}
	sort.Strings(names)
	return names
}
