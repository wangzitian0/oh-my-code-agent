package qualify

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Sandbox is one fixture case's fully isolated temporary directory tree. It
// is created fresh per case (never reused) and its paths are the only
// locations a host binary invocation is ever pointed at.
//
// Outside is a directory disjoint from every other field: it stands in for
// "the real world" (a realistic-looking native config tree, see
// PlantOutsideCanary) and is never named by any environment variable handed
// to an invoked subprocess. Asserting Outside is byte-for-byte unchanged
// before and after an invocation is this harness's automated, deterministic
// zero-write proof (doc.go explains why the actual real home is a separate,
// opt-in check rather than the default one).
type Sandbox struct {
	Root            string
	Home            string
	CodexHome       string
	ClaudeConfigDir string
	Project         string
	Outside         string
}

// NewSandbox creates a fresh isolated directory tree under root (normally a
// t.TempDir() the caller controls) for one fixture case targeting host. Codex
// gets a separate CodexHome (docs/architecture/runtime.md §7.1: "isolated
// Codex home and a virtual process home"); Claude Code gets a
// ClaudeConfigDir (§7.2's "relocated configuration directory"; the exact
// environment variable, CLAUDE_CONFIG_DIR, was confirmed by static inspection
// of the installed claude binary, see fixtures/README.md).
func NewSandbox(root, host string) (*Sandbox, error) {
	sb := &Sandbox{
		Root:    root,
		Home:    filepath.Join(root, "home"),
		Project: filepath.Join(root, "project"),
		Outside: filepath.Join(root, "outside-world"),
	}
	dirs := []string{sb.Home, sb.Project, sb.Outside}

	switch host {
	case "codex":
		sb.CodexHome = filepath.Join(root, "codex-home")
		dirs = append(dirs, sb.CodexHome)
	case "claude-code":
		sb.ClaudeConfigDir = filepath.Join(root, "claude-config")
		dirs = append(dirs, sb.ClaudeConfigDir)
	default:
		return nil, fmt.Errorf("qualify: NewSandbox: unsupported host %q (want %q or %q)", host, "codex", "claude-code")
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("qualify: NewSandbox: %w", err)
		}
	}
	return sb, nil
}

// Env returns the exact environment this sandbox's host invocation must run
// with: an explicit, minimal list built from scratch (never
// append(os.Environ(), ...)), so nothing ambient from the real calling
// process's environment can leak into, or be read by, the invoked binary.
// PATH is the one exception: it is required to resolve the binary itself and
// its own dynamic loader/runtime dependencies, so the caller supplies the
// real PATH value explicitly (RunInvocation does this deliberately, never
// implicitly).
func (sb *Sandbox) Env(path string) []string {
	env := []string{
		"HOME=" + sb.Home,
		"PATH=" + path,
	}
	if sb.CodexHome != "" {
		env = append(env, "CODEX_HOME="+sb.CodexHome)
	}
	if sb.ClaudeConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+sb.ClaudeConfigDir)
	}
	return env
}

// PopulateFromInput copies a fixture case's input/ directory into this
// sandbox: input/home/** -> Home, input/project/** -> Project, and (Codex
// only) input/codex-home/** -> CodexHome. A subdirectory that does not exist
// in the case's input/ is simply skipped, not an error — most cases only
// need one or two of the three roots populated.
func (sb *Sandbox) PopulateFromInput(inputDir string) error {
	mapping := map[string]string{
		"home":    sb.Home,
		"project": sb.Project,
	}
	if sb.CodexHome != "" {
		mapping["codex-home"] = sb.CodexHome
	}
	if sb.ClaudeConfigDir != "" {
		mapping["claude-config"] = sb.ClaudeConfigDir
	}
	for name, dst := range mapping {
		src := filepath.Join(inputDir, name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("qualify: PopulateFromInput: stat %s: %w", src, err)
		}
		if err := copyTree(src, dst); err != nil {
			return fmt.Errorf("qualify: PopulateFromInput: %s -> %s: %w", src, dst, err)
		}
	}
	return nil
}

// PlantOutsideCanary populates Outside with a realistic native-config-shaped
// tree (a settings file, a skill, and an executable canary script) so the
// zero-write/zero-exec proof around an invocation is a true positive: if
// isolation failed and a subprocess actually reached Outside, either the
// snapshot diff or the canary marker would catch it. This mirrors
// internal/plugin/conformance's runObserveZeroSideEffects canary technique.
func (sb *Sandbox) PlantOutsideCanary() (canaryMarker string, err error) {
	if err := writeFile(filepath.Join(sb.Outside, "settings.json"), `{"permissions":{"allow":["*"]}}`, 0o644); err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(sb.Outside, "skills", "deploy", "SKILL.md"), "# deploy\n", 0o644); err != nil {
		return "", err
	}
	canaryPath := filepath.Join(sb.Outside, "bin", "canary.sh")
	if err := writeFile(canaryPath, "#!/bin/sh\necho executed > \"$(dirname \"$0\")/CANARY_MARKER\"\n", 0o755); err != nil {
		return "", err
	}
	return filepath.Join(sb.Outside, "bin", "CANARY_MARKER"), nil
}

func writeFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// copyTree recursively copies src onto dst, preserving each entry's file
// mode (so an executable fixture file, e.g. a planted canary script, stays
// executable) and directory structure. It never follows symlinks specially;
// a fixture's committed input/ is expected to contain only regular files and
// directories.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
