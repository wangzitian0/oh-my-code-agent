package report

import (
	"fmt"
	"sort"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/assurance"
	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/contextcost"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/drift"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// HostInput is one host's already-detected, already-observed input to
// [Build]. Detection and Observations are gathered by the caller (cmd/omca,
// matching every other command's own detect-then-observe pattern, e.g.
// cmd/omca/env.go's runEnv) — this package never shells out to a host
// binary or walks a native home itself, matching internal/effective and
// internal/resolve's own "explicit inputs, nothing implicit" discipline.
type HostInput struct {
	Detection    hostcontext.HostDetection
	Observations []domain.Observation
}

// BuildRequest is everything [Build] needs, explicit and caller-supplied.
type BuildRequest struct {
	Worktree         hostcontext.Worktree
	WorktreeStateDir string
	Hosts            []HostInput
	Repository       knowledge.Repository

	// Profiles/Activation/Exceptions are the Desired Graph's inputs
	// (internal/profiles.Compose's output). A caller with no composed
	// desired state yet (e.g. a worktree that has never run
	// `omca activate`) may leave these zero-valued; resolve.Resolve degrades
	// to an empty ResolvedState for empty inputs, so Build still succeeds —
	// a Desired Graph is not required for BuildDriftSignals' own
	// Effective-Graph-only signals (adapter.go) or for the Knowledge/
	// context-cost/duplicate-capability sections.
	Profiles   []domain.Profile
	Activation domain.Activation
	Exceptions []domain.Exception

	Now time.Time
}

// Build computes one immutable [Artifact] for req: per installed host, it
// resolves the Knowledge Pack, computes the Effective Graph
// (internal/effective) and the Desired Graph (internal/resolve), adapts the
// Effective Graph into drift signals ([BuildDriftSignals]), estimates
// context cost from the host's current generation (when one exists,
// honestly nil otherwise), and aggregates duplicate capabilities. Every
// host's signals are classified and grouped together (drift.ClassifyAll,
// drift.Group), so one root cause spanning several hosts still collapses
// into one ActionCard (docs/architecture/reporting.md §7) rather than one
// card per host.
//
// A host with Detection.Installed == false is skipped entirely (nothing was
// observed for it to build a graph from) — this mirrors cmd/omca/env.go's
// runEnv, which likewise skips generation compilation for an uninstalled
// host.
func Build(req BuildRequest) (Artifact, error) {
	if req.Worktree.ID == "" {
		return Artifact{}, fmt.Errorf("report: Build: Worktree.ID is required")
	}
	if req.Now.IsZero() {
		return Artifact{}, fmt.Errorf("report: Build: Now is required")
	}

	hosts := append([]HostInput(nil), req.Hosts...)
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Detection.Host < hosts[j].Detection.Host })

	var (
		allSignals      []drift.Signal
		hostSummaries   []HostSummary
		duplicates      []effective.DuplicateCapability
		planes          domain.ReportPlanes
		knowledgeStatus = map[string]domain.KnowledgeStatus{}
		debug           = map[string]HostDebug{}
	)

	for _, hi := range hosts {
		host := hi.Detection.Host
		if !hi.Detection.Installed {
			continue
		}

		resolution := req.Repository.Resolve(host, hi.Detection.Surface, hi.Detection.Version)
		hostKnowledge := HostKnowledge{Qualified: resolution.Qualified, PackID: resolution.PackID, Reason: resolution.Reason}
		hk := domain.HostKnowledge{}
		if status, ok := resolution.Status(); ok {
			hostKnowledge.Status = status
			knowledgeStatus[host] = status
		}
		if resolution.Qualified {
			if pack, ok := findPack(req.Repository, resolution.PackID); ok {
				hk = pack.Knowledge
			}
		}

		graph, err := effective.ComputeEffectiveGraph(host, hi.Detection.Version, hi.Observations, hk, effective.Options{}, nil)
		if err != nil {
			return Artifact{}, fmt.Errorf("report: Build: %s: computing effective graph: %w", host, err)
		}
		// Every effective value in the report carries its evidence level
		// (issue #26's own acceptance criterion), and that level must never
		// exceed what internal/assurance's committed evidence-ceiling table
		// (docs/architecture/evidence-ceiling.md) proves this host+concept
		// can honestly back — VerifyGraph re-derives it in place before
		// anything downstream (drift signals, Debug, Evidence) reads it, so
		// there is no code path in this function that can see an
		// unverified EvidenceLevel.
		graph = assurance.VerifyGraph(host, graph, hk)

		rs, err := resolve.Resolve(req.Profiles, req.Activation, req.Exceptions, host, req.Now)
		if err != nil {
			return Artifact{}, fmt.Errorf("report: Build: %s: resolving desired state: %w", host, err)
		}
		desired := effective.DesiredGraph{ResolvedState: rs}

		graphs := effective.Graphs{
			Host:        host,
			HostVersion: hi.Detection.Version,
			Observed:    effective.ObservedGraph{Observations: hi.Observations},
			Effective:   graph,
			Desired:     desired,
		}

		candidates, err := effective.ExtractCandidates(hi.Observations)
		if err != nil {
			return Artifact{}, fmt.Errorf("report: Build: %s: extracting candidates: %w", host, err)
		}

		allSignals = append(allSignals, BuildDriftSignals(req.Worktree.ID, graphs, resolution, req.Repository)...)
		duplicates = append(duplicates, graph.DuplicateCapabilities...)

		currentSources, pendingSources, currentGenID, pendingGenID, costEntry := generationSources(req.WorktreeStateDir, host, hi.Detection.Version)
		currentCount, pendingCount := len(currentSources), len(pendingSources)

		// Evidence turns graph's (now-verified) EvidenceLevel/Guarantee
		// fields into standalone, separately queryable domain.Evidence
		// documents (issue #26's Goal line: "attach honest evidence to
		// every conclusion"), plus — when internal/context's --version
		// probe succeeded — the one E3 (HOST_REPORTED) record this
		// package can honestly produce today (docs/architecture/
		// evidence-ceiling.md's "host" rows).
		hostEvidence := assurance.BuildEvidence(host, graph, hk, req.Now)
		if v, ok := assurance.HostVersionEvidence(hi.Detection, req.Now); ok {
			hostEvidence = append(hostEvidence, v)
		}

		debug[host] = HostDebug{
			Graph:               graph,
			Candidates:          candidates,
			Observations:        hi.Observations,
			Desired:             rs,
			KnowledgeEvidence:   hk.Evidence,
			Evidence:            hostEvidence,
			CurrentSources:      currentSources,
			PendingSources:      pendingSources,
			CurrentGenerationID: currentGenID,
			PendingGenerationID: pendingGenID,
		}

		hostSummaries = append(hostSummaries, HostSummary{
			Host:        host,
			HostVersion: hi.Detection.Version,
			Knowledge:   hostKnowledge,
			ContextCost: costEntry,
			Planes: HostPlaneCounts{
				Observed:  len(hi.Observations),
				Effective: len(graph.Entries),
				Conflicts: len(graph.Conflicts),
				Desired:   len(rs.Assets),
				Current:   currentCount,
				Pending:   pendingCount,
			},
		})

		planes.Native += len(hi.Observations)
		planes.Observed += len(hi.Observations)
		planes.Desired += len(rs.Assets)
		planes.HostEffective += len(graph.Entries)
		planes.Current += currentCount
		planes.Pending += pendingCount
	}

	assertions, err := drift.ClassifyAll(allSignals, req.Exceptions, req.Now)
	if err != nil {
		return Artifact{}, fmt.Errorf("report: Build: classifying drift: %w", err)
	}
	cards := drift.Group(assertions)
	driftCards, err := buildDriftCards(cards)
	if err != nil {
		return Artifact{}, fmt.Errorf("report: Build: %w", err)
	}

	flatDrift := make([]domain.DriftAssertion, 0, len(assertions))
	for _, a := range assertions {
		flatDrift = append(flatDrift, a.DriftAssertion)
	}

	artifact := Artifact{
		Report: domain.Report{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       "Report",
			Metadata: domain.ReportMetadata{
				ID:          fmt.Sprintf("report:%s:%s", req.Worktree.ID, req.Now.UTC().Format(time.RFC3339Nano)),
				Worktree:    req.Worktree.ID,
				GeneratedAt: req.Now.UTC().Format(time.RFC3339),
			},
			Spec: domain.ReportSpec{
				Planes:          planes,
				Drift:           flatDrift,
				KnowledgeStatus: knowledgeStatus,
			},
		},
		ActionCards:           driftCards,
		Hosts:                 hostSummaries,
		DuplicateCapabilities: buildDuplicateCapabilityEntries(duplicates),
		Debug:                 debug,
	}

	fingerprint, err := computeFingerprint(artifact)
	if err != nil {
		return Artifact{}, fmt.Errorf("report: Build: %w", err)
	}
	artifact.Report.Spec.Fingerprint = fingerprint

	return artifact, nil
}

