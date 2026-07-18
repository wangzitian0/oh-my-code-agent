package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// BisectStep is one disposable generation [Bisect] would build (DryRun) or
// did build (!DryRun): the Nth generation in the sequence, importing
// exactly the first N candidate sources -- never fewer, never a different
// N, and no others.
type BisectStep struct {
	// Index is this step's 1-based position in the sequence.
	Index int
	// CandidateID is the domain.Observation.Metadata.ID of the ONE new
	// candidate source this step adds beyond the previous step (step 1's
	// own single candidate has no previous step to add beyond).
	CandidateID string
	// GenerationID is this step's content-addressed generation ID -- always
	// populated, even for a DryRun plan, since CompileGenerationID needs no
	// write to compute.
	GenerationID string
	// OutputDir is this step's generation directory under GenerationsRoot.
	// Empty for a DryRun plan (nothing was compiled, so there is nothing to
	// point at yet).
	OutputDir string
	// Compiled is true once Compile has actually run for this step. Always
	// false for a DryRun plan.
	Compiled bool
}

// BisectPlan is [Bisect]'s full result: the ordered disposable-generation
// sequence for Host, and whether it was actually compiled.
type BisectPlan struct {
	Host   string
	DryRun bool
	Steps  []BisectStep
}

// BisectRequest is everything [Bisect] needs to plan or build one host's
// disposable bisect sequence.
type BisectRequest struct {
	// Compile is the same desired-state/host/observation input Compile
	// itself takes. Hosts must contain exactly the one host being
	// bisected: Bisect is single-host, matching `omca bisect <host>`'s own
	// grammar (docs/architecture/runtime.md §11's "omca bisect codex") --
	// no acceptance criterion this PR closes asks for a bisected
	// multi-host generation, and Compile.Hosts[0].Observations is exactly
	// the candidate-source set Bisect imports one at a time (below).
	Compile CompileRequest
	// GenerationsRoot is where compiled disposable generations are
	// written, the same content-addressed convention
	// EnsureGeneration/Compile already use (generationsRoot/
	// DirSafeID(id)) -- required unless DryRun.
	GenerationsRoot string
	// DryRun reports the plan -- which disposable generations Bisect would
	// build, in what order, and what content-addressed ID each one would
	// get -- without calling Compile or writing anything to disk. This is
	// the mandatory safety mode docs/architecture/runtime.md's round-3
	// pre-dispatch audit requires `omca bisect` to ship with: a DryRun call
	// never compiles or activates anything, full stop (see Bisect's own
	// doc comment for why the real, compiling path is equally safe by
	// construction, not just the DryRun one).
	DryRun bool
}

