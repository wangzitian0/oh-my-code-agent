package qualify

import (
	"os"
	"path/filepath"
	"testing"
)

const validManifestYAML = `
apiVersion: omca.dev/v1alpha1
kind: FixtureInvocation
host: codex
surface: cli
version: 0.144.5
platform: darwin-arm64
cwd: project
invoke:
  attempted: true
  command: codex
  args: ["--version"]
  reason: "version-only isolation smoke test"
binary:
  acquisitionMethod: "npm global install (asdf nodejs)"
  resolvedPath: "/fake/path/codex"
  sha256: "deadbeef"
  versionSource: "codex --version"
observationRules:
  - root: home
    concept: instruction
    scope: user
    surface: cli
  - root: project
    concept: instruction
    scope: workspace
    surface: cli
`

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invocation.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadInvocationManifestValid(t *testing.T) {
	path := writeManifest(t, validManifestYAML)
	m, err := LoadInvocationManifest(path)
	if err != nil {
		t.Fatalf("LoadInvocationManifest: %v", err)
	}
	if m.Host != "codex" || m.Version != "0.144.5" {
		t.Errorf("m = %+v, unexpected host/version", m)
	}
	if len(m.ObservationRules) != 2 {
		t.Errorf("len(ObservationRules) = %d, want 2", len(m.ObservationRules))
	}
}

func TestLoadInvocationManifestUnknownHost(t *testing.T) {
	bad := `
apiVersion: omca.dev/v1alpha1
kind: FixtureInvocation
host: not-a-real-host
surface: cli
version: 1.0.0
cwd: project
invoke:
  attempted: false
  reason: "no safe path"
observationRules:
  - root: home
    concept: instruction
    scope: user
    surface: cli
`
	path := writeManifest(t, bad)
	if _, err := LoadInvocationManifest(path); err == nil {
		t.Error("LoadInvocationManifest(unknown host) error = nil, want error")
	}
}

func TestLoadInvocationManifestMissingReasonWhenSkipped(t *testing.T) {
	bad := `
apiVersion: omca.dev/v1alpha1
kind: FixtureInvocation
host: codex
surface: cli
version: 1.0.0
cwd: project
invoke:
  attempted: false
observationRules:
  - root: home
    concept: instruction
    scope: user
    surface: cli
`
	path := writeManifest(t, bad)
	if _, err := LoadInvocationManifest(path); err == nil {
		t.Error("LoadInvocationManifest(skipped invoke without reason) error = nil, want error")
	}
}

func TestLoadInvocationManifestInvokeAttemptedWithoutCommand(t *testing.T) {
	bad := `
apiVersion: omca.dev/v1alpha1
kind: FixtureInvocation
host: codex
surface: cli
version: 1.0.0
cwd: project
invoke:
  attempted: true
  reason: "should have a command"
observationRules:
  - root: home
    concept: instruction
    scope: user
    surface: cli
`
	path := writeManifest(t, bad)
	if _, err := LoadInvocationManifest(path); err == nil {
		t.Error("LoadInvocationManifest(attempted without command) error = nil, want error")
	}
}

func TestLoadInvocationManifestBadCwd(t *testing.T) {
	bad := `
apiVersion: omca.dev/v1alpha1
kind: FixtureInvocation
host: codex
surface: cli
version: 1.0.0
cwd: somewhere-else
invoke:
  attempted: false
  reason: "n/a"
observationRules:
  - root: home
    concept: instruction
    scope: user
    surface: cli
`
	path := writeManifest(t, bad)
	if _, err := LoadInvocationManifest(path); err == nil {
		t.Error("LoadInvocationManifest(bad cwd) error = nil, want error")
	}
}

func TestLoadInvocationManifestNoObservationRules(t *testing.T) {
	bad := `
apiVersion: omca.dev/v1alpha1
kind: FixtureInvocation
host: codex
surface: cli
version: 1.0.0
cwd: project
invoke:
  attempted: false
  reason: "n/a"
`
	path := writeManifest(t, bad)
	if _, err := LoadInvocationManifest(path); err == nil {
		t.Error("LoadInvocationManifest(no observation rules) error = nil, want error")
	}
}

func TestLoadInvocationManifestWrongKind(t *testing.T) {
	bad := `
apiVersion: omca.dev/v1alpha1
kind: SomethingElse
host: codex
surface: cli
version: 1.0.0
cwd: project
invoke:
  attempted: false
  reason: "n/a"
observationRules:
  - root: home
    concept: instruction
    scope: user
    surface: cli
`
	path := writeManifest(t, bad)
	if _, err := LoadInvocationManifest(path); err == nil {
		t.Error("LoadInvocationManifest(wrong kind) error = nil, want error")
	}
}

func TestLoadInvocationManifestFileNotFound(t *testing.T) {
	if _, err := LoadInvocationManifest(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("LoadInvocationManifest(missing file) error = nil, want error")
	}
}