// findPack returns the loaded Pack named packID from repo, if any. Resolution
// already exposes an in-process pointer to the same Pack via its unexported
// field (used by CapabilityFor/Status), but that field never leaves the
// knowledge package — this looks the Pack back up by ID from Repository's
// own exported Packs() accessor instead of reaching for an unexported field.
func findPack(repo knowledge.Repository, packID string) (knowledge.Pack, bool) {
	for _, p := range repo.Packs() {
		if p.Knowledge.Metadata.ID == packID {
			return p, true
		}
	}
	return knowledge.Pack{}, false
}

// generationSources reads host's current/pending generation manifests (when
// present) under worktreeStateDir, returning each one's Sources list, the
// generation's own Metadata.ID, and a context-cost estimate derived from the
// current generation. A missing current/pending generation is not an error —
// a worktree that has never run `omca env`/`omca activate` for this host
// simply has nothing to report or estimate yet, so the returned slices/IDs
// are zero-valued and costEntry is nil (never a synthesized cost for a
// generation that does not exist — reporting.md §8's "unknown ... reported
// as unknown, not a fake token count" applies here just as much as to the
// estimate's own Method/Confidence fields).
//
// Both returned Sources lists are filtered to host: a Generation can share
// multiple hosts' artifact trees, so Spec.Sources is a flat list across all
// of them (domain.GenerationSourceEntry's doc comment), and host's
// current/pending pointer can legitimately reference a multi-host
// generation dir. Without filtering, CURRENT/PENDING plane counts and
// compare/diff output would incorrectly include another host's sources —
// the same host-scoping bug class internal/runtime.DiffProposedChanges
// already guards against (its `s.Host != host` filter), applied here too.
// currentGenID/pendingGenID are NOT filtered by host (a Generation has one
// Metadata.ID regardless of how many hosts' artifact trees it carries) —
// they identify the generation manifest itself, matching runtime.
// ReadGenerationManifest's own return.
func generationSources(worktreeStateDir, host, hostVersion string) (currentSources, pendingSources []domain.GenerationSourceEntry, currentGenID, pendingGenID string, costEntry *ContextCostEntry) {
	if worktreeStateDir == "" {
		return nil, nil, "", "", nil
	}
	if dir, err := runtime.CurrentGenerationDir(worktreeStateDir, host); err == nil {
		if gen, err := runtime.ReadGenerationManifest(dir); err == nil {
			currentSources = sourcesForHost(gen.Spec.Sources, host)
			currentGenID = gen.Metadata.ID
			excludedMCP, excludedSkills := contextcost.CountUserExclusions(gen)
			cost := contextcost.EstimateContextCost(excludedMCP, excludedSkills)
			costEntry = &ContextCostEntry{ContextCostEstimate: cost, HostVersion: hostVersion}
		}
	}
	if dir, err := runtime.PendingGenerationDir(worktreeStateDir, host); err == nil {
		if gen, err := runtime.ReadGenerationManifest(dir); err == nil {
			pendingSources = sourcesForHost(gen.Spec.Sources, host)
			pendingGenID = gen.Metadata.ID
		}
	}
	return currentSources, pendingSources, currentGenID, pendingGenID, costEntry
}

