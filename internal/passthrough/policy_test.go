package passthrough

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const validPolicy = `apiVersion: omca.dev/v1alpha1
kind: PassthroughPolicy
passthrough:
  env:
    UV_CACHE_DIR:
      category: cache
      sourceEnv: HOST_UV_CACHE_DIR
    ASDF_DATA_DIR:
      category: runtime
      sourceEnv: HOST_ASDF_DATA_DIR
`

func TestPreviewContainsNoValuesAndDoesNotQualify(t *testing.T) {
	policy, err := Parse([]byte(validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	preview := Inspect(policy, func(name string) bool { return name == "HOST_UV_CACHE_DIR" })
	if preview.Applied || len(preview.Entries) != 2 {
		t.Fatalf("bad preview: %+v", preview)
	}
	if preview.Entries[0].Target != "ASDF_DATA_DIR" || preview.Entries[0].SourcePresent || !preview.Entries[1].SourcePresent {
		t.Fatalf("bad order/presence: %+v", preview)
	}
	for _, entry := range preview.Entries {
		if entry.Qualification != "unqualified" {
			t.Fatal("false qualification")
		}
	}
	if !strings.Contains(preview.Summary(), "1 sources missing or empty") {
		t.Fatal(preview.Summary())
	}
}

func TestRejectUnsafeOrAmbiguousPoliciesWithoutEcho(t *testing.T) {
	cases := map[string]string{
		"version":    strings.Replace(validPolicy, "v1alpha1", "v999", 1),
		"kind":       strings.Replace(validPolicy, "PassthroughPolicy", "Other", 1),
		"unknown":    validPolicy + "unexpected: synthetic-secret\n",
		"duplicate":  validPolicy + "kind: PassthroughPolicy\n",
		"documents":  validPolicy + "---\n" + validPolicy,
		"identity":   strings.Replace(validPolicy, "category: cache", "category: identity", 1),
		"literal":    strings.Replace(validPolicy, "sourceEnv: HOST_UV_CACHE_DIR", "value: synthetic-secret", 1),
		"expression": strings.Replace(validPolicy, "HOST_UV_CACHE_DIR", "$(touch synthetic-secret)", 1),
		"empty":      "apiVersion: omca.dev/v1alpha1\nkind: PassthroughPolicy\npassthrough: {env: {}}\n",
		"oversize":   strings.Repeat("x", MaxBytes+1),
		"truncated":  "passthrough: [synthetic-secret",
	}
	for _, name := range []string{"HOME", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "OMCA_REAL_HOME", "PATH", "XDG_CONFIG_HOME", "CARGO_HOME", "API_TOKEN", "GIT_CONFIG_GLOBAL", "SSH_AUTH_SOCK"} {
		cases["target-"+name] = strings.Replace(validPolicy, "UV_CACHE_DIR:", name+":", 1)
		cases["source-"+name] = strings.Replace(validPolicy, "HOST_UV_CACHE_DIR", name, 1)
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(data))
			if err == nil {
				t.Fatal("accepted invalid policy")
			}
			if strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("input echoed")
			}
		})
	}
}

func TestReadRefusesSpecialFilesAndLeavesInputsUntouched(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "policy.yaml")
	if err := os.WriteFile(path, []byte(validPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	policy, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(policy)
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{link, fifo, root} {
		if _, err := Read(invalid); err == nil {
			t.Fatal("accepted nonregular file")
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != validPolicy {
		t.Fatal("policy mutated")
	}
	reread, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(reread)
	if string(encoded) != string(before) {
		t.Fatal("changed read")
	}
	if _, err := Read(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