// Bisect builds (or, for a DryRun request, only plans) req.Compile's target
// host's disposable bisect sequence -- docs/architecture/runtime.md §11's
// "bisect builds disposable generations that import candidate sources one
// at a time."
//
// # What "candidate sources" means here
//
// The candidate set is every domain.Observation in
// req.Compile.Hosts[0].Observations -- every Instruction, Skill, MCP
// server, Hook, and Policy/state fact Observe found for this host, in
// Observe's own stable Metadata.ID sort order (re-sorted defensively here
// so a caller-supplied, differently-ordered slice still produces a
// deterministic sequence). The issue's own text gives "a Profile's
// newly-enabled Skills/MCP servers" only as an example ("e.g."), not a
// restriction to those two concepts -- treating every observed source as a
// candidate, rather than hand-picking which concepts qualify, means Bisect
// needs no separate, independently-maintained notion of "which kinds of
// source can cause a problem" that could silently drift out of sync with
// whatever concepts internal/observe actually knows how to discover.
//
// Step N (1-indexed) compiles a CompileRequest identical to req.Compile in
// every field except Hosts[0].Observations, which is narrowed to exactly
// the first N candidates in sorted order -- "one at a time" read literally:
// each step differs from the previous by exactly one additional candidate,
// never a reordering or a removal. Profiles/Activation/Exceptions (the
// desired-state POLICY: which sources are actually included, at what
// intent) are held fixed across every step, matching every other Compile
// caller's discipline of "the compiler decides inclusion, callers only ever
// vary inputs" -- Bisect varies exactly one input dimension (which sources
// exist to be classified at all) and nothing else, so a difference observed
// between generation N-1 and N's compiled output is attributable to
// candidate N's own inclusion decision, not to some other simultaneously
// changed variable.
//
// # Why this is safe even in its real, compiling form
//
// Bisect NEVER calls SetPendingGeneration, SetCurrentGeneration, or
// Activate for any step -- every compiled generation is written under
// GenerationsRoot and left there, inspectable (ReadGenerationManifest,
// `omca diff`, `omca compare`) but never activated: the "disposable" /
// "never activating any of them as current" contract issue #28's own text
// requires. Compile only ever writes new, content-addressed, read-only
// files under GenerationsRoot -- the exact same write surface
// EnsureGeneration/Compile already have for `omca env`/`omca run`/`omca
// activate` -- and never touches "current"/"pending"/the Ledger/any native
// host home. There is no code path in this function that can activate,
// exec, or disrupt anything: a real (non-DryRun) Bisect call differs from a
// DryRun one only in whether os.WriteFile actually runs, never in what it
// could possibly affect.
//
// Compiling is idempotent, mirroring cmd/omca/mcp.go's own "compute genID,
// check for an existing valid manifest, compile only on a genuine cache
// miss" pattern (used there for the identical reason: two different
// callers -- or, here, two different bisect runs whose candidate sets
// happen to share a prefix -- computing the same content-addressed
// generation must never both attempt to write into an already-read-only
// directory).
func Bisect(req BisectRequest) (BisectPlan, error) {
	if len(req.Compile.Hosts) != 1 {
		return BisectPlan{}, fmt.Errorf("runtime: Bisect: Compile.Hosts must name exactly one host (bisect is single-host), got %d", len(req.Compile.Hosts))
	}
	host := req.Compile.Hosts[0].Detection.Host
	if err := domain.ValidateHostID(host); err != nil {
		return BisectPlan{}, fmt.Errorf("runtime: Bisect: %w", err)
	}
	if !req.DryRun {
		if req.GenerationsRoot == "" {
			return BisectPlan{}, fmt.Errorf("runtime: Bisect: GenerationsRoot is required unless DryRun")
		}
		if !filepath.IsAbs(req.GenerationsRoot) {
			return BisectPlan{}, fmt.Errorf("runtime: Bisect: GenerationsRoot %q is not absolute", req.GenerationsRoot)
		}
	}

	candidates := append([]domain.Observation(nil), req.Compile.Hosts[0].Observations...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Metadata.ID < candidates[j].Metadata.ID })

	baseHost := req.Compile.Hosts[0]
	steps := make([]BisectStep, 0, len(candidates))
	for i := 1; i <= len(candidates); i++ {
		stepReq := req.Compile
		stepReq.Hosts = []HostCompileInput{{
			Detection:      baseHost.Detection,
			Observations:   candidates[:i],
			OMCABinaryPath: baseHost.OMCABinaryPath,
		}}
		stepReq.Invocation = fmt.Sprintf("omca bisect %s (step %d/%d)", host, i, len(candidates))
		// A bisect generation is a disposable inspection artifact, never a
		// real activation lineage: Parent is left exactly whatever
		// req.Compile.Parent already was (normally nil) for every step,
		// never synthesized from a previous bisect step's own ID -- so a
		// bisect run can never be misread by Rollback (which trusts
		// Metadata.Parent completely) as a real generation history.

		genID, err := CompileGenerationID(stepReq)
		if err != nil {
			return BisectPlan{}, fmt.Errorf("runtime: Bisect: step %d/%d: %w", i, len(candidates), err)
		}
		step := BisectStep{
			Index:        i,
			CandidateID:  candidates[i-1].Metadata.ID,
			GenerationID: genID,
		}

		if !req.DryRun {
			outputDir := filepath.Join(req.GenerationsRoot, DirSafeID(genID))
			if _, err := ensureCompiled(stepReq, outputDir); err != nil {
				return BisectPlan{}, fmt.Errorf("runtime: Bisect: step %d/%d: %w", i, len(candidates), err)
			}
			step.OutputDir = outputDir
			step.Compiled = true
		}

		steps = append(steps, step)
	}

	return BisectPlan{Host: host, DryRun: req.DryRun, Steps: steps}, nil
}

// ensureCompiled is Compile with EnsureGeneration's own idempotency
// discipline layered on top, for a caller (Bisect) that -- unlike every
// existing EnsureGeneration call site -- builds full CompileRequests rather
// than BootstrapRequests, so EnsureGeneration itself (which is hardcoded to
// Bootstrap) does not fit. Mirrors cmd/omca/mcp.go's identical inline
// "ReadGenerationManifest; a genuinely missing manifest is a cache miss,
// anything else present-but-invalid is a refuse-to-overwrite error"
// pattern verbatim, moved here so Bisect's own per-step loop does not have
// to repeat it.
func ensureCompiled(req CompileRequest, outputDir string) (domain.Generation, error) {
	gen, err := ReadGenerationManifest(outputDir)
	if err == nil {
		return gen, nil
	}
	if !os.IsNotExist(err) {
		return domain.Generation{}, fmt.Errorf("existing generation directory %s is present but its manifest failed validation, refusing to overwrite a content-addressed path: %w", outputDir, err)
	}
	return Compile(req, outputDir)
}
