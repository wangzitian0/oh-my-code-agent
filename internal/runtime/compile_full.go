package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
)

// Compile is this package's full generation compiler entry point (issue #18,
// PR-14, "Full generation compiler + content-addressed store"), the one
// doc.go's original PR-09 text anticipated by name: "That is PR-14... which
// resolves a real Desired Graph (Profiles/Bindings/Activation, PR-12) into
// artifacts." Bindings themselves (Profile selection for a repository/path
// context) are PR-12 scope, not this function's -- Compile takes an
// already-selected Profile list directly (this issue's own corrected
// "Depends on PR-13 AND PR-09" line does not name PR-12; tests here build
// domain.Profile/domain.Activation/[]domain.Exception fixtures directly, and
// any future Binding-selection caller does that selection before
// constructing a CompileRequest).
//
// Unlike Bootstrap (bootstrap.go), which compiles exactly one host into its
// own generation directory (doc.go's documented per-host simplification),
// Compile takes every host named in req.Hosts and compiles all of them into
// ONE Generation document with multiple Spec.Hosts entries, written under
// one outputDir/hosts/<host>/<surface>/ tree per host but sharing a single
// manifest.json and a single content-addressed generation ID. This is a
// deliberate reading of docs/architecture/runtime.md §5.5 ("A generation
// contains one artifact tree per host and surface") and the issue's own
// round-2 MECE text ("Combining multiple hosts' artifact trees under one
// shared generation ID/directory is PR-12/PR-14 scope", doc.go) as
// describing ONE generation with several host subtrees, not several
// generations that happen to share a directory prefix -- the schema already
// supports either (GenerationSpec.Hosts was already a map before this PR),
// so this is a documented judgment call, not a schema requirement.
//
// # Why Compile reuses Bootstrap's exact Observation-classification policy
//
// The round-2 MECE requirement asks for "one compiler, two entry points,"
// not two independent implementations that happen to look similar. This
// package's shared core (compileHostTree, compile.go) already separates the
// two genuinely different concerns: walking a host's Observations, deciding
// file placement, computing artifact digests, and assembling
// GenerationSourceEntry records is general and fully shared; only the
// Observation *classification* policy (which Observations belong in the
// tree at all) and the resolved *permission* policy are caller-supplied
// inputs (hostTreeInput.Classify / hostTreeInput.Permissions).
//
// Compile supplies the exact same Classify function Bootstrap does
// (package-level classify, compile.go) rather than a Resolve-driven
// classifier, for an honest reason: a resolve.ResolvedAsset names a
// Profile-declared logical asset ID (e.g. "code-review"), while an
// Observation names a physically discovered source (a file path).
// Connecting the two -- "this discovered skill directory IS the
// Profile-declared skill named code-review" -- is exactly the Identity
// Matcher component docs/architecture/README.md's Observed-to-Effective
// Graph pipeline describes and roadmap M3 schedules; it does not exist yet,
// and internal/resolve's own doc comment is explicit that Resolve itself
// "does not implement Binding matching" (a sibling gap of the same shape).
// Building an ad hoc, weaker version of that matching here -- e.g. matching
// on file/directory basename -- would be exactly the kind of invented,
// unproven behavior this project's evidence-level discipline
// (docs/architecture/reporting.md) argues against.
//
// What Compile DOES add, honestly, on top of what Observation-classification
// alone gives Bootstrap: every resolve.ResolvedAsset (skill/mcpServer/
// instruction) this host's real Desired Graph decided is recorded as its own
// GenerationSourceEntry audit trail (resolvedAssetSources, below) --
// Included mirrors ResolvedAsset.Active, Reason carries ResolvedAsset.Reason
// and Intent, exactly as issue #18's own text asks ("recording Reason/Intent
// into GenerationSourceEntry.Reason the same way compileHostTree already
// does for the bootstrap policy's own reasons"). This sits alongside (not
// instead of) the Observation-derived trail: one records "what physically
// exists and was included/excluded," the other records "what the Desired
// Graph decided," and until the Identity Matcher exists there is honestly no
// way to unify the two into one entry per asset. A future PR building the
// Identity Matcher can replace Compile's Classify function with a
// Resolve-aware one without touching compileHostTree at all -- the seam
// this PR establishes is exactly where that upgrade belongs.
//
// Real, structural differences from Bootstrap, all genuinely new in this PR:
// a real (not fixed-policy) DesiredGraphDigest computed from req.Profiles/
// Activation/Exceptions; resolved permissions (mergePermissions) compiled
// into host artifacts via hostTreeInput.Permissions, honoring the
// DENY-is-never-weakened rule (see resolveSandboxPermission, compile.go);
// Knowledge Pack digests actually populated (req.KnowledgePacks, left empty
// by Bootstrap); every host combined into one Generation; and the new
// DesiredState/SourceDigest/OntologyVersion schema fields populated with
// real values instead of left at their PR-09 defaults.
type CompileRequest struct {
	// Worktree is the worktree this generation belongs to. Worktree.ID
	// becomes Generation.metadata.worktree and folds into the generation
	// ID; Worktree.Root is used, exactly like BootstrapRequest.Worktree.Root,
	// to compute each copied Instructions file's path relative to the
	// repository root.
	Worktree hostcontext.Worktree

	// Hosts is every host this call compiles into the ONE resulting
	// Generation, one HostCompileInput per host. Must name at least one
	// host, and no host may repeat.
	Hosts []HostCompileInput

	// Profiles, Activation, and Exceptions are internal/resolve.Resolve's
	// own three desired-state inputs (already-selected Profiles -- Binding
	// matching is PR-12 scope, not performed here), used both to compute
	// DesiredGraphDigest/DesiredState and, per host, to call resolve.Resolve
	// directly.
	Profiles   []domain.Profile
	Activation domain.Activation
	Exceptions []domain.Exception

	// KnowledgePacks are this generation's resolved Knowledge Pack
	// references (docs/architecture/runtime.md §5.3's "Knowledge Pack IDs
	// and digests"), already computed by a caller (internal/knowledge is a
	// separate resolution concern this package does not perform itself,
	// matching Bootstrap's existing "caller supplies already-computed X"
	// discipline throughout this package). May be empty.
	KnowledgePacks []domain.KnowledgePackRef

	// Now is the wall-clock time recorded in Generation.metadata.createdAt.
	// Injected, never read via time.Now() internally -- see
	// BootstrapRequest.Now's identical doc comment.
	Now time.Time

	// Parent is the previous generation ID this one supersedes, or nil.
	Parent *string

	// Invocation is optional free text describing what triggered this
	// compilation (GenerationMetadata.Invocation's doc comment), e.g.
	// "omca run codex". Never folded into the generation ID.
	Invocation string
}

