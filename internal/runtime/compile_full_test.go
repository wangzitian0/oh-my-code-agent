package runtime

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// buildSimpleCompileRequest is compile_full_test.go's analogue of
// generationid_test.go's buildSimpleCodexRequest: one repository AGENTS.md,
// one codex host, and caller-supplied desired-state inputs, just enough for
// a non-trivial CompileRequest.
func buildSimpleCompileRequest(t *testing.T, agentsContent string, profiles []domain.Profile, activation domain.Activation, exceptions []domain.Exception, now time.Time) CompileRequest {
	t.Helper()
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), agentsContent)
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	return CompileRequest{
		Worktree: tr.worktree(t),
		Hosts: []HostCompileInput{
			{Detection: tr.detection("0.144.5"), Observations: obs},
		},
		Profiles:   profiles,
		Activation: activation,
		Exceptions: exceptions,
		Now:        now,
	}
}

// sandboxProfile builds a minimal, valid Profile carrying exactly one
// policy.permissions entry ("sandbox"), matching docs/product/requirements.md
// §4.1's own worked example shape.
func sandboxProfile(id string, intent domain.Intent, value string) domain.Profile {
	return domain.Profile{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Profile",
		Metadata:   domain.Metadata{ID: id},
		Spec: domain.ProfileSpec{
			Policy: domain.ProfilePolicy{
				Permissions: map[string]domain.PermissionRef{
					"sandbox": {Intent: intent, Value: value},
				},
			},
		},
	}
}

// TestCompileGenerationID_DeterministicAcrossCalls is issue #18's own
// determinism AC, the full-compilation analogue of generationid_test.go's
// TestGenerationID_DeterministicAcrossCalls: computing CompileGenerationID
// twice from the same request, and once more from a request that differs
// only in Now/Parent/Invocation (none of which changes the generated
// artifact tree's content), all agree.
func TestCompileGenerationID_DeterministicAcrossCalls(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "workspace-write")}
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)

	id1, err := CompileGenerationID(req)
	if err != nil {
		t.Fatalf("CompileGenerationID (1st): %v", err)
	}
	id2, err := CompileGenerationID(req)
	if err != nil {
		t.Fatalf("CompileGenerationID (2nd): %v", err)
	}
	if id1 != id2 {
		t.Fatalf("CompileGenerationID is not deterministic across identical calls: %q != %q", id1, id2)
	}

	reqDiffering := req
	reqDiffering.Now = time.Date(2030, 12, 31, 23, 59, 59, 0, time.UTC)
	parent := "generation:some-earlier-generation-id"
	reqDiffering.Parent = &parent
	reqDiffering.Invocation = "omca run codex"
	id3, err := CompileGenerationID(reqDiffering)
	if err != nil {
		t.Fatalf("CompileGenerationID (different Now/Parent/Invocation): %v", err)
	}
	if id3 != id1 {
		t.Fatalf("CompileGenerationID changed when only Now/Parent/Invocation changed: %q != %q", id3, id1)
	}
}

// TestCompileGenerationID_SensitiveToProfileChange proves the desired-state
// input is a real, digested input: changing a Profile's permission value
// changes the ID. Without this, a buggy CompileGenerationID that ignored
// req.Profiles entirely would pass the determinism test above vacuously.
func TestCompileGenerationID_SensitiveToProfileChange(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	profilesA := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "workspace-write")}
	profilesB := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "read-only")}

	reqA := buildSimpleCompileRequest(t, "# instructions\n", profilesA, domain.Activation{}, nil, now)
	reqB := buildSimpleCompileRequest(t, "# instructions\n", profilesB, domain.Activation{}, nil, now)

	idA, err := CompileGenerationID(reqA)
	if err != nil {
		t.Fatalf("CompileGenerationID(A): %v", err)
	}
	idB, err := CompileGenerationID(reqB)
	if err != nil {
		t.Fatalf("CompileGenerationID(B): %v", err)
	}
	if idA == idB {
		t.Fatalf("CompileGenerationID did not change when the Profile's permission value changed: both %q", idA)
	}
}

