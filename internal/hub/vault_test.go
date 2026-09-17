package hub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVault_ExpandEnv(t *testing.T) {
	envMap := map[string]string{
		"FOO": "bar",
		"BAZ": "qux",
	}
	os.Setenv("TEST_GLOBAL_VAR", "global_val")
	defer os.Unsetenv("TEST_GLOBAL_VAR")

	tests := []struct {
		input    string
		expected string
	}{
		{"hello $FOO", "hello bar"},
		{"hello ${FOO}", "hello bar"},
		{"${FOO}/${BAZ}", "bar/qux"},
		{"global: ${TEST_GLOBAL_VAR}", "global: global_val"},
		{"escape: $$FOO", "escape: $FOO"},
		{"unclosed: ${FOO", "unclosed: ${FOO"},
		{"undefined: ${NOT_SET}", "undefined: "},
		{"mixed: ${FOO}_${BAZ}", "mixed: bar_qux"},
		{"slash: $FOO/$BAZ", "slash: bar/qux"},
	}

	for _, tc := range tests {
		got := ExpandEnv(tc.input, envMap)
		if got != tc.expected {
			t.Errorf("ExpandEnv(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestVault_LoadEnvFileAndHydrate(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".test.env")
	content := `# Test comment
export API_KEY="secret-123"
BASE_URL='https://example.com/api'
PORT=8080 # inline comment
EMPTY_VAL=
`
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	envMap, err := LoadEnvFile(envPath)
	if err != nil {
		t.Fatalf("LoadEnvFile error: %v", err)
	}

	if envMap["API_KEY"] != "secret-123" {
		t.Errorf("expected API_KEY=secret-123, got %q", envMap["API_KEY"])
	}
	if envMap["BASE_URL"] != "https://example.com/api" {
		t.Errorf("expected BASE_URL=https://example.com/api, got %q", envMap["BASE_URL"])
	}
	if envMap["PORT"] != "8080" {
		t.Errorf("expected PORT=8080, got %q", envMap["PORT"])
	}

	// Test HydrateToolConfig
	cfg := ToolConfig{
		Name:    "test-tool",
		Command: "${BASE_URL}/run",
		Args:    []string{"--key=${API_KEY}", "--port=${PORT}"},
		Env: map[string]string{
			"AUTH": "Bearer ${API_KEY}",
		},
		WorkingDir: "/tmp/${PORT}",
	}

	hydrated := HydrateToolConfig(cfg, envMap)
	if hydrated.Command != "https://example.com/api/run" {
		t.Errorf("hydrated Command: %q", hydrated.Command)
	}
	if hydrated.Args[0] != "--key=secret-123" || hydrated.Args[1] != "--port=8080" {
		t.Errorf("hydrated Args: %v", hydrated.Args)
	}
	if hydrated.Env["AUTH"] != "Bearer secret-123" {
		t.Errorf("hydrated Env: %v", hydrated.Env)
	}
	if hydrated.WorkingDir != "/tmp/8080" {
		t.Errorf("hydrated WorkingDir: %q", hydrated.WorkingDir)
	}
}

func TestConfig_MultiProfileResolution(t *testing.T) {
	cfg := &Config{
		SharedTools: map[string]ToolConfig{
			"worker": {Name: "worker", Command: "subagent-worker"},
		},
		Profiles: map[string]ProfileConfig{
			"work": {
				WorkspaceRoots: []string{"/workspace/projects", "/workspace/order"},
				Tools: map[string]ToolConfig{
					"memory": {Name: "memory", Command: "memory-work"},
					"gitlab": {Name: "gitlab", Command: "mcp-gitlab"},
				},
			},
			"personal": {
				WorkspaceRoots: []string{"/home/dev", "/home/dev/personal"},
				Tools: map[string]ToolConfig{
					"memory": {Name: "memory", Command: "memory-personal"},
					"github": {Name: "github", Command: "mcp-github"},
				},
			},
		},
	}

	// 1. Explicit profile resolution
	res, ok := cfg.ResolveServer("work", "", "memory")
	if !ok || res.InstanceKey != "work:memory" || res.Config.Command != "memory-work" {
		t.Fatalf("expected work:memory, got %+v (ok=%v)", res, ok)
	}

	// 2. Auto-detection via CWD prefix
	res, ok = cfg.ResolveServer("", "/workspace/projects/frontend", "gitlab")
	if !ok || res.InstanceKey != "work:gitlab" {
		t.Fatalf("expected work:gitlab via CWD, got %+v (ok=%v)", res, ok)
	}

	res, ok = cfg.ResolveServer("", "/home/dev/personal/blog", "memory")
	if !ok || res.InstanceKey != "personal:memory" || res.Config.Command != "memory-personal" {
		t.Fatalf("expected personal:memory via CWD, got %+v (ok=%v)", res, ok)
	}

	// 3. Shared tools accessible from any context
	res, ok = cfg.ResolveServer("", "/random/dir", "worker")
	if !ok || res.InstanceKey != "shared:worker" {
		t.Fatalf("expected shared:worker, got %+v (ok=%v)", res, ok)
	}

	// 4. Fallback when server unique to one profile
	res, ok = cfg.ResolveServer("", "", "github")
	if !ok || res.InstanceKey != "personal:github" {
		t.Fatalf("expected personal:github fallback, got %+v (ok=%v)", res, ok)
	}
}
