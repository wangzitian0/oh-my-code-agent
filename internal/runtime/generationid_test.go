package runtime

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// buildSimpleCodexRequest is a small, shared fixture for the GenerationID
// tests below: one repository AGENTS.md and one native config.toml, just
// enough for a non-trivial observation set.
func buildSimpleCodexRequest(t *testing.T, agentsContent string, now time.Time) BootstrapRequest {
	t.Helper()
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), agentsContent)
	mustWriteFile(t, filepath.Join(tr.CodexHome, "config.toml"), "[mcp_servers.demo]\ncommand = \"npx\"\n")

	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	return BootstrapRequest{
		Detection:    tr.detection("0.144.5"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          now,
	}
}

// TestGenerationID_DeterministicAcrossCalls is issue #13 AC #4, "Rebuilding
// from identical inputs yields the identical generation ID
// (content-addressed)": computing GenerationID twice from the same request
// values, and once more from a request that differs only in Now/Parent (an
// input GenerationID must NOT depend on), all agree.
func TestGenerationID_DeterministicAcrossCalls(t *testing.T) {
	req := buildSimpleCodexRequest(t, "# instructions\n", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	id1, err := GenerationID(req)
	if err != nil {
		t.Fatalf("GenerationID (1st): %v", err)
	}
	id2, err := GenerationID(req)
	if err != nil {
		t.Fatalf("GenerationID (2nd): %v", err)
	}
	if id1 != id2 {
		t.Fatalf("GenerationID is not deterministic across identical calls: %q != %q", id1, id2)
	}

	reqDifferentNow := req
	reqDifferentNow.Now = time.Date(2030, 12, 31, 23, 59, 59, 0, time.UTC)
	parent := "generation:some-earlier-generation-id"
	reqDifferentNow.Parent = &parent
	id3, err := GenerationID(reqDifferentNow)
	if err != nil {
		t.Fatalf("GenerationID (different Now/Parent): %v", err)
	}
	if id3 != id1 {
		t.Fatalf("GenerationID changed when only Now/Parent changed: %q != %q (Now and Parent must not affect the content-addressed ID)", id3, id1)
	}
}

// TestGenerationID_SensitiveToObservationContentChange proves GenerationID
// is not a constant function: changing one observed file's content (a
// different AGENTS.md body, which changes that Observation's RawDigest/
// ParsedDigest) must change the ID. Without this test, a buggy
// GenerationID that always returns the same digest would pass the
// determinism test above vacuously.
func TestGenerationID_SensitiveToObservationContentChange(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reqA := buildSimpleCodexRequest(t, "# instructions version A\n", now)
	reqB := buildSimpleCodexRequest(t, "# instructions version B, totally different\n", now)

	idA, err := GenerationID(reqA)
	if err != nil {
		t.Fatalf("GenerationID(A): %v", err)
	}
	idB, err := GenerationID(reqB)
	if err != nil {
		t.Fatalf("GenerationID(B): %v", err)
	}
	if idA == idB {
		t.Fatalf("GenerationID did not change when repository Instructions content changed: both %q", idA)
	}
}

// TestGenerationID_SensitiveToObservationCountChange proves adding a wholly
// new observed source (not just changing an existing one's content) also
// changes the ID -- covers a class of bug where only a fixed-size set of
// fields is hashed and an added/removed slice element is silently ignored.
func TestGenerationID_SensitiveToObservationCountChange(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")

	obsBefore, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe (before): %v", err)
	}
	reqBefore := BootstrapRequest{Detection: tr.detection("0.144.5"), Worktree: tr.worktree(t), Observations: obsBefore, Now: now}
	idBefore, err := GenerationID(reqBefore)
	if err != nil {
		t.Fatalf("GenerationID (before): %v", err)
	}

	mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", "extra", "SKILL.md"), "---\nname: extra\n---\nbody\n")
	obsAfter, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe (after): %v", err)
	}
	if len(obsAfter) != len(obsBefore)+1 {
		t.Fatalf("sanity check failed: expected exactly one new observation, got %d -> %d", len(obsBefore), len(obsAfter))
	}
	reqAfter := BootstrapRequest{Detection: tr.detection("0.144.5"), Worktree: tr.worktree(t), Observations: obsAfter, Now: now}
	idAfter, err := GenerationID(reqAfter)
	if err != nil {
		t.Fatalf("GenerationID (after): %v", err)
	}

	if idBefore == idAfter {
		t.Fatalf("GenerationID did not change when a new native source was discovered: both %q", idBefore)
	}
}

// TestGenerationID_SensitiveToHostVersion proves the host version is a real
// input, not just carried through unused. The same fixture tree is observed
// under two different versions (host version does not affect what Observe
// finds on disk -- both observation sets describe byte-identical content,
// only the recorded Spec.Host.Version differs, exactly as a real
// re-detection after a host upgrade would produce); only that stamped
// version, and Detection.Version to match it (BootstrapRequest.validate
// requires every observation's version to match what the generation is
// being compiled for -- see request.go), differ between the two requests.
func TestGenerationID_SensitiveToHostVersion(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")

	obsV1, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe(0.144.5): %v", err)
	}
	obsV2, err := observe.Observe(tr.request("0.145.0"))
	if err != nil {
		t.Fatalf("observe.Observe(0.145.0): %v", err)
	}

	wt := tr.worktree(t)
	reqV1 := BootstrapRequest{Detection: tr.detection("0.144.5"), Worktree: wt, Observations: obsV1, Now: now}
	reqV2 := BootstrapRequest{Detection: tr.detection("0.145.0"), Worktree: wt, Observations: obsV2, Now: now}

	idV1, err := GenerationID(reqV1)
	if err != nil {
		t.Fatalf("GenerationID(v1): %v", err)
	}
	idV2, err := GenerationID(reqV2)
	if err != nil {
		t.Fatalf("GenerationID(v2): %v", err)
	}
	if idV1 == idV2 {
		t.Fatalf("GenerationID did not change when host version changed: both %q", idV1)
	}
}

// TestGenerationID_SensitiveToWorktree proves the worktree identity is a
// real input: the same observation content, from two different worktree
// roots, must not collide.
func TestGenerationID_SensitiveToWorktree(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tr1 := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr1.WorktreeRoot, "AGENTS.md"), "# same content\n")
	obs1, err := observe.Observe(tr1.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe (tr1): %v", err)
	}

	tr2 := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr2.WorktreeRoot, "AGENTS.md"), "# same content\n")
	obs2, err := observe.Observe(tr2.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe (tr2): %v", err)
	}

	req1 := BootstrapRequest{Detection: tr1.detection("0.144.5"), Worktree: tr1.worktree(t), Observations: obs1, Now: now}
	req2 := BootstrapRequest{Detection: tr2.detection("0.144.5"), Worktree: tr2.worktree(t), Observations: obs2, Now: now}

	id1, err := GenerationID(req1)
	if err != nil {
		t.Fatalf("GenerationID(req1): %v", err)
	}
	id2, err := GenerationID(req2)
	if err != nil {
		t.Fatalf("GenerationID(req2): %v", err)
	}
	if id1 == id2 {
		t.Fatalf("GenerationID collided across two different worktrees with identical file content: both %q", id1)
	}
}
