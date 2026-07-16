package qualify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// fingerprint is the byte-for-byte identity of one file: its mode and a
// content digest. Modeled directly on internal/plugin/conformance/snapshot.go
// (PR-05's zero-write proof): mtime is deliberately excluded from equality so
// a filesystem-timestamp-resolution artifact can never masquerade as (or
// hide) a real content change.
type fingerprint struct {
	mode   os.FileMode
	digest string
}

// snapshot is a full recursive inventory of one directory tree: relative
// path (slash-separated) -> fingerprint.
type snapshot map[string]fingerprint

// snapshotTree walks root and records a fingerprint for every regular file
// and directory underneath it. Unlike conformance's snapshotTree, a
// nonexistent root is not an error: it returns an empty snapshot, because
// this package's callers snapshot real host config paths
// (docs/architecture/runtime.md §7.1/§7.2) that may simply not exist on a
// given machine (e.g. no `/etc/codex`, or a host never installed). A root
// that is itself a regular file (not a directory) — several of
// RealHomePaths's entries are single files, e.g. `settings.json` — snapshots
// as one entry keyed "." rather than being silently skipped.
func snapshotTree(root string) (snapshot, error) {
	snap := make(snapshot)
	rootInfo, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return snap, nil
		}
		return nil, fmt.Errorf("snapshot %s: %w", root, err)
	}
	if !rootInfo.IsDir() {
		digest, err := digestFile(root)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", root, err)
		}
		snap["."] = fingerprint{mode: rootInfo.Mode(), digest: digest}
		return snap, nil
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			snap[rel] = fingerprint{mode: info.Mode()}
			return nil
		}
		digest, err := digestFile(path)
		if err != nil {
			return err
		}
		snap[rel] = fingerprint{mode: info.Mode(), digest: digest}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot %s: %w", root, err)
	}
	return snap, nil
}

func digestFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// diffSnapshots reports every difference between a before/after pair of
// snapshots of the same tree: files or directories added, removed, or
// changed in mode/content. An empty result means the tree was untouched.
// Never includes file content — only the path and the kind of change — so a
// diff of a directory that happens to hold secrets (a real ~/.claude.json)
// never leaks a value.
func diffSnapshots(before, after snapshot) []string {
	var diffs []string

	for path, beforeFp := range before {
		afterFp, ok := after[path]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("removed: %s", path))
			continue
		}
		if afterFp != beforeFp {
			diffs = append(diffs, fmt.Sprintf("changed: %s", path))
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			diffs = append(diffs, fmt.Sprintf("added: %s", path))
		}
	}

	sort.Strings(diffs)
	return diffs
}
