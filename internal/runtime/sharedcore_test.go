package runtime

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestSharedCompilerCore_BootstrapAndCompileUseSameCore is issue #18's
// round-2 MECE requirement made mechanically checkable: "one compiler, two
// entry points... bootstrap is the minimal-input case, not a second
// implementation." It swaps compileHostTreeFn (compile.go) for a
// call-counting wrapper around the real compileHostTree, runs one Bootstrap
// call and one Compile call, and asserts the wrapper was invoked exactly
// twice -- once per entry point, through the identical function value. If a
// future change forked Compile's tree-walking logic into its own copy
// (rather than routing through compileHostTreeFn like Bootstrap already
// does), this test's call count would stop matching 2, independent of
// whether anyone remembered to update any doc comment describing the
// "shared core" design.
func TestSharedCompilerCore_BootstrapAndCompileUseSameCore(t *testing.T) {
	original := compileHostTreeFn
	t.Cleanup(func() { compileHostTreeFn = original })

	var calls int
	compileHostTreeFn = func(in hostTreeInput) ([]generatedFile, []domain.GenerationSourceEntry, error) {
		calls++
		return original(in)
	}

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	trBootstrap := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(trBootstrap.WorktreeRoot, "AGENTS.md"), "# bootstrap instructions\n")
	obsBootstrap, err := observe.Observe(trBootstrap.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe (bootstrap fixture): %v", err)
	}
	bootstrapReq := BootstrapRequest{
		Detection:    trBootstrap.detection("0.144.5"),
		Worktree:     trBootstrap.worktree(t),
		Observations: obsBootstrap,
		Now:          now,
	}
	bootstrapDir := filepath.Join(t.TempDir(), "generation")
	if _, err := Bootstrap(bootstrapReq, bootstrapDir); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, bootstrapDir)
	if calls != 1 {
		t.Fatalf("after Bootstrap, compileHostTreeFn was called %d times, want 1", calls)
	}

	compileReq := buildSimpleCompileRequest(t, "# compile instructions\n", nil, domain.Activation{}, nil, now)
	compileDir := filepath.Join(t.TempDir(), "generation")
	if _, err := Compile(compileReq, compileDir); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, compileDir)
	if calls != 2 {
		t.Fatalf("after Bootstrap (1) + Compile (1), compileHostTreeFn was called %d times total, want 2 -- Bootstrap and Compile must route through the identical shared compiler core (issue #18 round-2 MECE requirement)", calls)
	}
}