// HostCompileInput is one host's slice of a CompileRequest: its detection
// and already-computed Observation inventory (normally observe.Observe's
// result for Detection.Host), plus that host's own OMCA MCP binary path --
// the same three values BootstrapRequest carries for its one host, given
// here per host since Compile handles several at once.
type HostCompileInput struct {
	Detection      hostcontext.HostDetection
	Observations   []domain.Observation
	OMCABinaryPath string
}

// validate rejects a CompileRequest this package cannot compile: no hosts,
// a duplicate host, an unrecognized/unimplemented host, a worktree with no
// resolved identity/root, a missing injected clock value, a non-absolute
// OMCABinaryPath, or Observations that do not actually belong to the host
// they are attached to (validateObservationsBelongToHost, request.go -- the
// exact same caller-composition-bug check BootstrapRequest.validate() runs).
func (req CompileRequest) validate() error {
	if len(req.Hosts) == 0 {
		return fmt.Errorf("runtime: CompileRequest: Hosts is required (at least one host)")
	}
	if req.Worktree.ID == "" {
		return fmt.Errorf("runtime: CompileRequest: Worktree.ID is required")
	}
	if req.Worktree.Root == "" {
		return fmt.Errorf("runtime: CompileRequest: Worktree.Root is required")
	}
	if req.Now.IsZero() {
		return fmt.Errorf("runtime: CompileRequest: Now is required (this package never reads the clock implicitly)")
	}
	seen := make(map[string]bool, len(req.Hosts))
	for i, h := range req.Hosts {
		if err := domain.ValidateHostID(h.Detection.Host); err != nil {
			return fmt.Errorf("runtime: CompileRequest: Hosts[%d]: %w", i, err)
		}
		if seen[h.Detection.Host] {
			return fmt.Errorf("runtime: CompileRequest: Hosts[%d]: duplicate host %q", i, h.Detection.Host)
		}
		seen[h.Detection.Host] = true
		if _, err := NativeHomeDirName(h.Detection.Host); err != nil {
			return fmt.Errorf("runtime: CompileRequest: Hosts[%d]: %w", i, err)
		}
		if h.OMCABinaryPath != "" && !filepath.IsAbs(h.OMCABinaryPath) {
			return fmt.Errorf("runtime: CompileRequest: Hosts[%d]: OMCABinaryPath %q is not absolute", i, h.OMCABinaryPath)
		}
		if err := validateObservationsBelongToHost(h.Observations, h.Detection.Host, h.Detection.Version, surfaceOf(h.Detection)); err != nil {
			return fmt.Errorf("runtime: CompileRequest: Hosts[%d]: %w", i, err)
		}
	}
	return nil
}

