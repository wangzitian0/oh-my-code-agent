package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/assurance"
	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// VerificationResult is [VerifyActivation]'s outcome for one host's current
// generation.
type VerificationResult struct {
	// Host is the canonical host ID verified.
	Host string
	// GenerationID is the generation that was verified -- host's "current"
	// generation at the moment VerifyActivation ran.
	GenerationID string
	// Passed is true when every recorded artifact digest still matches the
	// on-disk content AND every Observation-derived source the manifest
	// recorded as Included:true is still discoverable in a freshly
	// re-derived EffectiveGraph (issue #70) -- see VerifyActivation's own
	// doc comment for the full two-check design.
	Passed bool
	// FailedArtifacts lists every entry (a real artifact path, relative to
	// the generation directory, for the artifact-digest check; a
	// GenerationSourceEntry's own Source -- or, when empty, its Concept --
	// for the effective-graph check) that failed verification: an artifact
	// missing/unreadable, present with content that no longer digests to
	// what the manifest recorded, or a manifest-recorded Included:true
	// source that a fresh re-observation of the activated generation could
	// no longer discover. Bare identifiers only, never a formatted
	// "identifier: error text" string -- see Detail for the human-readable
	// explanation. Empty when Passed.
	FailedArtifacts []string
	// Detail is a human-readable summary, always non-empty.
	Detail string
}

