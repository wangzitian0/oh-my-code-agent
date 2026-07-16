package conformance

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
// content digest. mtime is deliberately not part of the fingerprint's
// equality check inputs here — content hash plus mode is a stronger,
// filesystem-timestamp-resolution-independent proof that nothing wrote to
// the file, which is what the zero-write proof actually needs.
type fingerprint struct {
	mode   os.FileMode
	digest string
}

// snapshot is a full recursive inventory of one directory tree: relative
// path -> fingerprint.
type snapshot map[string]fingerprint

// snapshotTree walks root and records a fingerprint for every regular file
// and directory underneath it, keyed by slash-separated path relative to
// root. It is used to prove a HostAdapter.Observe call performed zero writes
// by comparing a snapshot taken before the call to one taken after.
func snapshotTree(root string) (snapshot, error) {
	snap := make(snapshot)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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
//
// This is the actual enforcement mechanism behind the zero-write proof: it
// is exercised directly (independent of any HostAdapter) in
// snapshot_test.go to prove it detects a real change, not just that it
// vacuously returns nothing for two empty snapshots.
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