// mergePermissions combines every input Profile's spec.policy.permissions
// into one host-neutral map (domain.ProfilePolicy has no host selector or
// Activation-layer analogue -- docs/product/requirements.md §4.1's
// policy.permissions is host-neutral by construction, unlike assets). This
// applies the one precedence rule this project states as absolute for
// permission-shaped intent ("denied intent cannot be weakened by a lower
// scope", init.md Invariants; the same rule internal/resolve.Resolve applies
// to assets): a DENIED value, once established by any input Profile, can
// never be overwritten by a later Profile in the input order, REQUIRED or
// otherwise. A REQUIRED value is sticky against a later soft (DEFAULT/
// AVAILABLE) value but yields to a later DENIED (permissions have no
// Exception mechanism the way assets do -- see domain.Exception's doc
// comment, which scopes Exceptions to "the intent resolution engine,"
// internal/resolve, not to permissions -- so unlike Resolve's asset
// precedence, a REQUIRED/DENIED contradiction across Profiles is not
// reported as a Conflict here; it resolves to DENIED, the safer of the two,
// rather than left unresolved with no Conflict-reporting machinery to carry
// it). Otherwise the last applicable Profile in input order wins, matching
// Resolve's own "later/more-specific Profile ... can turn it off" rule for
// soft intents.
func mergePermissions(profiles []domain.Profile) map[string]domain.PermissionRef {
	merged := map[string]domain.PermissionRef{}
	for _, p := range profiles {
		for key, ref := range p.Spec.Policy.Permissions {
			existing, ok := merged[key]
			switch {
			case ok && existing.Intent == domain.IntentDenied:
				continue // DENIED is sticky: nothing overrides an established deny.
			case ok && existing.Intent == domain.IntentRequired && ref.Intent != domain.IntentDenied:
				continue // REQUIRED is sticky against a non-DENIED value.
			default:
				merged[key] = ref // a DENIED value always wins; otherwise last value wins.
			}
		}
	}
	return merged
}

// desiredGraphDigestInputs is the deterministic content Compile digests for
// Generation.spec.desiredGraphDigest. Unlike Bootstrap's fixed
// BootstrapPolicyDigest placeholder (policy.go -- see doc.go's "why
// desiredGraphDigest is a bootstrap-policy digest" section for why a
// bootstrap generation has no real value here), Compile has an actual
// Desired Graph to digest: the exact Profiles/Activation/Exceptions that fed
// resolve.Resolve. Array order is preserved deliberately (domain.
// CanonicalDigest's own documented behavior): Profile order is semantically
// meaningful to Resolve's own precedence rules (broad-to-narrow,
// docs/product/requirements.md §5.1), so two requests naming the same
// Profiles in a different order are correctly treated as different desired
// state, not spuriously deduplicated.
type desiredGraphDigestInputs struct {
	Profiles   []domain.Profile   `json:"profiles"`
	Activation domain.Activation  `json:"activation"`
	Exceptions []domain.Exception `json:"exceptions"`
}

