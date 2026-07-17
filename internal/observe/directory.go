package observe

import (
	"fmt"
	"path/filepath"
	"strings"
)

// directoryChain returns every directory strictly between worktreeRoot and
// workingDir, inclusive of workingDir itself but exclusive of worktreeRoot
// (which observeRoot already covers at `workspace` scope — see
// Observe's existing WorktreeRoot handling in request.go), in root-to-leaf
// order (matching docs/ontology/README.md §6.2's "closer instructions
// appear later" CONCAT_ORDERED direction, even though this package does not
// itself encode ordering into its output — see doc.go). workingDir equal to
// worktreeRoot returns an empty, non-error chain (nothing left to add beyond
// what workspace-scope observation already covers).
//
// Both arguments must already be absolute and workingDir must be
// worktreeRoot itself or a descendant of it — Observe validates this before
// calling, so directoryChain itself treats a violation as a caller
// (programmer) error via a returned error, not a silent empty result, per
// this package's "genuine, unexpected failure" error-vs-silent-skip
// distinction (request.go's Observe doc comment).
func directoryChain(worktreeRoot, workingDir string) ([]string, error) {
	root := filepath.Clean(worktreeRoot)
	leaf := filepath.Clean(workingDir)

	rel, err := filepath.Rel(root, leaf)
	if err != nil {
		return nil, fmt.Errorf("observe: directoryChain: %w", err)
	}
	if rel == "." {
		return nil, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("observe: directoryChain: WorkingDirectory %q is not WorktreeRoot %q or a descendant of it", workingDir, worktreeRoot)
	}

	segments := strings.Split(filepath.ToSlash(rel), "/")
	chain := make([]string, 0, len(segments))
	cur := root
	for _, seg := range segments {
		cur = filepath.Join(cur, seg)
		chain = append(chain, cur)
	}
	return chain, nil
}