// sourcesForHost filters a Generation's flat, multi-host Spec.Sources list
// down to the entries stamped with host, mirroring
// internal/runtime.DiffProposedChanges's identical `s.Host != host` filter.
func sourcesForHost(sources []domain.GenerationSourceEntry, host string) []domain.GenerationSourceEntry {
	var out []domain.GenerationSourceEntry
	for _, s := range sources {
		if s.Host == host {
			out = append(out, s)
		}
	}
	return out
}

// buildDuplicateCapabilityEntries attaches a ContextCostAttribution to every
// effective.DuplicateCapability (round-2 audit: "duplicate-capability
// section with context-cost attribution").
func buildDuplicateCapabilityEntries(duplicates []effective.DuplicateCapability) []DuplicateCapabilityEntry {
	if len(duplicates) == 0 {
		return nil
	}
	out := make([]DuplicateCapabilityEntry, 0, len(duplicates))
	for _, d := range duplicates {
		out = append(out, DuplicateCapabilityEntry{
			Fingerprint: d.Fingerprint,
			Sources:     d.Sources,
			ContextCost: attributeDuplicateCost(d),
		})
	}
	return out
}

// estimatedTokensPerDuplicateToolSchema is this package's own fixed,
// documented, NOT-measured per-item average for one redundant tool schema —
// the same honest-estimate discipline internal/mcp's
// estimatedTokensPerExcludedMCPServer/estimatedTokensPerExcludedSkill
// constants document (internal/mcp/status.go), reused in spirit rather than
// value: a duplicate capability's redundant cost is a tool schema
// definition, not a whole excluded MCP configuration source or Skill
// description, so it gets its own named constant rather than borrowing
// either of those unrelated averages.
const estimatedTokensPerDuplicateToolSchema = 120