func desiredGraphDigestFor(profiles []domain.Profile, activation domain.Activation, exceptions []domain.Exception) (string, error) {
	digest, err := domain.CanonicalDigest(desiredGraphDigestInputs{Profiles: profiles, Activation: activation, Exceptions: exceptions})
	if err != nil {
		return "", fmt.Errorf("runtime: desiredGraphDigestFor: %w", err)
	}
	return digest, nil
}

// desiredStateRef builds the domain.DesiredStateRef Compile records at
// Generation.spec.desiredState -- §5.3's "selected Profiles and Activation"
// pending-manifest field, naming exactly which document contents (by
// digest, not just logical ID) fed DesiredGraphDigest.
func desiredStateRef(profiles []domain.Profile, activation domain.Activation, exceptions []domain.Exception) (*domain.DesiredStateRef, error) {
	profileRefs := make([]domain.ProfileRef, 0, len(profiles))
	for _, p := range profiles {
		d, err := domain.CanonicalDigest(p)
		if err != nil {
			return nil, fmt.Errorf("runtime: desiredStateRef: %w", err)
		}
		profileRefs = append(profileRefs, domain.ProfileRef{ID: p.Metadata.ID, Digest: d})
	}
	activationDigest, err := domain.CanonicalDigest(activation)
	if err != nil {
		return nil, fmt.Errorf("runtime: desiredStateRef: %w", err)
	}
	exceptionDigests := make([]string, 0, len(exceptions))
	for _, e := range exceptions {
		d, err := domain.CanonicalDigest(e)
		if err != nil {
			return nil, fmt.Errorf("runtime: desiredStateRef: %w", err)
		}
		exceptionDigests = append(exceptionDigests, d)
	}
	sort.Strings(exceptionDigests)
	return &domain.DesiredStateRef{Profiles: profileRefs, Activation: activationDigest, Exceptions: exceptionDigests}, nil
}

// resolvedAssetSources turns one host's resolve.ResolvedState into
// GenerationSourceEntry audit records: one entry per decided ResolvedAsset
// (Included mirrors Active, Reason carries the resolver's own Reason and
// Intent) plus one per unresolved Conflict (always Included:false, naming
// the candidate intents that could not be adjudicated). See Compile's own
// doc comment for why this is additive to, not a replacement for, the
// Observation-derived sources compileHostTree already produces, and why
// Concept here uses resolve.AssetKind's own wire vocabulary ("mcpServer",
// camelCase, docs/product/requirements.md §4.1) rather than Observation's
// ontology Concept vocabulary ("mcp_server", snake_case,
// docs/ontology/README.md) -- these are two different, independently
// documented vocabularies for two different kinds of record, and unifying
// them is out of this PR's scope.
func resolvedAssetSources(resolved resolve.ResolvedState) []domain.GenerationSourceEntry {
	const desiredStateScope = "desired-state"
	entries := make([]domain.GenerationSourceEntry, 0, len(resolved.Assets)+len(resolved.Conflicts))
	for _, a := range resolved.Assets {
		entries = append(entries, domain.GenerationSourceEntry{
			Concept:  string(a.Kind),
			Source:   a.ID,
			Scope:    desiredStateScope,
			Host:     resolved.Host,
			Included: a.Active,
			Reason:   fmt.Sprintf("resolved desired state (intent=%q): %s", string(a.Intent), a.Reason),
		})
	}
	for _, c := range resolved.Conflicts {
		intents := make([]string, 0, len(c.CandidateIntents))
		for _, in := range c.CandidateIntents {
			intents = append(intents, string(in))
		}
		entries = append(entries, domain.GenerationSourceEntry{
			Concept:  string(c.Kind),
			Source:   c.AssetID,
			Scope:    desiredStateScope,
			Host:     resolved.Host,
			Included: false,
			Reason:   fmt.Sprintf("resolved desired state: unresolved conflict among candidate intents %v; excluded from generation until resolved (docs/product/requirements.md §4.3: 'ambiguous conflicts remain visible and block unsafe generation')", intents),
		})
	}
	return entries
}

