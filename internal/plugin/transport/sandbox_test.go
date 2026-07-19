package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/conformance"
	"github.com/wangzitian0/oh-my-code-agent/internal/qualify"
)

// fingerprint/snapshotTree/diffSnapshots are a third, minimal instance of the
// exact same zero-write proof mechanism internal/plugin/conformance/
// snapshot.go and internal/qualify/snapshot.go already each implement
// independently (their own doc comments cross-reference each other) — kept
// local rather than imported because both of those are unexported in their
// own packages, and this file needs the mechanism, not a public API change
// to either.
type fingerprint struct {
	mode   os.FileMode
	digest string
}

type snapshot map[string]fingerprint

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
		return nil, err
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

func diffSnapshots(before, after snapshot) []string {
	var diffs []string
	for path, beforeFp := range before {
		afterFp, ok := after[path]
		if !ok {
			diffs = append(diffs, "removed: "+path)
			continue
		}
		if afterFp != beforeFp {
			diffs = append(diffs, "changed: "+path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			diffs = append(diffs, "added: "+path)
		}
	}
	sort.Strings(diffs)
	return diffs
}

// TestRemoteAdapter_Sandbox_NoExtraWriteOrExecCapability is issue #29's third
// acceptance criterion: "Transport adds no write/exec capability beyond the
// contract (sandbox test)." It launches the real, compiled
// fakeadapterexternal fixture binary as a genuine subprocess whose
// environment is built entirely from scratch by internal/qualify.Sandbox —
// HOME/CODEX_HOME/PATH explicitly redirected into a scratch directory tree,
// never inherited from the test process (the same discipline
// internal/qualify.RunInvocation already applies to real host binary
// invocations, and internal/plugin/conformance's own runObserveZeroSideEffects
// applies to a single Observe call) — then drives the ENTIRE HostAdapter
// contract through it via conformance.Run, and snapshot-diffs the whole
// sandbox root from before the subprocess ever started to after it exited.
//
// A conformant adapter (conformance.FakeAdapter, which this fixture wraps)
// never writes or executes anything across its whole contract surface, so an
// empty diff here is a genuine end-to-end proof that RemoteAdapter's own
// subprocess-launching mechanism does not itself grant the external binary
// any capability the contract does not already describe — not just that one
// method (Observe) avoids writing, which conformance.Run already proves on
// its own, separate internal temp directory.
func TestRemoteAdapter_Sandbox_NoExtraWriteOrExecCapability(t *testing.T) {
	root := t.TempDir()
	sb, err := qualify.NewSandbox(root, "codex")
	if err != nil {
		t.Fatalf("qualify.NewSandbox: %v", err)
	}
	canaryMarker, err := sb.PlantOutsideCanary()
	if err != nil {
		t.Fatalf("PlantOutsideCanary: %v", err)
	}

	before, err := snapshotTree(root)
	if err != nil {
		t.Fatalf("snapshot sandbox before launch: %v", err)
	}

	cmd := exec.Command(fakeAdapterExternalBinary)
	// sb.Env builds HOME/PATH/CODEX_HOME from scratch (never
	// append(os.Environ(), ...)) -- os.Getenv("PATH") is passed through only
	// so the fixture binary's own dynamic loader/runtime can still resolve,
	// exactly like internal/qualify.RunInvocation's own pathEnv parameter.
	cmd.Env = sb.Env(os.Getenv("PATH"))
	cmd.Dir = sb.Project

	remote, err := NewRemoteAdapter(cmd)
	if err != nil {
		t.Fatalf("NewRemoteAdapter: unexpected error: %v", err)
	}

	conformance.Run(t, remote)

	if err := remote.Close(); err != nil {
		t.Errorf("Close: unexpected error: %v", err)
	}

	after, err := snapshotTree(root)
	if err != nil {
		t.Fatalf("snapshot sandbox after conformance.Run: %v", err)
	}
	if diffs := diffSnapshots(before, after); len(diffs) != 0 {
		t.Errorf("the sandboxed subprocess wrote outside its declared contract (zero-write/zero-exec violation): %v", diffs)
	}
	if _, statErr := os.Stat(canaryMarker); statErr == nil {
		t.Error("the sandboxed subprocess executed the outside-world canary script (zero-exec violation): CANARY_MARKER was created")
	}
}