// attributeDuplicateCost estimates the extra context spent because d's
// fingerprint is reachable through more than one transport: every source
// beyond the first is redundant (the model already has the capability once
// it has seen one), so the estimate is (len(Sources)-1) *
// estimatedTokensPerDuplicateToolSchema — honestly labeled as an estimate,
// never measured, matching reporting.md §8's every-estimate-carries-method-
// and-confidence rule.
func attributeDuplicateCost(d effective.DuplicateCapability) ContextCostAttribution {
	redundant := len(d.Sources) - 1
	if redundant < 0 {
		redundant = 0
	}
	return ContextCostAttribution{
		RedundantSources: redundant,
		EstimatedTokens:  redundant * estimatedTokensPerDuplicateToolSchema,
		Method:           fmt.Sprintf("%d redundant source(s) beyond the first x ~%d tokens/tool-schema (fixed, documented per-item average, not measured from this fingerprint's actual schemas)", redundant, estimatedTokensPerDuplicateToolSchema),
		Confidence:       contextcost.ConfidenceEstimateNotMeasured,
	}
}

// computeFingerprint digests a's content-addressable identity: everything
// except Metadata (ID/GeneratedAt are per-build bookkeeping, not content),
// Spec.Fingerprint itself (obviously, since this computes it), and every
// Debug[host].Evidence[i].Spec.ObservedAt (assurance.BuildEvidence/
// HostVersionEvidence stamp these from the same req.Now Metadata.GeneratedAt
// derives from — bookkeeping of when Build ran, not content of what it
// found). Two Build calls over identical logical inputs at different
// instants produce different Metadata and different Evidence.ObservedAt
// values but the same Fingerprint — the "reproducible: input and output
// digests can reconstruct the result" trust property (docs/architecture/
// reporting.md §1).
func computeFingerprint(a Artifact) (string, error) {
	a.Report.Metadata = domain.ReportMetadata{}
	a.Report.Spec.Fingerprint = ""
	// a.Debug is a map: a was received by value, but a map's header is a
	// reference to the same underlying storage the caller's Artifact.Debug
	// already points to. Assigning into a.Debug[host] directly here would
	// silently corrupt the real, returned Artifact's Evidence.ObservedAt
	// values (used for actual display/query, not just this digest) as a
	// side effect of computing a fingerprint — so this builds an entirely
	// new map instead of mutating a.Debug in place.
	if len(a.Debug) > 0 {
		debugForDigest := make(map[string]HostDebug, len(a.Debug))
		for host, hd := range a.Debug {
			if len(hd.Evidence) > 0 {
				evidence := append([]domain.Evidence(nil), hd.Evidence...)
				for i := range evidence {
					evidence[i].Spec.ObservedAt = ""
				}
				hd.Evidence = evidence
			}
			debugForDigest[host] = hd
		}
		a.Debug = debugForDigest
	}
	return domain.CanonicalDigest(a)
}