// sourceEntryFingerprint returns a deterministic per-entry digest for one
// host's GenerationSourceEntry, folded into sourceFingerprints and, through
// it, Generation.spec.sourceDigest.
//
// It digests the WHOLE entry struct (every GenerationSourceEntry field via
// domain.CanonicalDigest), not a hand-picked subset: an earlier version of
// this fingerprint used fmt.Sprintf over only Concept/Source/Included/
// Reason, silently omitting Scope/CapabilityGap/TrackingIssue -- two Sources
// lists differing only in one of those omitted fields (e.g. the same asset
// excluded for a mundane reason vs. flagged as an unproven capability gap)
// would have collided to the identical sourceDigest (Copilot review finding
// on this PR, proven non-colliding by
// TestSourceEntryFingerprint_DiffersOnScopeAndCapabilityGap).
// Digesting the struct itself is also forward-safe: a future field added to
// GenerationSourceEntry is automatically covered without anyone needing to
// remember to update a hand-rolled format string.
func sourceEntryFingerprint(host string, entry domain.GenerationSourceEntry) (string, error) {
	digest, err := domain.CanonicalDigest(struct {
		Host  string                       `json:"host"`
		Entry domain.GenerationSourceEntry `json:"entry"`
	}{Host: host, Entry: entry})
	if err != nil {
		return "", fmt.Errorf("runtime: sourceEntryFingerprint: %w", err)
	}
	return digest, nil
}

// compileHostIDInputs is one host's deterministic slice of
// compileGenerationIDInputs.
type compileHostIDInputs struct {
	Host         string   `json:"host"`
	HostVersion  string   `json:"hostVersion"`
	Observations []string `json:"observations"`
}

// exceptionLivenessFingerprints returns one sorted "assetId|scope|live"
// string per exception, classifying each as live or expired against now
// using the EXACT boundary comparison internal/resolve.Resolve itself uses
// (resolve.go's findException: `now.Before(ex.ExpiresAt)`, strict -- an
// Exception whose ExpiresAt is now or earlier is expired). This is a
// regression fix (Copilot review finding on this PR): CompileGenerationID
// used to exclude Now entirely, but Resolve's actual output (which
// Sources/artifacts end up compiled) depends on Now through exactly this
// comparison -- two Compile calls with identical Profiles/Activation/
// Exceptions but Now on either side of an Exception's expiry boundary could
// produce different compiled content under the SAME generation ID, breaking
// the one invariant a content-addressed ID exists to guarantee ("same ID
// implies same content"). Folding in each exception's raw ExpiresAt or the
// raw Now value would overcorrect: it would make the ID change on every
// call even when no exception's live/expired classification actually
// changed, which is not "different desired state" in any sense
// resolve.Resolve's output cares about. Folding in only the classification
// itself is the minimal, precise fix: the ID changes if and only if the
// compiled content actually could.
func exceptionLivenessFingerprints(exceptions []domain.Exception, now time.Time) []string {
	out := make([]string, 0, len(exceptions))
	for _, ex := range exceptions {
		out = append(out, fmt.Sprintf("%s|%s|%v", ex.AssetID, ex.Scope, now.Before(ex.ExpiresAt)))
	}
	sort.Strings(out)
	return out
}

