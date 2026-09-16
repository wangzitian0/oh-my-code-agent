// Package passthrough validates candidate environment declarations without
// resolving paths, projecting values, or qualifying host behavior.
package passthrough

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const MaxBytes = 64 * 1024

// Policy is deliberately separate from runtime activation. Its classifications
// are user intent, never evidence that a path contains only cache/runtime data.
type Policy struct {
	APIVersion  string `yaml:"apiVersion"`
	Kind        string `yaml:"kind"`
	Passthrough struct {
		Env map[string]Rule `yaml:"env"`
	} `yaml:"passthrough"`
}

type Rule struct {
	Category  string `yaml:"category"`
	SourceEnv string `yaml:"sourceEnv"`
}

type Entry struct {
	Target        string `json:"target"`
	SourceEnv     string `json:"sourceEnv"`
	Category      string `json:"category"`
	SourcePresent bool   `json:"sourcePresent"`
	Qualification string `json:"qualification"`
}

type Preview struct {
	SchemaVersion int     `json:"schemaVersion"`
	Mode          string  `json:"mode"`
	Applied       bool    `json:"applied"`
	Entries       []Entry `json:"entries"`
}

var namePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func allowedName(name string) bool {
	if !namePattern.MatchString(name) {
		return false
	}
	// These are isolation, executable-loading, identity or configuration inputs,
	// not cache/runtime path declarations. This is a guard, not a security sandbox.
	for _, prefix := range []string{"OMCA_", "GIT_", "SSH_", "AWS_", "AZURE_", "GOOGLE_", "LD_", "DYLD_", "BASH_"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	for _, fragment := range []string{"TOKEN", "SECRET", "PASSWORD", "CREDENTIAL", "API_KEY", "PRIVATE_KEY", "AUTH"} {
		if strings.Contains(name, fragment) {
			return false
		}
	}
	switch name {
	case "HOME", "USER", "LOGNAME", "PATH", "SHELL", "ENV", "CDPATH", "IFS", "PYTHONPATH", "NODE_OPTIONS", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "XDG_CONFIG_HOME", "XDG_CONFIG_DIRS", "CARGO_HOME":
		return false
	}
	return true
}

// Parse rejects unknown fields, duplicate keys, extra documents and unsupported
// versions. Parser errors are deliberately not echoed: malformed input can
// contain accidental credential literals.
func Parse(data []byte) (Policy, error) {
	var policy Policy
	if len(data) > MaxBytes {
		return policy, errors.New("policy exceeds size limit")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&policy); err != nil {
		return Policy{}, errors.New("invalid passthrough policy structure")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Policy{}, errors.New("expected one policy document")
	}
	if policy.APIVersion != "omca.dev/v1alpha1" || policy.Kind != "PassthroughPolicy" {
		return Policy{}, errors.New("unsupported passthrough policy version or kind")
	}
	if len(policy.Passthrough.Env) == 0 {
		return Policy{}, errors.New("passthrough.env must contain at least one declaration")
	}
	for target, rule := range policy.Passthrough.Env {
		if !allowedName(target) || !allowedName(rule.SourceEnv) {
			return Policy{}, errors.New("invalid or reserved environment name in passthrough.env")
		}
		if rule.Category != "cache" && rule.Category != "runtime" {
			return Policy{}, errors.New("only cache and runtime declarations are supported; identity requires separate qualification")
		}
	}
	return policy, nil
}

// Read never follows the final symlink or blocks on a FIFO. No referenced
// environment value or target path is read. Errors contain no input content.
func Read(path string) (Policy, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Policy{}, os.ErrNotExist
		}
		return Policy{}, errors.New("policy cannot be opened as a regular file")
	}
	file := os.NewFile(uintptr(fd), "passthrough policy")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Policy{}, errors.New("policy must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return Policy{}, errors.New("policy cannot be read")
	}
	return Parse(data)
}

// Inspect asks only whether a source has a nonempty value; values never enter
// the result. Present does not mean trusted, compatible, or applied.
func Inspect(policy Policy, present func(string) bool) Preview {
	result := Preview{SchemaVersion: 1, Mode: "preview-only", Entries: []Entry{}}
	for target, rule := range policy.Passthrough.Env {
		result.Entries = append(result.Entries, Entry{Target: target, SourceEnv: rule.SourceEnv, Category: rule.Category, SourcePresent: present(rule.SourceEnv), Qualification: "unqualified"})
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Target < result.Entries[j].Target })
	return result
}

func (preview Preview) Summary() string {
	missing := 0
	for _, entry := range preview.Entries {
		if !entry.SourcePresent {
			missing++
		}
	}
	return fmt.Sprintf("%d candidate declarations; %d sources missing or empty; all unqualified, none applied", len(preview.Entries), missing)
}