// TestCompileGenerationID_SensitiveToKnowledgePacks proves Knowledge Pack
// digests are a real input to the generation ID, matching issue #18's AC
// "same desired state + same Knowledge digests produce the identical
// generation digest" read in its sensitivity direction: a different
// Knowledge digest for an otherwise-identical request must NOT collide.
func TestCompileGenerationID_SensitiveToKnowledgePacks(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "workspace-write")}

	reqA := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)
	reqA.KnowledgePacks = []domain.KnowledgePackRef{{ID: "codex:cli:0.144", Digest: "sha256:" + strings.Repeat("a", 64)}}

	reqB := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)
	reqB.KnowledgePacks = []domain.KnowledgePackRef{{ID: "codex:cli:0.144", Digest: "sha256:" + strings.Repeat("b", 64)}}

	idA, err := CompileGenerationID(reqA)
	if err != nil {
		t.Fatalf("CompileGenerationID(A): %v", err)
	}
	idB, err := CompileGenerationID(reqB)
	if err != nil {
		t.Fatalf("CompileGenerationID(B): %v", err)
	}
	if idA == idB {
		t.Fatalf("CompileGenerationID did not change when the Knowledge Pack digest changed: both %q", idA)
	}
}

// TestCompileGenerationID_SensitiveToExceptionExpiry is a regression test
// for a real Copilot review finding on this PR: CompileGenerationID used to
// exclude Now entirely, but resolve.Resolve's actual output depends on Now
// through each Exception's now.Before(ExpiresAt) liveness check -- so two
// Compile calls with identical Profiles/Activation/Exceptions, differing
// only in Now landing on either side of an Exception's expiry, could
// produce different compiled Sources/artifacts under the SAME generation
// ID, breaking content-addressing's core guarantee ("same ID implies same
// content"). This proves the fix: a Now before expiry and a Now after
// expiry produce DIFFERENT IDs for the same Exception.
func TestCompileGenerationID_SensitiveToExceptionExpiry(t *testing.T) {
	profiles := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "workspace-write")}
	exceptions := []domain.Exception{
		{AssetID: "code-review", Scope: "company:example", Justification: "temporary", ExpiresAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
	}

	beforeExpiry := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	afterExpiry := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// One fixture/worktree, built once (buildSimpleCompileRequest's
	// t.TempDir()-derived Worktree.ID is otherwise a second, uncontrolled
	// variable between two separate calls) -- reqAfter is a copy of
	// reqBefore with ONLY Now changed, matching
	// TestCompileGenerationID_DeterministicAcrossCalls's established
	// "reqDiffering := req; reqDiffering.Now = ..." pattern exactly.
	reqBefore := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, exceptions, beforeExpiry)
	reqAfter := reqBefore
	reqAfter.Now = afterExpiry

	idBefore, err := CompileGenerationID(reqBefore)
	if err != nil {
		t.Fatalf("CompileGenerationID(before expiry): %v", err)
	}
	idAfter, err := CompileGenerationID(reqAfter)
	if err != nil {
		t.Fatalf("CompileGenerationID(after expiry): %v", err)
	}
	if idBefore == idAfter {
		t.Fatalf("CompileGenerationID did not change when the Exception's live/expired status flipped: both %q", idBefore)
	}
}

// TestCompileGenerationID_StableAcrossNowNotCrossingAnyExpiry is
// TestCompileGenerationID_SensitiveToExceptionExpiry's negative control:
// two different Now values that both land on the SAME side of every
// Exception's expiry (here, no exceptions at all, matching
// TestCompileGenerationID_DeterministicAcrossCalls's existing "Now doesn't
// matter" case) must still produce the identical ID -- proving the fix
// folds in each exception's live/expired *classification*, not raw Now
// itself, which would have overcorrected into "the ID always changes when
// Now changes" and broken reproducibility for no real content difference.
func TestCompileGenerationID_StableAcrossNowNotCrossingAnyExpiry(t *testing.T) {
	profiles := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "workspace-write")}
	exceptions := []domain.Exception{
		{AssetID: "code-review", Scope: "company:example", Justification: "temporary", ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	now1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // both still well before the exception's 2030 expiry

	// One fixture/worktree, built once -- see
	// TestCompileGenerationID_SensitiveToExceptionExpiry's identical note on
	// why calling buildSimpleCompileRequest twice would confound the result
	// with two different Worktree.IDs.
	req1 := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, exceptions, now1)
	req2 := req1
	req2.Now = now2

	id1, err := CompileGenerationID(req1)
	if err != nil {
		t.Fatalf("CompileGenerationID(now1): %v", err)
	}
	id2, err := CompileGenerationID(req2)
	if err != nil {
		t.Fatalf("CompileGenerationID(now2): %v", err)
	}
	if id1 != id2 {
		t.Fatalf("CompileGenerationID changed even though no exception's live/expired status changed: %q != %q", id1, id2)
	}
}