// compileGenerationIDInputs is the deterministic subset of CompileRequest
// that actually determines what Compile produces -- the full-compilation
// analogue of generationid.go's generationIDInputs. It deliberately excludes
// Now itself, Parent, Invocation, and every host's OMCABinaryPath, for the
// same reasons generationid.go's own doc comment gives (none of them
// changes the compiled artifact tree's content on its own; OMCABinaryPath in
// particular is always the worktree's own stable PATH-shim path, never a
// build-specific snapshot -- request.go's OMCABinaryPath doc comment).
// ExceptionLiveness is the one Now-derived value that DOES belong here --
// see exceptionLivenessFingerprints's doc comment for why raw Now itself
// still must not be folded in directly.
type compileGenerationIDInputs struct {
	Worktree           string                `json:"worktree"`
	DesiredGraphDigest string                `json:"desiredGraphDigest"`
	ExceptionLiveness  []string              `json:"exceptionLiveness"`
	PermissionsDigest  string                `json:"permissionsDigest"`
	KnowledgePacks     []string              `json:"knowledgePacks"`
	Hosts              []compileHostIDInputs `json:"hosts"`
}

// CompileGenerationID computes the content-addressed ID for the full
// generation req describes: two CompileRequests naming the same desired
// state (Profiles/Activation/Exceptions, folded through DesiredGraphDigest,
// the merged permissions digest, and each Exception's live/expired
// classification as of Now -- see exceptionLivenessFingerprints), the same
// Knowledge Pack digests, and the same per-host Observations produce the
// identical ID regardless of Parent, Invocation, or any host's
// OMCABinaryPath -- issue #18's determinism AC ("Same desired state + same
// Knowledge digests produce the identical generation digest"), proven by
// compile_full_test.go's determinism and sensitivity tests. Now itself is
// NOT excluded unconditionally the way Parent/Invocation/OMCABinaryPath
// are: two calls with different Now values produce the same ID only when
// every Exception's live/expired status is unchanged between them (a real,
// content-relevant difference otherwise -- see
// exceptionLivenessFingerprints's doc comment; this was a Copilot review
// finding on this PR).
func CompileGenerationID(req CompileRequest) (string, error) {
	if err := req.validate(); err != nil {
		return "", err
	}

	desiredGraphDigest, err := desiredGraphDigestFor(req.Profiles, req.Activation, req.Exceptions)
	if err != nil {
		return "", fmt.Errorf("runtime: CompileGenerationID: %w", err)
	}
	permissionsDigest, err := domain.CanonicalDigest(mergePermissions(req.Profiles))
	if err != nil {
		return "", fmt.Errorf("runtime: CompileGenerationID: %w", err)
	}

	packs := make([]string, 0, len(req.KnowledgePacks))
	for _, kp := range req.KnowledgePacks {
		packs = append(packs, kp.ID+"@"+kp.Digest)
	}
	sort.Strings(packs)

	hostInputs := make([]compileHostIDInputs, 0, len(req.Hosts))
	for _, h := range req.Hosts {
		fingerprints := make([]string, 0, len(h.Observations))
		for _, o := range h.Observations {
			fingerprints = append(fingerprints, observationFingerprint(o))
		}
		sort.Strings(fingerprints)
		hostInputs = append(hostInputs, compileHostIDInputs{
			Host:         h.Detection.Host,
			HostVersion:  h.Detection.Version,
			Observations: fingerprints,
		})
	}
	sort.Slice(hostInputs, func(i, j int) bool { return hostInputs[i].Host < hostInputs[j].Host })

	digest, err := domain.CanonicalDigest(compileGenerationIDInputs{
		Worktree:           req.Worktree.ID,
		DesiredGraphDigest: desiredGraphDigest,
		ExceptionLiveness:  exceptionLivenessFingerprints(req.Exceptions, req.Now),
		PermissionsDigest:  permissionsDigest,
		KnowledgePacks:     packs,
		Hosts:              hostInputs,
	})
	if err != nil {
		return "", fmt.Errorf("runtime: CompileGenerationID: %w", err)
	}
	// "generation:" prefix mirrors GenerationID's identical convention.
	return "generation:" + digest, nil
}

