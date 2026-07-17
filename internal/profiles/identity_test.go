package profiles

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestResolveIdentities_Unambiguous(t *testing.T) {
	res := ResolveIdentities([]string{"personal:alice", "company:example", "team:payments", "project:order-service"})
	if res.IsAmbiguous() {
		t.Fatalf("expected no ambiguity, got %+v", res.Ambiguous)
	}
	want := map[string]string{
		"personal": "personal:alice",
		"company":  "company:example",
		"team":     "team:payments",
		"project":  "project:order-service",
	}
	if !reflect.DeepEqual(res.Resolved, want) {
		t.Errorf("Resolved = %v, want %v", res.Resolved, want)
	}
}

// TestResolveIdentities_DuplicateIDCollapses proves the same Profile ID
// named by two different matching Bindings is not itself ambiguity — only
// genuinely distinct candidates within one category are.
func TestResolveIdentities_DuplicateIDCollapses(t *testing.T) {
	res := ResolveIdentities([]string{"company:example", "company:example"})
	if res.IsAmbiguous() {
		t.Fatalf("expected no ambiguity for a repeated identical id, got %+v", res.Ambiguous)
	}
	if res.Resolved["company"] != "company:example" {
		t.Errorf("Resolved[company] = %q, want company:example", res.Resolved["company"])
	}
}

// TestResolveIdentities_Ambiguous is the core of issue #16 AC #2: "Multiple
// plausible identities require explicit selection." Two distinct
// company:* Profiles both matching leaves the company category ambiguous.
func TestResolveIdentities_Ambiguous(t *testing.T) {
	res := ResolveIdentities([]string{"personal:alice", "company:example", "company:other-corp", "project:order-service"})
	if !res.IsAmbiguous() {
		t.Fatal("expected company to be ambiguous")
	}
	if len(res.Ambiguous) != 1 {
		t.Fatalf("Ambiguous = %+v, want exactly one ambiguous category", res.Ambiguous)
	}
	amb := res.Ambiguous[0]
	if amb.Category != "company" {
		t.Errorf("ambiguous category = %q, want company", amb.Category)
	}
	want := []string{"company:example", "company:other-corp"}
	if !reflect.DeepEqual(amb.Candidates, want) {
		t.Errorf("candidates = %v, want %v", amb.Candidates, want)
	}
	// personal and project are unambiguous and still resolved.
	if res.Resolved["personal"] != "personal:alice" || res.Resolved["project"] != "project:order-service" {
		t.Errorf("Resolved = %v, want personal:alice and project:order-service still present", res.Resolved)
	}
}

func TestFinalProfileIDs_OrderedBroadToNarrow(t *testing.T) {
	res := ResolveIdentities([]string{"project:order-service", "personal:alice", "team:payments", "company:example", "task:hotfix"})
	got, err := FinalProfileIDs(res, nil)
	if err != nil {
		t.Fatalf("FinalProfileIDs: %v", err)
	}
	want := []string{"personal:alice", "company:example", "team:payments", "project:order-service", "task:hotfix"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FinalProfileIDs = %v, want %v (broad-to-narrow per docs/product/requirements.md §5.1)", got, want)
	}
}

func TestFinalProfileIDs_AmbiguousWithoutSelection_Errors(t *testing.T) {
	res := ResolveIdentities([]string{"company:example", "company:other-corp"})
	if _, err := FinalProfileIDs(res, nil); err == nil {
		t.Fatal("expected an error: ambiguous category with no selection")
	}
}

func TestFinalProfileIDs_AmbiguousWithSelection_Resolves(t *testing.T) {
	res := ResolveIdentities([]string{"personal:alice", "company:example", "company:other-corp"})
	selection := map[string]string{"company": "company:other-corp"}
	got, err := FinalProfileIDs(res, selection)
	if err != nil {
		t.Fatalf("FinalProfileIDs: %v", err)
	}
	want := []string{"personal:alice", "company:other-corp"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FinalProfileIDs = %v, want %v", got, want)
	}
}

// TestFinalProfileIDs_SelectionNotAmongCandidates_Errors proves a stale or
// corrupted persisted selection (naming a Profile ID Binding matching never
// actually offered for that category) is rejected rather than silently
// accepted.
func TestFinalProfileIDs_SelectionNotAmongCandidates_Errors(t *testing.T) {
	res := ResolveIdentities([]string{"company:example", "company:other-corp"})
	selection := map[string]string{"company": "company:not-a-real-candidate"}
	if _, err := FinalProfileIDs(res, selection); err == nil {
		t.Fatal("expected an error: selection names a candidate Binding matching never offered")
	}
}

// TestPersistAndReadSelection_RoundTrip is issue #16 AC #2's literal test:
// "the choice persists locally ... (test)." It writes through
// PersistSelection under a fake worktree state dir and reads it back with
// ReadSelection, on real files on disk (t.TempDir()-rooted).
func TestPersistAndReadSelection_RoundTrip(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state", "worktrees", "abc123")
	selection := map[string]string{"company": "company:other-corp", "project": "project:order-service"}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	if err := PersistSelection(stateDir, "worktree:sha256:abc123", selection, now); err != nil {
		t.Fatalf("PersistSelection: %v", err)
	}

	got, ok, err := ReadSelection(stateDir)
	if err != nil {
		t.Fatalf("ReadSelection: %v", err)
	}
	if !ok {
		t.Fatal("ReadSelection: ok = false, want true after PersistSelection")
	}
	if !reflect.DeepEqual(got, selection) {
		t.Errorf("ReadSelection = %v, want %v", got, selection)
	}

	// The file must exist at exactly desired/identities.yaml under the
	// worktree state dir (docs/architecture/README.md §8), as real bytes on
	// disk -- not just something this package's own functions agree on in
	// memory.
	onDisk := filepath.Join(stateDir, "desired", "identities.yaml")
	if _, err := os.Stat(onDisk); err != nil {
		t.Errorf("expected a file at %s: %v", onDisk, err)
	}
}

// TestReadSelection_NothingPersistedYet proves the "never selected before"
// state is a normal, no-error outcome.
func TestReadSelection_NothingPersistedYet(t *testing.T) {
	stateDir := t.TempDir()
	got, ok, err := ReadSelection(stateDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false: nothing has been persisted")
	}
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

// TestPersistSelection_NeverWritesUnderRepository is issue #16 AC #2's
// other half: "[the choice] is never written into the repository (test)."
// It persists a selection at a worktree state dir that is a sibling of (not
// nested inside) a separate fake repository checkout, and asserts the
// repository directory tree gains no new files at all.
func TestPersistSelection_NeverWritesUnderRepository(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	repoOMCA := filepath.Join(repoDir, ".omca")
	stateDir := filepath.Join(root, "state", "worktrees", "abc123")

	mustWriteFile(t, filepath.Join(repoOMCA, "project.yaml"), "placeholder: true")

	before := listFiles(t, repoDir)

	selection := map[string]string{"personal": "personal:alice"}
	if err := PersistSelection(stateDir, "worktree:sha256:abc123", selection, time.Now()); err != nil {
		t.Fatalf("PersistSelection: %v", err)
	}

	after := listFiles(t, repoDir)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("repository tree changed after PersistSelection: before=%v after=%v", before, after)
	}

	// The selection is, however, actually persisted -- just not there.
	if _, ok, err := ReadSelection(stateDir); err != nil || !ok {
		t.Errorf("ReadSelection after PersistSelection: ok=%v err=%v, want ok=true err=nil", ok, err)
	}
}

func listFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}
