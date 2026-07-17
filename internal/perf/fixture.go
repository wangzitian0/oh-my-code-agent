package perf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// fakeVersionBinaryScript is a hermetic POSIX shell script answering only
// `--version` — this package's own local copy of the pattern
// cmd/omca/testenv_test.go's writeFakeVersionBinary and internal/context/
// host_test.go's writeFakeBinary already establish for the identical safety
// requirement (never invoke a real codex/claude binary from measurement
// code). Duplicated here, not imported, for the same "no test-only helper
// exported across a package boundary" reason internal/shim's doc.go gives
// for its own small duplicated helpers — and because this package's use is
// production code (MeasureSynthetic is called from a `go test`, but is not
// itself a _test.go file), not a test-only helper.
const fakeVersionBinaryScript = "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\nprintf '%s\\n' \"$FAKE_HOST_VERSION_OUTPUT\"\nfi\n"

// buildFakeHostBinary writes a hermetic fake "codex" executable into dir,
// answering `--version` with "codex-cli 0.144.5" (reading the exact output
// from its own FAKE_HOST_VERSION_OUTPUT environment variable rather than
// baking it into the script text, so the same one script file could in
// principle serve either host's name by varying only the invoking
// environment — not exercised today since this package measures codex
// only, see doc.go, but keeps the script itself host-neutral). Returns the
// absolute path to dir (usable directly as a PATH entry).
func buildFakeHostBinary(dir, name string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("perf: buildFakeHostBinary: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(fakeVersionBinaryScript), 0o755); err != nil {
		return "", fmt.Errorf("perf: buildFakeHostBinary: %w", err)
	}
	return dir, nil
}

// SyntheticFixtureSize is how many fake native user-global MCP servers and
// Skills buildSyntheticFixture plants under one fake CODEX_HOME.
type SyntheticFixtureSize struct {
	MCPServers int
	Skills     int
}

// DefaultSyntheticFixtureSize matches internal/runtime/bootstrap_codex_test.go's
// own TestBootstrap_Codex_30MCPServersAnd20Skills_NoneLeak fixture size —
// issue #15's own instruction to reuse "a large fake fixture like PR-09's
// own 30-MCP/20-skill test fixture" for the synthetic benchmark that
// demonstrates the mechanism scales.
var DefaultSyntheticFixtureSize = SyntheticFixtureSize{MCPServers: 30, Skills: 20}

// buildSyntheticFixture writes a synthetic, hermetic CODEX_HOME (never the
// real ~/.codex) under root/codex-home, containing size.MCPServers distinct
// [mcp_servers.*] entries in one config.toml and size.Skills distinct
// SKILL.md packages, plus a worktree root under root/project containing one
// repository AGENTS.md — mirroring bootstrap_codex_test.go's own fixture
// construction so this package's synthetic measurement compiles against
// realistic content shape, not an empty directory. Returns the two
// resulting absolute paths.
func buildSyntheticFixture(root string, size SyntheticFixtureSize) (codexHome, worktreeRoot string, err error) {
	codexHome = filepath.Join(root, "codex-home")
	worktreeRoot = filepath.Join(root, "project")

	var toml strings.Builder
	for i := 0; i < size.MCPServers; i++ {
		fmt.Fprintf(&toml, "[mcp_servers.fake-mcp-%02d]\ncommand = \"npx\"\nargs = [\"fake-package-%02d\"]\n\n", i, i)
	}
	if err := writeFile(filepath.Join(codexHome, "config.toml"), toml.String()); err != nil {
		return "", "", err
	}
	for i := 0; i < size.Skills; i++ {
		name := fmt.Sprintf("fake-skill-%02d", i)
		content := fmt.Sprintf("---\nname: %s\n---\nSynthetic fixture skill, never a real one.\n", name)
		if err := writeFile(filepath.Join(codexHome, "skills", name, "SKILL.md"), content); err != nil {
			return "", "", err
		}
	}
	if err := writeFile(filepath.Join(worktreeRoot, "AGENTS.md"), "# synthetic fixture project instructions\n"); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Join(worktreeRoot, ".git"), 0o755); err != nil {
		return "", "", fmt.Errorf("perf: buildSyntheticFixture: %w", err)
	}
	return codexHome, worktreeRoot, nil
}

// writeFile creates path's parent directories and writes content, the same
// tiny fixture-writing helper every other package's own test suite in this
// module already duplicates locally (internal/runtime/helpers_test.go's
// mustWriteFile, this package's non-test analogue).
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("perf: writeFile: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("perf: writeFile: %w", err)
	}
	return nil
}