// VerifyActivation re-reads host's CURRENT generation directory under
// worktreeStateDir and recomputes every artifact digest
// [domain.GenerationHostEntry.Artifacts] recorded for host at compile time
// (internal/runtime/compile_full.go's Compile, which stamps each rendered
// file with domain.CanonicalDigest(content) at the moment it writes it),
// comparing each one against a fresh read of the same path today.
//
// This is docs/architecture/runtime.md's MVP acceptance scenario item 7,
// "activate it after restart and verify the new effective state," and the
// M5 exit gate line "failed verification leaves a recoverable previous
// generation" -- read literally as "prove the generation Activate just
// switched 'current' to is still, byte for byte, the generation Compile
// actually produced."
//
// This is a DIFFERENT question from Activate's own CAS check
// (freshSourceDigest, activate.go): the CAS check runs BEFORE the switch
// and asks "do the pending generation's own DESIRED-STATE INPUTS still
// match a fresh recomputation" (has the world drifted since compile time);
// VerifyActivation runs AFTER the switch and asks "does the generation
// directory Activate just made 'current' still contain, on disk, exactly
// what its own manifest says it does" (has the compiled OUTPUT itself been
// corrupted, partially written, or tampered with since compile time,
// including by whatever raced the switch itself). Neither implies the
// other -- freshSourceDigest never reads a single byte from
// worktreeStateDir/generations, and VerifyActivation never re-observes
// anything outside the generation directory itself.
//
// VerifyActivation performs no writes and no subprocess execution: it is a
// pure filesystem read confined entirely to
// worktreeStateDir/generations/<current generation>, the same OMCA-owned,
// already-isolated tree Compile itself wrote -- never a real host's native
// home, and never anything requiring a live host process. This is what
// makes it safe to run unconditionally after every real Activate call (see
// [ActivateAndVerify]), not just in a test against a fixture.
//
// A host with no current generation, an unreadable manifest, or a manifest
// naming no host entry for host is a genuine verification failure, not a
// silently-skipped no-op: there is nothing recoverable to report as
// "verified," and a caller (ActivateAndVerify) that skipped verification
// for exactly the cases where something is already wrong would defeat the
// whole point of this function.
//
// # Artifact-digest integrity, plus a re-derived EffectiveGraph (issue #70)
//
// VerifyActivation runs two layered checks, in order, and reports both:
// the artifact-digest check above -- "is the generation directory Activate
// just switched 'current' to, byte for byte, still what Compile actually
// produced" -- and, additionally, a re-derivation of this host's
// effective.EffectiveGraph from the activated generation's OWN compiled
// tree, verified with internal/assurance.VerifyGraph and cross-checked
// against every GenerationSourceEntry the generation's own manifest
// recorded as Included:true (crossCheckEffectiveGraph, below).
//
// The artifact-digest check alone cannot catch a real, different failure
// class: a compiler bug where Compile's own manifest claims a source is
// included but renders it in a way internal/observe's discovery rules would
// never recognize as that source again if the generation directory were
// re-observed as a host home -- internally-consistent-but-wrong output. The
// digest check only ever proves "the bytes Compile wrote are still the
// bytes on disk"; it says nothing about whether those bytes are actually
// DISCOVERABLE the way a real host launch (via internal/shim's native-home
// redirection) would expect them to be. This second check closes exactly
// that gap: it points a synthetic hostcontext.HostDetection's NativeHomes
// at generationDir/hosts/<host>/<surface>/<NativeHomeDirName> (the exact
// directory layout compile_full.go's Compile already writes, and a real
// launch's native-home redirection already points a host binary at) and its
// WorktreeRoot at generationDir/hosts/<host>/<surface>/instructions (the
// exact directory compileHostTree renders repository-scope Instructions
// sources into, preserving each one's original repository-relative path --
// compile.go's compileHostTree), re-runs internal/observe.Observe against
// that synthetic detection exactly as a real launch's own observation pass
// would, resolves this host's domain.HostKnowledge Pack the same way
// internal/report/build.go's Build already does (knowledge.Default plus
// Repository.Resolve/Packs -- this file's knowledgePackByID), computes a
// fresh effective.EffectiveGraph (internal/effective.ComputeEffectiveGraph),
// and verifies it (internal/assurance.VerifyGraph) -- failing when a
// concept the manifest's own Sources list records at least one Included:true
// Observation-derived entry for is not present anywhere in that fresh graph
// (crossCheckEffectiveGraph).
//
// This second check is deliberately restricted to Observation-derived
// Sources entries -- compileHostTree's own records, distinguishable from
// resolvedAssetSources' desired-state audit trail by Scope !=
// desiredStateScope (compile_full.go) -- and never resolvedAssetSources'
// own entries: those record what internal/resolve.Resolve's pure intent
// resolution decided (ResolvedAsset.Active), which this codebase can
// honestly confirm has no necessary physical rendering to re-observe at all
// (no Identity Matcher connects a resolved asset to a discovered Observation
// yet -- compile_full.go's own "Why Compile reuses Bootstrap's exact
// Observation-classification policy" section). Cross-checking those would
// produce false failures unrelated to any real compiler bug.
//
// Both checks run unconditionally and independently: neither one's failure
// masks or short-circuits the other, and a VerificationResult reports every
// artifact-digest AND effective-graph failure together when both occur.
//
// Like the artifact-digest check, this is a pure, safe, read-only operation
// confined entirely to worktreeStateDir/generations/<current generation>:
// the synthetic HostDetection this builds is pointed exclusively at paths
// inside that already-isolated tree, never a real host's real native home,
// and internal/observe.Observe itself performs no writes and no subprocess
// execution (internal/observe/doc.go's own safety-properties list) -- so
// this check inherits exactly the same safety guarantee VerifyActivation's
// own doc comment above already documents.
func VerifyActivation(worktreeStateDir, host string, now time.Time) (VerificationResult, error) {
	if worktreeStateDir == "" {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: worktreeStateDir is required")
	}
	if err := domain.ValidateHostID(host); err != nil {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: %w", err)
	}
	if now.IsZero() {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: now is required (this package never reads the clock implicitly)")
	}

	currentDir, err := CurrentGenerationDir(worktreeStateDir, host)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: no current generation for %s to verify: %w", host, err)
	}
	gen, err := ReadGenerationManifest(currentDir)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: current generation manifest for %s at %s is unreadable: %w", host, currentDir, err)
	}
	entry, ok := gen.Spec.Hosts[host]
	if !ok {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: current generation %s for %s has no host entry for %q in its own manifest", gen.Metadata.ID, host, host)
	}

	var failed []string
	var failReasons []string
	for _, a := range entry.Artifacts {
		full := filepath.Join(currentDir, a.Path)
		content, readErr := os.ReadFile(full)
		if readErr != nil {
			failed = append(failed, a.Path)
			failReasons = append(failReasons, fmt.Sprintf("%s: %v", a.Path, readErr))
			continue
		}
		// Mirrors Compile's own digest computation exactly
		// (compile_full.go: domain.CanonicalDigest(string(f.Content))) --
		// reusing the identical call shape is what makes "same bytes, same
		// digest" a structural guarantee rather than two independently
		// maintained digest schemes that could drift apart from each
		// other for reasons that have nothing to do with the file actually
		// changing (the same reasoning activate.go's freshSourceDigest doc
		// comment gives for reusing hostSourcesFor/aggregateSources).
		digest, digestErr := domain.CanonicalDigest(string(content))
		if digestErr != nil {
			failed = append(failed, a.Path)
			failReasons = append(failReasons, fmt.Sprintf("%s: computing digest: %v", a.Path, digestErr))
			continue
		}
		if digest != a.Digest {
			failed = append(failed, a.Path)
			failReasons = append(failReasons, a.Path)
		}
	}

	// Second, independent check (issue #70): re-derive an EffectiveGraph
	// from the activated generation's own compiled tree and cross-check it
	// against every Observation-derived source the manifest itself recorded
	// as Included:true. Runs unconditionally -- even when the digest loop
	// above already found failures -- so neither check can mask the other;
	// see this function's own doc comment for the full design.
	egFailed, egReasons := verifyEffectiveGraphAgainstManifest(worktreeStateDir, currentDir, host, entry, gen.Spec.Sources)
	failed = append(failed, egFailed...)
	failReasons = append(failReasons, egReasons...)

	if len(failed) > 0 {
		return VerificationResult{
			Host:            host,
			GenerationID:    gen.Metadata.ID,
			Passed:          false,
			FailedArtifacts: failed,
			Detail:          fmt.Sprintf("%d issue(s) found verifying %s's generation %s (artifact-digest integrity and/or a re-derived effective graph): %v", len(failed), host, gen.Metadata.ID, failReasons),
		}, nil
	}
	return VerificationResult{
		Host:         host,
		GenerationID: gen.Metadata.ID,
		Passed:       true,
		Detail:       fmt.Sprintf("%d artifact(s) for %s match generation %s's recorded manifest digests, and its effective graph re-derives cleanly from the compiled tree", len(entry.Artifacts), host, gen.Metadata.ID),
	}, nil
}