// hostSourceEntry pairs one host's canonical ID with the sources list
// hostSourcesFor computed for it -- aggregateSources' own input shape,
// factored out only so both Compile and freshSourceDigest (activate.go) can
// build the same list without either one hand-rolling a second (host,
// sources) tuple type.
type hostSourceEntry struct {
	Host    string
	Sources []domain.GenerationSourceEntry
}

// hostSourcesFor computes one host's rendered file content and full sources
// list (Observation-derived, via compileHostTreeFn, plus resolved-asset-
// derived, via resolvedAssetSources) -- WITHOUT writing anything to disk.
// This is Compile's own per-host step, factored out so a second caller can
// run exactly the same computation a second time against fresh inputs. See
// this file's own top doc comment's "why Compile reuses Bootstrap's exact
// Observation-classification policy" section for the sibling precedent this
// follows: activate.go's CAS check (docs/architecture/runtime.md §5.4's
// "ensure source digests still match" step) needs the identical sourceDigest
// computation Compile performs, recomputed against a freshly re-observed/
// re-resolved environment, and reusing this function -- rather than
// re-deriving a parallel digest scheme -- is what actually guarantees "same
// inputs, same digest" holds between the two call sites.
func hostSourcesFor(req CompileRequest, h HostCompileInput, permissions map[string]domain.PermissionRef) ([]generatedFile, []domain.GenerationSourceEntry, error) {
	resolved, resolveErr := resolve.Resolve(req.Profiles, req.Activation, req.Exceptions, h.Detection.Host, req.Now)
	if resolveErr != nil {
		return nil, nil, fmt.Errorf("runtime: hostSourcesFor: %w", resolveErr)
	}

	surface := surfaceOf(h.Detection)
	files, sources, treeErr := compileHostTreeFn(hostTreeInput{
		Host:           h.Detection.Host,
		Surface:        surface,
		WorktreeRoot:   req.Worktree.Root,
		Observations:   h.Observations,
		OMCABinaryPath: h.OMCABinaryPath,
		Permissions:    permissions,
		Classify:       classify,
	})
	if treeErr != nil {
		return nil, nil, treeErr
	}
	sources = append(sources, resolvedAssetSources(resolved)...)
	return files, sources, nil
}

// aggregateSources combines every host's sources list into one sorted,
// generation-wide Sources slice plus the single sourceDigest Generation.spec.
// sourceDigest records -- the exact fold Compile always performed inline
// before this PR, factored out so freshSourceDigest (activate.go) can
// recompute the identical digest from a second call to hostSourcesFor
// without duplicating the sort/fingerprint/digest steps a second time.
func aggregateSources(perHost []hostSourceEntry) ([]domain.GenerationSourceEntry, string, error) {
	var allSources []domain.GenerationSourceEntry
	var sourceFingerprints []string
	for _, hs := range perHost {
		allSources = append(allSources, hs.Sources...)
		for _, s := range hs.Sources {
			fp, fpErr := sourceEntryFingerprint(hs.Host, s)
			if fpErr != nil {
				return nil, "", fmt.Errorf("runtime: aggregateSources: %w", fpErr)
			}
			sourceFingerprints = append(sourceFingerprints, fp)
		}
	}

	sort.Slice(allSources, func(i, j int) bool {
		if allSources[i].Concept != allSources[j].Concept {
			return allSources[i].Concept < allSources[j].Concept
		}
		if allSources[i].Source != allSources[j].Source {
			return allSources[i].Source < allSources[j].Source
		}
		return allSources[i].Reason < allSources[j].Reason
	})
	sort.Strings(sourceFingerprints)
	sourceDigest, err := domain.CanonicalDigest(sourceFingerprints)
	if err != nil {
		return nil, "", fmt.Errorf("runtime: aggregateSources: %w", err)
	}
	return allSources, sourceDigest, nil
}

