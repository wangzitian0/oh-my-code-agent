package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/passthrough"
)

const previewPolicy = `apiVersion: omca.dev/v1alpha1
kind: PassthroughPolicy
passthrough:
  env:
    UV_CACHE_DIR:
      category: cache
      sourceEnv: HOST_UV_CACHE_DIR
`

func TestPassthroughPreviewReadOnlyAndRedacted(t *testing.T) {
	fixture := setupManagedTestEnv(t, true, true)
	dir := filepath.Join(fixture.WorktreeRoot, ".omca")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "passthrough.yaml")
	if err := os.WriteFile(path, []byte(previewPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_UV_CACHE_DIR", "synthetic-secret-not-a-path")
	t.Setenv("UV_CACHE_DIR", "keep-caller-value")
	// A preview must not dispatch a host or initialize state.
	t.Setenv("PATH", t.TempDir())
	state := filepath.Join(t.TempDir(), "absent-state")
	t.Setenv("OMCA_STATE_DIR", state)
	for _, args := range [][]string{{"passthrough", "preview"}, {"passthrough", "preview", "--file", path}} {
		var out, errout bytes.Buffer
		if code := run(args, &out, &errout); code != 0 {
			t.Fatalf("exit %d: %s", code, errout.String())
		}
		var preview passthrough.Preview
		if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
			t.Fatal(err)
		}
		if preview.Applied || len(preview.Entries) != 1 || !preview.Entries[0].SourcePresent || preview.Entries[0].Qualification != "unqualified" {
			t.Fatalf("bad preview: %+v", preview)
		}
		if strings.Contains(out.String()+errout.String(), "synthetic-secret") {
			t.Fatal("value leaked")
		}
	}
	if os.Getenv("UV_CACHE_DIR") != "keep-caller-value" {
		t.Fatal("environment projected")
	}
	for _, directory := range []string{fixture.HomeDir, fixture.StateRoot} {
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatalf("preview mutated native home or state: %v", err)
		}
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("state initialized")
	}
	data, _ := os.ReadFile(path)
	if string(data) != previewPolicy {
		t.Fatal("policy changed")
	}
}

func TestPassthroughDoctorNeverClaimsQualification(t *testing.T) {
	root := t.TempDir()
	env := hostcontext.RealEnvironment()
	if f := checkPassthrough(root, env); f.Status != statusWarn {
		t.Fatalf("missing policy %+v", f)
	}
	dir := filepath.Join(root, ".omca")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "passthrough.yaml")
	for _, tc := range []struct {
		data   string
		status doctorStatus
	}{{previewPolicy, statusWarn}, {strings.Replace(previewPolicy, "UV_CACHE_DIR:", "HOME:", 1), statusFail}, {"secret: synthetic-secret", statusFail}} {
		if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		f := checkPassthrough(root, env)
		if f.Status != tc.status {
			t.Fatalf("finding %+v", f)
		}
		if strings.Contains(f.Detail, "synthetic-secret") {
			t.Fatal("value leaked")
		}
	}
}

func TestPassthroughInvalidArgumentsAndSymlinkDirectory(t *testing.T) {
	for _, args := range [][]string{{}, {"apply"}, {"preview", "--secret=synthetic-secret"}, {"preview", "extra"}} {
		var out, errout bytes.Buffer
		if code := runPassthrough(&out, &errout, args); code != 2 {
			t.Fatalf("exit %d", code)
		}
		if strings.Contains(errout.String(), "synthetic-secret") {
			t.Fatal("argument echoed")
		}
	}
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".omca")); err != nil {
		t.Fatal(err)
	}
	if f := checkPassthrough(root, hostcontext.RealEnvironment()); f.Status != statusFail {
		t.Fatalf("symlink accepted %+v", f)
	}
	var out, errout bytes.Buffer
	if code := runPassthrough(&out, &errout, []string{"preview", "--file", filepath.Join(root, "missing")}); code != 1 {
		t.Fatalf("exit %d", code)
	}
}
