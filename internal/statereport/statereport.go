// Package statereport measures what host-written state an OMCA installation
// is actually holding, grouped by worktree and by the sharing class
// internal/auth already assigns it.
//
// It exists because a classification nothing measures is a claim nothing
// checks. internal/auth carries a careful table saying which host state may
// be shared how widely, and domain.MutableStateClass carries the vocabulary,
// but until something counts bytes against that table there is no way to
// notice that a class promising sharing is producing N independent copies
// instead -- which is exactly what was happening: one codex native home held
// 126 MB, ~97 MB of it recreatable cache and scratch that every other
// worktree was storing its own copy of.
//
// Read-only by construction: it stats and walks directories, and never opens
// a file, so no session content, credential or log line can reach its output.
// Sizes and names only.
package statereport

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/auth"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Entry is one classified directory or file inside one worktree's host home.
type Entry struct {
	Worktree string                   `json:"worktree"`
	Host     string                   `json:"host"`
	Name     string                   `json:"name"`
	Bytes    int64                    `json:"bytes"`
	Class    domain.MutableStateClass `json:"class"`
	// Classified is false when internal/auth's table has no row for this
	// path. Reported rather than guessed: an unclassified entry is a gap in
	// the table, and silently defaulting it to some class would hide that.
	Classified bool `json:"classified"`
}

// Duplication is one name that exists independently in more than one
// worktree while carrying a class that claims sharing.
//
// This is the finding the whole package exists to surface. A class only
// shares as far as some runtime is there to serve it (domain.RuntimeScope);
// when nothing applies the sharing, the class stays true on paper while the
// bytes are copied per checkout, and nothing in the system says so.
type Duplication struct {
	Host       string                   `json:"host"`
	Name       string                   `json:"name"`
	Class      domain.MutableStateClass `json:"class"`
	Copies     int                      `json:"copies"`
	TotalBytes int64                    `json:"totalBytes"`
	// WastedBytes is everything beyond the single copy the class says would
	// suffice. It is the concrete cost of the gap, not an estimate.
	WastedBytes int64 `json:"wastedBytes"`
}

// Result is one complete measurement of a state root.
type Result struct {
	StateRoot   string        `json:"stateRoot"`
	Worktrees   int           `json:"worktrees"`
	TotalBytes  int64         `json:"totalBytes"`
	ByClass     []ClassTotal  `json:"byClass"`
	Duplication []Duplication `json:"duplication"`
	Entries     []Entry       `json:"entries"`
}

// ClassTotal is one sharing class's share of the total.
type ClassTotal struct {
	Class      domain.MutableStateClass `json:"class"`
	Bytes      int64                    `json:"bytes"`
	Entries    int                      `json:"entries"`
	Classified bool                     `json:"classified"`
}

// Measure walks stateRoot (normally $XDG_STATE_HOME/omca) and classifies
// every host-home entry it finds under every worktree.
func Measure(stateRoot string) (Result, error) {
	if stateRoot == "" {
		return Result{}, fmt.Errorf("statereport: Measure: stateRoot is required")
	}
	out := Result{StateRoot: stateRoot}

	worktreeDirs, err := os.ReadDir(filepath.Join(stateRoot, "worktrees"))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil // nothing compiled yet is not an error
		}
		return Result{}, fmt.Errorf("statereport: Measure: %w", err)
	}

	tables := map[string][]auth.StateItem{}
	for _, wt := range worktreeDirs {
		if !wt.IsDir() {
			continue
		}
		out.Worktrees++
		hostsDir := filepath.Join(stateRoot, "worktrees", wt.Name(), "state", "hosts")
		hosts, hostErr := os.ReadDir(hostsDir)
		if hostErr != nil {
			continue // a worktree with no host state yet
		}
		for _, host := range hosts {
			if !host.IsDir() {
				continue
			}
			if _, ok := tables[host.Name()]; !ok {
				items, tableErr := auth.ClassificationTable(host.Name())
				if tableErr != nil {
					// An unknown host has no table; its entries are reported
					// unclassified rather than dropped.
					items = nil
				}
				tables[host.Name()] = items
			}
			homeDirs, _ := filepath.Glob(filepath.Join(hostsDir, host.Name(), "*", "*"))
			for _, home := range homeDirs {
				entries, readErr := os.ReadDir(home)
				if readErr != nil {
					continue
				}
				for _, e := range entries {
					size, sizeErr := dirSize(filepath.Join(home, e.Name()))
					if sizeErr != nil {
						continue
					}
					class, classified := classify(tables[host.Name()], e.Name())
					out.Entries = append(out.Entries, Entry{
						Worktree: wt.Name(), Host: host.Name(), Name: e.Name(),
						Bytes: size, Class: class, Classified: classified,
					})
					out.TotalBytes += size
				}
			}
		}
	}

	out.ByClass = totalsByClass(out.Entries)
	out.Duplication = findDuplication(out.Entries)
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Bytes > out.Entries[j].Bytes })
	return out, nil
}

// classify matches a directory entry against internal/auth's table, which is
// the single source for what a piece of host state is. Matching is on the
// table's own NativePath, with a trailing slash meaning "this directory".
func classify(items []auth.StateItem, name string) (domain.MutableStateClass, bool) {
	for _, it := range items {
		if strings.TrimSuffix(it.NativePath, "/") == name {
			return it.Class, true
		}
	}
	return "", false
}

func totalsByClass(entries []Entry) []ClassTotal {
	type key struct {
		class      domain.MutableStateClass
		classified bool
	}
	agg := map[key]*ClassTotal{}
	for _, e := range entries {
		k := key{e.Class, e.Classified}
		if agg[k] == nil {
			agg[k] = &ClassTotal{Class: e.Class, Classified: e.Classified}
		}
		agg[k].Bytes += e.Bytes
		agg[k].Entries++
	}
	out := make([]ClassTotal, 0, len(agg))
	for _, v := range agg {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

// findDuplication reports every (host, name) that carries a sharing class
// and yet exists independently in more than one worktree.
//
// Only sharing classes are considered: generation-local state is per-scope
// by definition, so multiple copies of it are the design working, not a
// defect. A sharing class with N copies is the opposite -- the class says
// one copy would serve, and N exist.
func findDuplication(entries []Entry) []Duplication {
	type key struct {
		host, name string
	}
	seen := map[key][]Entry{}
	for _, e := range entries {
		if !e.Classified || !e.Class.SharesAcrossGenerations() {
			continue
		}
		k := key{e.Host, e.Name}
		seen[k] = append(seen[k], e)
	}
	var out []Duplication
	for k, group := range seen {
		if len(group) < 2 {
			continue
		}
		var total, largest int64
		for _, e := range group {
			total += e.Bytes
			if e.Bytes > largest {
				largest = e.Bytes
			}
		}
		out = append(out, Duplication{
			Host: k.host, Name: k.name, Class: group[0].Class,
			Copies: len(group), TotalBytes: total, WastedBytes: total - largest,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WastedBytes > out[j].WastedBytes })
	return out
}

// dirSize sums the apparent size of every regular file under path. It never
// opens a file and never follows a symlink out of the tree: a symlinked
// share counts as the link itself, not as another copy of its target, which
// is what makes a successfully shared entry visibly cheap here.
func dirSize(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, nil
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal
		}
		if d.IsDir() {
			return nil
		}
		fi, statErr := d.Info()
		if statErr != nil || fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		total += fi.Size()
		return nil
	})
	return total, err
}