// TestCompile_RebuildingIntoFreshOutputDir_YieldsIdenticalID is the
// end-to-end version of the determinism AC (matching bootstrap_test.go's
// TestBootstrap_RebuildingIntoFreshOutputDir_YieldsIdenticalID): compiling
// the identical CompileRequest twice, into two different fresh output
// directories, yields the identical Generation.metadata.id and identical
// artifact digests -- true content-addressing, not just an ID-in-isolation
// property.
func TestCompile_RebuildingIntoFreshOutputDir_YieldsIdenticalID(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "workspace-write")}
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)

	dir1 := filepath.Join(t.TempDir(), "generation")
	gen1, err := Compile(req, dir1)
	if err != nil {
		t.Fatalf("Compile (1st): %v", err)
	}
	restoreWritable(t, dir1)

	dir2 := filepath.Join(t.TempDir(), "generation")
	gen2, err := Compile(req, dir2)
	if err != nil {
		t.Fatalf("Compile (2nd): %v", err)
	}
	restoreWritable(t, dir2)

	if gen1.Metadata.ID != gen2.Metadata.ID {
		t.Fatalf("rebuilding from identical inputs produced different generation IDs: %q != %q", gen1.Metadata.ID, gen2.Metadata.ID)
	}
	if gen1.Spec.DesiredGraphDigest != gen2.Spec.DesiredGraphDigest {
		t.Errorf("desiredGraphDigest differs across identical rebuilds: %q != %q", gen1.Spec.DesiredGraphDigest, gen2.Spec.DesiredGraphDigest)
	}
	if gen1.Spec.SourceDigest != gen2.Spec.SourceDigest {
		t.Errorf("sourceDigest differs across identical rebuilds: %q != %q", gen1.Spec.SourceDigest, gen2.Spec.SourceDigest)
	}

	artifacts1 := gen1.Spec.Hosts["codex"].Artifacts
	artifacts2 := gen2.Spec.Hosts["codex"].Artifacts
	if len(artifacts1) != len(artifacts2) {
		t.Fatalf("artifact count differs across identical rebuilds: %d != %d", len(artifacts1), len(artifacts2))
	}
	byPath1 := make(map[string]string, len(artifacts1))
	for _, a := range artifacts1 {
		byPath1[a.Path] = a.Digest
	}
	for _, a := range artifacts2 {
		if byPath1[a.Path] != a.Digest {
			t.Errorf("artifact %s digest differs across identical rebuilds: %q != %q", a.Path, byPath1[a.Path], a.Digest)
		}
	}
}