// Compile compiles req into one immutable, multi-host generation and writes
// it under outputDir: outputDir/manifest.json plus, for every host in
// req.Hosts, outputDir/hosts/<host>/<surface>/... -- see this file's own top
// doc comment for the full design. Like Bootstrap, it never reads a native
// filesystem location or the clock itself; outputDir is caller-injected.
//
// On success, outputDir and everything under it are read-only, matching
// Bootstrap's identical guarantee (readonly.go); a caller that needs to
// recompile must target a fresh outputDir.
func Compile(req CompileRequest, outputDir string) (domain.Generation, error) {
	if err := req.validate(); err != nil {
		return domain.Generation{}, err
	}
	if outputDir == "" {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: outputDir is required")
	}
	if !filepath.IsAbs(outputDir) {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: outputDir %q is not absolute", outputDir)
	}

	genID, err := CompileGenerationID(req)
	if err != nil {
		return domain.Generation{}, err
	}
	desiredGraphDigest, err := desiredGraphDigestFor(req.Profiles, req.Activation, req.Exceptions)
	if err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", err)
	}
	desiredState, err := desiredStateRef(req.Profiles, req.Activation, req.Exceptions)
	if err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", err)
	}
	permissions := mergePermissions(req.Profiles)

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", err)
	}

	hosts := make(map[string]domain.GenerationHostEntry, len(req.Hosts))
	perHost := make([]hostSourceEntry, 0, len(req.Hosts))

	for _, h := range req.Hosts {
		surface := surfaceOf(h.Detection)
		files, sources, srcErr := hostSourcesFor(req, h, permissions)
		if srcErr != nil {
			return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", srcErr)
		}

		artifacts := make([]domain.GenerationArtifact, 0, len(files))
		for _, f := range files {
			fullPath := filepath.Join(outputDir, f.RelPath)
			if mkErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); mkErr != nil {
				return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", mkErr)
			}
			if writeErr := os.WriteFile(fullPath, f.Content, 0o644); writeErr != nil {
				return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", writeErr)
			}
			digest, digestErr := domain.CanonicalDigest(string(f.Content))
			if digestErr != nil {
				return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", digestErr)
			}
			artifacts = append(artifacts, domain.GenerationArtifact{Path: f.RelPath, Digest: digest})
		}

		hosts[h.Detection.Host] = domain.GenerationHostEntry{
			Surface:   surface,
			AdapterID: AdapterID,
			Ownership: domain.OwnershipManaged,
			Artifacts: artifacts,
		}

		perHost = append(perHost, hostSourceEntry{Host: h.Detection.Host, Sources: sources})
	}

	allSources, sourceDigest, err := aggregateSources(perHost)
	if err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", err)
	}

	knowledgePacks := req.KnowledgePacks
	if knowledgePacks == nil {
		knowledgePacks = []domain.KnowledgePackRef{}
	}

	gen := domain.Generation{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Generation",
		Metadata: domain.GenerationMetadata{
			ID:         genID,
			Worktree:   req.Worktree.ID,
			Invocation: req.Invocation,
			Parent:     req.Parent,
			CreatedAt:  req.Now.UTC().Format(time.RFC3339),
		},
		Spec: domain.GenerationSpec{
			DesiredGraphDigest: desiredGraphDigest,
			OntologyVersion:    domain.CurrentOntologyVersion,
			SourceDigest:       sourceDigest,
			DesiredState:       desiredState,
			KnowledgePacks:     knowledgePacks,
			Hosts:              hosts,
			Sources:            allSources,
			Status:             "pending",
			// Diff, RiskConfirmations, and ExpectedEvidence are reserved
			// placeholders -- see their doc comments in
			// internal/domain/generation.go. Left at their zero values
			// (nil/empty) here, exactly like Bootstrap.
		},
	}

	if err := domain.ValidateGeneration(gen); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: compiled an invalid Generation: %w", err)
	}

	manifestPath := filepath.Join(outputDir, "manifest.json")
	manifestBytes, err := json.MarshalIndent(gen, "", "  ")
	if err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", err)
	}

	if err := makeTreeReadOnly(outputDir); err != nil {
		return domain.Generation{}, fmt.Errorf("runtime: Compile: %w", err)
	}

	return gen, nil
}