// effectiveGraphSourceConcepts are the GenerationSourceEntry.Concept values
// verifyEffectiveGraphAgainstManifest knows how to cross-reference against a
// fresh effective.EffectiveGraph -- the exact three concepts
// internal/effective.ExtractCandidates itself understands (extract.go's own
// doc comment: "this package's scope is the three concepts... instruction,
// skill, mcp_server"). A GenerationSourceEntry naming any other concept
// (e.g. compile.go's "permission", a compile-time-only vocabulary with no
// ontology or EffectiveGraph counterpart at all) is outside what a
// re-derived EffectiveGraph could ever honestly confirm or refute, so it is
// simply not compared.
var effectiveGraphSourceConcepts = map[string]bool{
	"instruction": true,
	"skill":       true,
	"mcp_server":  true,
}

// knowledgePackByID returns the loaded Pack named packID from repo, if any.
// Mirrors internal/report/build.go's identical findPack: Resolution already
// carries an in-process pointer to the matched Pack, but that field stays
// unexported inside internal/knowledge, so both callers look it back up by
// ID from Repository's own exported Packs() accessor instead of reaching
// into Resolution's internals.
func knowledgePackByID(repo knowledge.Repository, packID string) (knowledge.Pack, bool) {
	for _, p := range repo.Packs() {
		if p.Knowledge.Metadata.ID == packID {
			return p, true
		}
	}
	return knowledge.Pack{}, false
}

