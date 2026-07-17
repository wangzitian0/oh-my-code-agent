package runtime

import (
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// SetPendingGeneration records generationDir as host's "pending" generation
// under worktreeStateDir: a symlink at worktreeStateDir/pending/<host>
// pointing at generationDir, plus a CurrentRecord JSON sidecar -- the exact
// same two-file shape SetCurrentGeneration (current.go) already uses for
// "current" (setGenerationPointer is the shared implementation both call),
// reused here rather than inventing a second record shape: "pending" and
// "current" record exactly the same kind of fact (which generation,
// compiled against which host binary/version, at what time); they differ
// only in which pointer they name.
//
// docs/architecture/runtime.md §5's layout diagram shows `pending ->
// generations/<generation-id>` as a sibling of `current` and `ledger/`
// under worktree state; §5.3 describes what a pending generation's manifest
// carries (domain.Generation's now-extended field list, see
// internal/domain/generation.go); §5.4 describes what happens NEXT --
// "validate pending -> ensure source digests still match -> ... ->
// atomically switch current -> ... -> append Ledger entry" -- an atomic,
// crash-safe, compare-and-swap transaction from pending to current. THIS
// function does not implement that transaction: it is a plain, synchronous
// write of the pending pointer, with no comparison against "current," no
// atomicity guarantee across the pending-and-current pair (only within each
// pointer's own two files, via setGenerationPointer's rename-into-place),
// and no rollback path. Building that transaction -- the "CAS, atomic
// switch, rollback" issue #18 itself names as PR-15's job, not this one's --
// is explicitly out of scope here. A caller in this PR's own test suite (or
// a future PR-15 that has not landed yet) is expected to call
// SetPendingGeneration after Compile returns, and, separately and later,
// whatever PR-15 builds to actually activate it.
func SetPendingGeneration(worktreeStateDir, host, generationDir string, gen domain.Generation, detection hostcontext.HostDetection, now time.Time) error {
	return setGenerationPointer(worktreeStateDir, "pending", host, generationDir, gen, detection, now)
}

// PendingGenerationDir resolves host's "pending" generation directory under
// worktreeStateDir (SetPendingGeneration's symlink target), returning an
// absolute, cleaned path. A caller that finds no pointer at all (this host
// has never had SetPendingGeneration called for it in this worktree state
// dir, or a prior pending generation was already activated and the pointer
// was never re-set -- PR-15's job to clear/replace, not this function's) gets
// an os.IsNotExist-satisfying error, exactly like CurrentGenerationDir's
// identical contract for "current."
func PendingGenerationDir(worktreeStateDir, host string) (string, error) {
	return generationPointerDir(worktreeStateDir, "pending", host)
}

// ReadPendingRecord reads the CurrentRecord sidecar SetPendingGeneration
// wrote for host under worktreeStateDir.
func ReadPendingRecord(worktreeStateDir, host string) (CurrentRecord, error) {
	return readPointerRecord(worktreeStateDir, "pending", host)
}