// TestCompile_Permission_DENIED_NeverWeakened is issue #18's round-2 golden
// case: "policy.permissions values compile into host artifacts where the
// capability level allows; DENY/lock intent is never weakened by
// compilation." A Profile DENIES the codex sandbox permission at value
// "danger-full-access" (a deliberately dangerous, deliberately-not-the-
// conservative-default value): the compiled config.toml must never contain
// that value, must keep the conservative "read-only" default, and the
// manifest must record why.
func TestCompile_Permission_DENIED_NeverWeakened(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{sandboxProfile("company:lockdown", domain.IntentDenied, "danger-full-access")}
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)

	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	tree := walkGeneratedTree(t, outputDir)
	configTOML := string(tree[filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")])
	if configTOML == "" {
		t.Fatal("no config.toml was generated")
	}
	if strings.Contains(configTOML, "danger-full-access") {
		t.Fatalf("DENIED permission value leaked into the compiled artifact -- compilation weakened a deny:\n%s", configTOML)
	}
	if !strings.Contains(configTOML, `sandbox_mode = "read-only"`) {
		t.Errorf("compiled artifact did not keep the conservative default sandbox_mode = \"read-only\":\n%s", configTOML)
	}

	found := false
	for _, s := range gen.Spec.Sources {
		if s.Concept == "permission" && s.Source == "sandbox" {
			found = true
			if s.Included {
				t.Errorf("permission source entry has Included=true, want false (DENIED must never be recorded as included): %+v", s)
			}
			if !strings.Contains(s.Reason, "DENIED") {
				t.Errorf("permission source entry reason does not mention DENIED: %q", s.Reason)
			}
		}
	}
	if !found {
		t.Fatal("no permission source entry was recorded for the DENIED sandbox permission")
	}
}

// TestCompile_Permission_REQUIRED_Honored proves the positive half of the
// same AC: a REQUIRED permission value this compiler recognizes IS compiled
// into the artifact ("lock intent is never weakened" cuts both ways -- a
// REQUIRED value must not be silently dropped either).
func TestCompile_Permission_REQUIRED_Honored(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{sandboxProfile("company:example", domain.IntentRequired, "workspace-write")}
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)

	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	tree := walkGeneratedTree(t, outputDir)
	configTOML := string(tree[filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")])
	if !strings.Contains(configTOML, `sandbox_mode = "workspace-write"`) {
		t.Errorf("compiled artifact did not honor the REQUIRED permission value:\n%s", configTOML)
	}

	found := false
	for _, s := range gen.Spec.Sources {
		if s.Concept == "permission" && s.Source == "sandbox" {
			found = true
			if !s.Included {
				t.Errorf("permission source entry has Included=false, want true for a recognized REQUIRED value: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("no permission source entry was recorded for the REQUIRED sandbox permission")
	}
}

// TestCompile_Permission_UnrecognizedValue_KeepsConservativeDefault proves
// "where the capability level allows": a permission value this compiler
// does not recognize is never guessed at -- it falls back to the
// conservative default, explained in the manifest, rather than either
// silently dropped or blindly written into the native config.
func TestCompile_Permission_UnrecognizedValue_KeepsConservativeDefault(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "some-future-mode-this-compiler-does-not-know")}
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)

	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	tree := walkGeneratedTree(t, outputDir)
	configTOML := string(tree[filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")])
	if strings.Contains(configTOML, "some-future-mode-this-compiler-does-not-know") {
		t.Fatalf("unrecognized permission value was written verbatim into the compiled artifact:\n%s", configTOML)
	}
	if !strings.Contains(configTOML, `sandbox_mode = "read-only"`) {
		t.Errorf("compiled artifact did not keep the conservative default for an unrecognized value:\n%s", configTOML)
	}
	foundExcluded := false
	for _, s := range gen.Spec.Sources {
		if s.Concept == "permission" && s.Source == "sandbox" && !s.Included {
			foundExcluded = true
		}
	}
	if !foundExcluded {
		t.Fatal("no excluded permission source entry was recorded for the unrecognized value")
	}
}

// TestCompile_NoPermissions_MatchesBootstrapConservativeDefault proves
// Compile with no policy.permissions at all (an empty Profile list, or
// Profiles that never mention "sandbox") renders exactly the same
// conservative default Bootstrap always has, and records zero permission
// source entries -- the resolveSandboxPermission "nil/empty permissions map"
// contract compile.go documents.
func TestCompile_NoPermissions_MatchesBootstrapConservativeDefault(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	req := buildSimpleCompileRequest(t, "# instructions\n", nil, domain.Activation{}, nil, now)

	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	tree := walkGeneratedTree(t, outputDir)
	configTOML := string(tree[filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")])
	if !strings.Contains(configTOML, `sandbox_mode = "read-only"`) {
		t.Errorf("compiled artifact did not keep the conservative default with no permission policy:\n%s", configTOML)
	}
	for _, s := range gen.Spec.Sources {
		if s.Concept == "permission" {
			t.Errorf("unexpected permission source entry with no policy.permissions supplied at all: %+v", s)
		}
	}
}

// TestCompile_MultiHost_OneSharedGeneration proves this PR's one-Generation-
// multiple-Hosts design decision (see compile_full.go's own doc comment):
// compiling codex and claude-code together in one CompileRequest produces
// ONE Generation with two Spec.Hosts entries and one shared manifest.json,
// not two separate generations.
func TestCompile_MultiHost_OneSharedGeneration(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "project")
	mustWriteFile(t, filepath.Join(worktreeRoot, "AGENTS.md"), "# codex instructions\n")
	mustWriteFile(t, filepath.Join(worktreeRoot, "CLAUDE.md"), "# claude instructions\n")

	codexDetection := hostcontext.HostDetection{
		Host: "codex", Surface: "cli", Version: "0.144.5",
		NativeHomes: []hostcontext.NativeHome{{Name: "CODEX_HOME", Path: filepath.Join(root, "codex-home"), FromEnvVar: "CODEX_HOME"}},
	}
	claudeDetection := hostcontext.HostDetection{
		Host: "claude-code", Surface: "cli", Version: "2.1.211",
		NativeHomes: []hostcontext.NativeHome{{Name: "CLAUDE_CONFIG_DIR", Path: filepath.Join(root, "claude-config"), FromEnvVar: "CLAUDE_CONFIG_DIR"}},
	}

	obsCodex, err := observe.Observe(observe.Request{Detection: codexDetection, WorktreeRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("observe.Observe (codex): %v", err)
	}
	obsClaude, err := observe.Observe(observe.Request{Detection: claudeDetection, WorktreeRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("observe.Observe (claude): %v", err)
	}

	req := CompileRequest{
		Worktree: hostcontext.Worktree{ID: worktreeIDFor(t, worktreeRoot), Root: worktreeRoot},
		Hosts: []HostCompileInput{
			{Detection: codexDetection, Observations: obsCodex},
			{Detection: claudeDetection, Observations: obsClaude},
		},
		Now: now,
	}

	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	if len(gen.Spec.Hosts) != 2 {
		t.Fatalf("Spec.Hosts has %d entries, want 2 (one shared Generation for both hosts)", len(gen.Spec.Hosts))
	}
	if _, ok := gen.Spec.Hosts["codex"]; !ok {
		t.Error("Spec.Hosts is missing a codex entry")
	}
	if _, ok := gen.Spec.Hosts["claude-code"]; !ok {
		t.Error("Spec.Hosts is missing a claude-code entry")
	}

	tree := walkGeneratedTree(t, outputDir)
	if _, ok := tree[filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")]; !ok {
		t.Errorf("expected hosts/codex/cli/codex-home/config.toml, got %v", keysOf(tree))
	}
	if _, ok := tree[filepath.Join("hosts", "claude-code", "cli", "claude-config", "settings.json")]; !ok {
		t.Errorf("expected hosts/claude-code/cli/claude-config/settings.json, got %v", keysOf(tree))
	}

	manifestCount := 0
	for path := range tree {
		if path == "manifest.json" {
			manifestCount++
		}
	}
	if manifestCount != 1 {
		t.Errorf("found %d manifest.json files, want exactly 1 (one shared generation directory)", manifestCount)
	}

	if err := domain.ValidateGeneration(gen); err != nil {
		t.Fatalf("ValidateGeneration: %v", err)
	}
}

// TestCompile_ResolvedAssetSources_RecordedWithReasonAndIntent is issue #18's
// own text made concrete: "compile every Active: true asset into that
// host's generation... recording Reason/Intent into
// GenerationSourceEntry.Reason the same way compileHostTree already does
// for the bootstrap policy's own reasons." A Profile REQUIRING a skill (no
// matching Observation exists -- there is no Identity Matcher yet, see
// compile_full.go's doc comment) still shows up in Spec.Sources, Included
// and carrying its resolved Reason/Intent.
func TestCompile_ResolvedAssetSources_RecordedWithReasonAndIntent(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{
		{
			APIVersion: domain.SupportedAPIVersion,
			Kind:       "Profile",
			Metadata:   domain.Metadata{ID: "company:example"},
			Spec: domain.ProfileSpec{
				Assets: domain.ProfileAssets{
					Skills: []domain.AssetRef{{ID: "code-review", Intent: domain.IntentRequired}},
				},
			},
		},
	}
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)

	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	found := false
	for _, s := range gen.Spec.Sources {
		if s.Concept == "skill" && s.Source == "code-review" {
			found = true
			if !s.Included {
				t.Errorf("resolved REQUIRED skill source entry has Included=false, want true: %+v", s)
			}
			if !strings.Contains(s.Reason, "REQUIRED") {
				t.Errorf("resolved skill source entry reason does not mention REQUIRED: %q", s.Reason)
			}
		}
	}
	if !found {
		t.Fatal("no resolved desired-state source entry was recorded for the REQUIRED skill \"code-review\"")
	}
}

// TestCompile_DesiredState_NamesProfilesAndActivation proves
// Spec.DesiredState -- docs/architecture/runtime.md §5.3's "selected
// Profiles and Activation" pending-manifest field -- actually names the
// input Profile(s) by ID and digest, unlike Bootstrap, which always leaves
// this nil (no real Desired Graph).
func TestCompile_DesiredState_NamesProfilesAndActivation(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "workspace-write")}
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)

	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	if gen.Spec.DesiredState == nil {
		t.Fatal("Spec.DesiredState is nil, want a populated DesiredStateRef")
	}
	if len(gen.Spec.DesiredState.Profiles) != 1 {
		t.Fatalf("Spec.DesiredState.Profiles has %d entries, want 1", len(gen.Spec.DesiredState.Profiles))
	}
	if gen.Spec.DesiredState.Profiles[0].ID != "company:example" {
		t.Errorf("Spec.DesiredState.Profiles[0].ID = %q, want %q", gen.Spec.DesiredState.Profiles[0].ID, "company:example")
	}
	if !domain.IsCanonicalDigest(gen.Spec.DesiredState.Profiles[0].Digest) {
		t.Errorf("Spec.DesiredState.Profiles[0].Digest %q is not a canonical digest", gen.Spec.DesiredState.Profiles[0].Digest)
	}
}

// TestCompile_BootstrapDesiredGraphDigest_DiffersFromCompile proves Compile
// really does compute a real Desired Graph digest, distinct from
// Bootstrap's fixed BootstrapPolicyDigest placeholder -- the exact
// distinction doc.go's "why desiredGraphDigest is a bootstrap-policy
// digest" section documents.
func TestCompile_BootstrapDesiredGraphDigest_DiffersFromCompile(t *testing.T) {
	bootstrapDigest, err := BootstrapPolicyDigest()
	if err != nil {
		t.Fatalf("BootstrapPolicyDigest: %v", err)
	}

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	profiles := []domain.Profile{sandboxProfile("company:example", domain.IntentDefault, "workspace-write")}
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)
	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	if gen.Spec.DesiredGraphDigest == bootstrapDigest {
		t.Fatalf("Compile's desiredGraphDigest equals BootstrapPolicyDigest(); Compile must digest the real Desired Graph, not the bootstrap placeholder")
	}
	if !domain.IsCanonicalDigest(gen.Spec.DesiredGraphDigest) {
		t.Errorf("desiredGraphDigest %q is not a canonical sha256 digest", gen.Spec.DesiredGraphDigest)
	}
}

// TestSourceEntryFingerprint_DiffersOnScopeAndCapabilityGap is a regression
// test for a real Copilot review finding on this PR: sourceEntryFingerprint
// (and, through it, Generation.spec.sourceDigest) used to be computed from
// only Concept/Source/Included/Reason, silently omitting Scope/
// CapabilityGap/TrackingIssue -- two entries identical in the covered
// fields but differing in an omitted one would have produced the same
// fingerprint. This proves each omitted field, changed alone, changes the
// fingerprint.
func TestSourceEntryFingerprint_DiffersOnScopeAndCapabilityGap(t *testing.T) {
	base := domain.GenerationSourceEntry{
		Concept:  "mcp_server",
		Source:   "/native/.claude.json",
		Included: false,
		Reason:   "excluded: native user-global source",
	}

	baseFP, err := sourceEntryFingerprint("claude-code", base)
	if err != nil {
		t.Fatalf("sourceEntryFingerprint(base): %v", err)
	}

	scopeChanged := base
	scopeChanged.Scope = "user"
	scopeFP, err := sourceEntryFingerprint("claude-code", scopeChanged)
	if err != nil {
		t.Fatalf("sourceEntryFingerprint(scopeChanged): %v", err)
	}
	if scopeFP == baseFP {
		t.Error("sourceEntryFingerprint did not change when Scope changed (base has no Scope, scopeChanged sets it to \"user\")")
	}

	gapChanged := base
	gapChanged.CapabilityGap = true
	gapChanged.TrackingIssue = "https://github.com/wangzitian0/oh-my-code-agent/issues/47"
	gapFP, err := sourceEntryFingerprint("claude-code", gapChanged)
	if err != nil {
		t.Fatalf("sourceEntryFingerprint(gapChanged): %v", err)
	}
	if gapFP == baseFP {
		t.Error("sourceEntryFingerprint did not change when CapabilityGap/TrackingIssue changed")
	}
	if gapFP == scopeFP {
		t.Error("sourceEntryFingerprint collided between a Scope-only change and a CapabilityGap-only change")
	}
}

// TestAggregateSources_HostNeutralTie_OrderIndependentOfCallerHostOrder
// proves the review-found fix: a host-neutral asset (no `hosts:` selector)
// produces an identical (Concept, Source, Reason) tuple on every host in a
// multi-host generation -- a guaranteed tie the comparator's first three
// keys never break. sort.Slice is not stable, so without Host as a final
// tiebreaker, that tie's relative position in the generation-wide Sources
// list depended on which host happened to appear first in the caller's
// perHost slice, breaking this codebase's otherwise-universal "shuffle
// input order, get byte-identical output" determinism guarantee for the
// human/audit-facing manifest (SourceDigest itself was never affected --
// see sourceEntryFingerprint, folded through a separately sorted list).
func TestAggregateSources_HostNeutralTie_OrderIndependentOfCallerHostOrder(t *testing.T) {
	tied := domain.GenerationSourceEntry{
		Concept: "instruction", Source: "company:example/security-default",
		Included: true, Reason: "included: company baseline",
	}
	codexOnly := domain.GenerationSourceEntry{
		Concept: "mcp_server", Source: "codex-only-server", Included: true, Reason: "included",
	}

	codexFirst := []hostSourceEntry{
		{Host: "codex", Sources: []domain.GenerationSourceEntry{withHost(codexOnly, "codex"), withHost(tied, "codex")}},
		{Host: "claude-code", Sources: []domain.GenerationSourceEntry{withHost(tied, "claude-code")}},
	}
	claudeFirst := []hostSourceEntry{
		{Host: "claude-code", Sources: []domain.GenerationSourceEntry{withHost(tied, "claude-code")}},
		{Host: "codex", Sources: []domain.GenerationSourceEntry{withHost(codexOnly, "codex"), withHost(tied, "codex")}},
	}

	sourcesA, digestA, err := aggregateSources(codexFirst)
	if err != nil {
		t.Fatalf("aggregateSources(codex-first): %v", err)
	}
	sourcesB, digestB, err := aggregateSources(claudeFirst)
	if err != nil {
		t.Fatalf("aggregateSources(claude-first): %v", err)
	}

	if digestA != digestB {
		t.Errorf("sourceDigest differs by caller host order: %q vs %q (should be order-independent regardless of this fix)", digestA, digestB)
	}
	if len(sourcesA) != len(sourcesB) {
		t.Fatalf("got %d vs %d sources", len(sourcesA), len(sourcesB))
	}
	for i := range sourcesA {
		// Compare the whole struct, not just Host/Source: GenerationSourceEntry
		// is entirely comparable (string/bool fields only), and limiting the
		// assertion to two fields would miss an ordering-instability bug that
		// happened to leave Concept/Reason/Included/Scope/CapabilityGap/
		// TrackingIssue mismatched while Host/Source coincidentally matched.
		if sourcesA[i] != sourcesB[i] {
			t.Errorf("Sources[%d] differs by caller host order:\n  %+v\nvs\n  %+v", i, sourcesA[i], sourcesB[i])
		}
	}
}

func withHost(e domain.GenerationSourceEntry, host string) domain.GenerationSourceEntry {
	e.Host = host
	return e
}