// verifyEffectiveGraphAgainstManifest is [VerifyActivation]'s second check
// (issue #70, see that function's own doc comment for the full design): it
// builds a synthetic hostcontext.HostDetection pointed entirely at paths
// inside generationDir, re-runs internal/observe.Observe against it,
// resolves this host's domain.HostKnowledge Pack, computes and verifies a
// fresh effective.EffectiveGraph, and cross-checks it against sources
// (normally gen.Spec.Sources) via crossCheckEffectiveGraph.
//
// hostVersion is read from worktreeStateDir's own CurrentRecord sidecar
// (ReadCurrentRecord -- the same "current" pointer SetCurrentGeneration
// wrote host's real detected version into at activation time), not from the
// Generation document itself: domain.GenerationHostEntry records no host
// version field (only Surface/AdapterID/Ownership/Artifacts), so this is the
// one place that fact is still available. A missing or unreadable
// CurrentRecord degrades hostVersion to "" rather than failing this check
// outright: knowledge.Repository.Resolve's own "zero matches degrades
// honestly" contract turns an empty version into an unqualified Resolution
// (hk stays domain.HostKnowledge{}), which still lets
// effective.ComputeEffectiveGraph run and extract/match candidates -- this
// check's cross-check step only cares about identity/discoverability, never
// evidence level, so it stays meaningful even without a resolved Pack.
//
// Any internal error while re-observing or re-computing the graph (an
// unsupported host, a malformed synthetic detection, etc.) is folded into a
// failure the same way the artifact-digest loop above folds a read/digest
// error into FailedArtifacts/Detail, rather than returned as a VerifyActivation-level
// error: this function's whole contract, like the digest check's, is that an
// inability to positively confirm integrity is itself a verification
// failure, not a caller-visible exception.
func verifyEffectiveGraphAgainstManifest(worktreeStateDir, generationDir, host string, entry domain.GenerationHostEntry, sources []domain.GenerationSourceEntry) (failed []string, reasons []string) {
	const setupFailureIdent = "effective-graph-verification"

	surface := entry.Surface
	if surface == "" {
		surface = defaultSurface
	}

	nativeHomeDirName, err := NativeHomeDirName(host)
	if err != nil {
		return []string{setupFailureIdent}, []string{fmt.Sprintf("effective-graph re-derivation: %v", err)}
	}
	nativeHomeEnvVar, err := NativeHomeEnvVar(host)
	if err != nil {
		return []string{setupFailureIdent}, []string{fmt.Sprintf("effective-graph re-derivation: %v", err)}
	}

	hostPrefix := filepath.Join(generationDir, "hosts", host, surface)
	detection := hostcontext.HostDetection{
		Host:    host,
		Surface: surface,
		NativeHomes: []hostcontext.NativeHome{
			{Name: nativeHomeEnvVar, Path: filepath.Join(hostPrefix, nativeHomeDirName)},
		},
	}

	var hostVersion string
	if rec, recErr := ReadCurrentRecord(worktreeStateDir, host); recErr == nil {
		hostVersion = rec.HostVersion
	}

	obs, obsErr := observe.Observe(observe.Request{
		Detection:    detection,
		WorktreeRoot: filepath.Join(hostPrefix, "instructions"),
	})
	if obsErr != nil {
		return []string{setupFailureIdent}, []string{fmt.Sprintf("effective-graph re-derivation: re-observing the activated generation's own compiled tree failed: %v", obsErr)}
	}

	var hk domain.HostKnowledge
	if repo, repoErr := knowledge.Default(); repoErr == nil {
		resolution := repo.Resolve(host, surface, hostVersion)
		if resolution.Qualified {
			if pack, ok := knowledgePackByID(repo, resolution.PackID); ok {
				hk = pack.Knowledge
			}
		}
	}

	graph, geErr := effective.ComputeEffectiveGraph(host, hostVersion, obs, hk, effective.Options{}, nil)
	if geErr != nil {
		return []string{setupFailureIdent}, []string{fmt.Sprintf("effective-graph re-derivation: computing the effective graph failed: %v", geErr)}
	}
	graph = assurance.VerifyGraph(host, graph, hk)

	return crossCheckEffectiveGraph(host, sources, graph)
}

// crossCheckEffectiveGraph reports, for every GenerationSourceEntry in
// sources that belongs to host, is Included:true, is Observation-derived
// (Scope != desiredStateScope -- see that const's own doc comment for why
// resolvedAssetSources' desired-state entries are never checked here), and
// names one of effectiveGraphSourceConcepts, whether the fresh graph
// actually found SOMETHING of that concept -- either a resolved
// EffectiveEntry or an unresolved Conflict. A Conflict still counts as
// "found": it means the Identity Matcher and merge logic genuinely
// recognized content of that concept and simply could not adjudicate a
// value for it, which already proves the physical content was
// rediscoverable -- exactly this check's whole question. Only "nothing of
// this concept was found at all" is a failure.
//
// This cannot verify a one-entry-to-one-entity correspondence: a
// GenerationSourceEntry's own Source field names the ORIGINAL path Compile
// observed at compile time (a real worktree/native-home location), which
// internal/effective's Identity Matcher folds into a LogicalID keyed on
// that exact path (extract.go's extractInstructionCandidate, e.g.) -- a
// value a re-observation of the compiled generation tree (a different
// directory, by construction) can never reproduce. What this DOES honestly
// prove is exactly the failure class issue #70 exists for: "Compile's
// manifest claims a source of concept X is included, but nothing of concept
// X is discoverable at all by re-observing what Compile actually wrote" --
// a compiler bug that rendered content at a path internal/observe's own
// discovery rules would never find again.
func crossCheckEffectiveGraph(host string, sources []domain.GenerationSourceEntry, graph effective.EffectiveGraph) (failed []string, reasons []string) {
	present := map[string]bool{}
	for _, e := range graph.Entries {
		present[e.Concept] = true
	}
	for _, c := range graph.Conflicts {
		present[c.Concept] = true
	}

	reportedConcept := map[string]bool{}
	for _, s := range sources {
		if s.Host != host || !s.Included || s.Scope == desiredStateScope || !effectiveGraphSourceConcepts[s.Concept] {
			continue
		}
		if present[s.Concept] {
			continue
		}
		ident := s.Source
		if ident == "" {
			ident = s.Concept
		}
		failed = append(failed, ident)
		if !reportedConcept[s.Concept] {
			reportedConcept[s.Concept] = true
			reasons = append(reasons, fmt.Sprintf("effective-graph re-derivation: manifest records at least one Included=true %q source (e.g. %q) for %s, but re-observing the activated generation's own compiled tree found nothing of that concept in the re-derived effective graph", s.Concept, ident, host))
		}
	}
	return failed, reasons
}

// ActivateAndVerifyResult is [ActivateAndVerify]'s success value.
type ActivateAndVerifyResult struct {
	Activation   ActivationResult
	Verification VerificationResult
	// RolledBack is true when a failed Verification triggered an automated
	// Rollback that itself succeeded.
	RolledBack bool
	// Rollback is non-nil exactly when RolledBack is true.
	Rollback *RollbackResult
}

// ActivateAndVerify runs [Activate], then immediately [VerifyActivation]
// against the generation Activate just switched "current" to, automatically
// triggering [Rollback] to the parent generation when verification fails --
// the M5 AC this PR (issue #28) exists to close: "failed post-activation
// verification triggers automated rollback to the parent; both events are
// ledgered."
//
// "Both events" are:
//
//  1. a "verification-failed" Ledger entry for the generation that failed
//     verification, appended BEFORE any rollback is attempted -- so the
//     failure itself is durably recorded even if the automated rollback
//     that follows cannot proceed (e.g. no parent generation exists yet,
//     see below);
//  2. Rollback's own "rolledback" entry (rollback.go), appended by Rollback
//     itself exactly as it already is for a manually invoked `omca
//     rollback`.
//
// A VerifyActivation call that itself errors (e.g. the current pointer is
// somehow corrupt immediately after a successful switch) is treated
// identically to Passed=false, never silently ignored: an inability to
// positively confirm the activated generation's integrity is not
// meaningfully different from a confirmed failure for the purpose of
// deciding whether to roll back -- this function's whole contract is "never
// leave a generation whose integrity could not be confirmed installed as
// 'current' without at least trying to recover."
//
// If the automated Rollback itself cannot proceed -- most notably, the
// activated generation has no parent recorded (e.g. this was the very
// first activation for host in this worktree) -- ActivateAndVerify returns
// a non-nil error explaining both the original verification failure and why
// no automatic recovery was possible, but the "verification-failed" Ledger
// entry (event 1, above) has already been durably recorded by that point:
// docs/project/roadmap.md's M5 exit gate line "failed verification leaves a
// recoverable previous generation" is a property of activations that HAVE a
// previous generation; a first activation with no predecessor is honestly
// outside what any rollback -- automated or manual -- can promise to
// recover, and this function never pretends otherwise.
//
// generationsRoot is the same caller-resolved, absolute
// worktreeStateDir/generations path every other Rollback caller already
// passes (see rollback.go's runRollback) -- required only on the failure
// path, but always validated up front by delegating straight to Rollback's
// own checks.
func ActivateAndVerify(req ActivateRequest, generationsRoot string) (ActivateAndVerifyResult, error) {
	actResult, err := Activate(req)
	if err != nil {
		return ActivateAndVerifyResult{}, err
	}

	verResult, verErr := VerifyActivation(req.WorktreeStateDir, req.Host, req.Now)
	if verErr != nil {
		verResult = VerificationResult{
			Host:         req.Host,
			GenerationID: actResult.ActivatedGenerationID,
			Passed:       false,
			Detail:       fmt.Sprintf("post-activation verification could not run: %v", verErr),
		}
	}

	result := ActivateAndVerifyResult{Activation: actResult, Verification: verResult}
	if verResult.Passed {
		return result, nil
	}

	// Event 1: the verification failure itself, ledgered BEFORE any
	// rollback attempt -- best-effort in the sense that its own failure
	// must never mask the real verification failure being reported, but
	// deliberately attempted unconditionally, even on a path where
	// rollback itself is about to fail too (e.g. no parent), matching
	// Activate's identical "record the rejected attempt, then return the
	// real error" discipline for a CAS rejection.
	ledgerGenID := verResult.GenerationID
	if ledgerGenID == "" {
		ledgerGenID = actResult.ActivatedGenerationID
	}
	if ledgerErr := AppendLedgerEntry(req.WorktreeStateDir, req.Host, LedgerEntry{
		Host:         req.Host,
		GenerationID: ledgerGenID,
		Kind:         "verification-failed",
		RecordedAt:   req.Now.UTC().Format(time.RFC3339),
		Detail:       verResult.Detail,
	}); ledgerErr != nil {
		return result, fmt.Errorf("runtime: ActivateAndVerify: %s: post-activation verification failed (%s) and recording that failure to the Ledger also failed: %w", req.Host, verResult.Detail, ledgerErr)
	}

	detection, ok := detectionForHost(req.Fresh.Hosts, req.Host)
	if !ok {
		return result, fmt.Errorf("runtime: ActivateAndVerify: %s: post-activation verification failed (%s) and automated rollback cannot proceed: Fresh.Hosts has no entry for %q", req.Host, verResult.Detail, req.Host)
	}

	// Event 2: Rollback appends its own "rolledback" entry on success.
	// Rollback takes its own activation lock (rollback.go) -- safe to call
	// here because Activate's defer already released its own lock before
	// returning above, so this is a fresh, independent acquisition, not a
	// re-entrant one.
	rbResult, rbErr := Rollback(req.WorktreeStateDir, generationsRoot, req.Host, detection, req.Now)
	if rbErr != nil {
		return result, fmt.Errorf("runtime: ActivateAndVerify: %s: post-activation verification failed (%s) and automated rollback also failed: %w", req.Host, verResult.Detail, rbErr)
	}
	result.RolledBack = true
	result.Rollback = &rbResult
	return result, nil
}
