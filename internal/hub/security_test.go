package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSecurity_ProfileIsolationBoundary(t *testing.T) {
	cfg := &Config{
		SharedTools: map[string]ToolConfig{
			"shared-worker": {Name: "shared-worker", Command: "echo"},
		},
		Profiles: map[string]ProfileConfig{
			"work": {
				WorkspaceRoots: []string{"/workspace/corp"},
				Tools: map[string]ToolConfig{
					"corp-gitlab": {Name: "corp-gitlab", Command: "echo"},
					"corp-code":   {Name: "corp-code", Command: "echo"},
				},
				EnvFiles: []string{"/workspace/corp/.env.secret"},
			},
			"personal": {
				WorkspaceRoots: []string{"/home/user/personal"},
				Tools: map[string]ToolConfig{
					"personal-github": {Name: "personal-github", Command: "echo"},
				},
			},
		},
	}

	// 1. Personal workspace caller trying to access corporate gitlab
	res, ok := cfg.ResolveServer("", "/home/user/personal/my-project", "corp-gitlab")
	if ok || res != nil {
		t.Fatalf("SECURITY VIOLATION: personal workspace caller resolved corporate tool: %+v", res)
	}

	// 2. Personal workspace caller trying to access personal-github
	res, ok = cfg.ResolveServer("", "/home/user/personal/my-project", "personal-github")
	if !ok || res == nil || res.InstanceKey != "personal:personal-github" {
		t.Fatalf("expected personal tool to resolve, got: %+v, ok=%v", res, ok)
	}

	// 3. Both workspaces can access shared tools
	resWork, okWork := cfg.ResolveServer("", "/workspace/corp/proj", "shared-worker")
	resPersonal, okPersonal := cfg.ResolveServer("", "/home/user/personal/proj", "shared-worker")
	if !okWork || resWork.InstanceKey != "shared:shared-worker" {
		t.Fatalf("shared tool resolution failed in work workspace")
	}
	if !okPersonal || resPersonal.InstanceKey != "shared:shared-worker" {
		t.Fatalf("shared tool resolution failed in personal workspace")
	}

	// 4. Explicit profile flag restriction: personal cannot resolve work tool even if specified in serverName
	res, ok = cfg.ResolveServer("personal", "", "corp-gitlab")
	if ok || res != nil {
		t.Fatalf("SECURITY VIOLATION: explicit personal profile resolved corporate tool")
	}
}

func TestSecurity_InputSanitization(t *testing.T) {
	cfg := &Config{
		SharedTools: map[string]ToolConfig{
			"valid-tool": {Name: "valid-tool", Command: "echo"},
		},
	}

	maliciousInputs := []string{
		"valid-tool\n",
		"valid-tool\r\ninjection",
		"valid-tool\x00evil",
		"../../etc/passwd",
		"../valid-tool",
		"valid/tool",
		"valid\\tool",
		"\tvalid-tool",
		"",
	}

	for _, mal := range maliciousInputs {
		res, ok := cfg.ResolveServer("", "", mal)
		if ok || res != nil {
			t.Errorf("SECURITY RISK: malicious serverName %q was resolved: %+v", mal, res)
		}
	}
}

func TestSecurity_JSONAutoRepair_PayloadBombResistance(t *testing.T) {
	t.Run("excessive nesting depth does not cause stack overflow or memory exhaustion", func(t *testing.T) {
		// 50,000 opening braces
		nested := strings.Repeat(`{"a":`, 50000) + `"leaf"`
		start := time.Now()
		repaired := JSONAutoRepair(nested)
		duration := time.Since(start)

		if duration > 2*time.Second {
			t.Errorf("repair took too long: %v", duration)
		}
		if !json.Valid([]byte(repaired)) {
			t.Fatalf("repaired deeply nested json is not valid json")
		}
	})

	t.Run("gigantic payload over 10MB is safely bounded", func(t *testing.T) {
		// 15MB payload
		huge := "{" + strings.Repeat("a", 15*1024*1024)
		start := time.Now()
		repaired := JSONAutoRepair(huge)
		duration := time.Since(start)

		if duration > 1*time.Second {
			t.Errorf("massive payload processing took too long: %v", duration)
		}
		if !strings.Contains(repaired, "exceeded 10MB safety limit") {
			t.Fatalf("expected safety limit truncation notice")
		}
	})
}

func TestSecurity_DetectNgramLoop_LargeDocumentSafety(t *testing.T) {
	// Generate a 100,000-word document with a loop at the very tail
	words := make([]string, 100000)
	for i := range words {
		words[i] = "normal"
	}
	// Append repetitive loop at tail
	for i := 0; i < 40; i++ {
		words = append(words, "looping", "pattern", "repeating", "indefinitely", "token", "sequence", "hazard", "cycle")
	}
	text := strings.Join(words, " ")

	start := time.Now()
	detected := DetectNgramLoop(text, 8, 4)
	duration := time.Since(start)

	if !detected {
		t.Fatalf("expected detection of tail loop")
	}
	if duration > 100*time.Millisecond {
		t.Errorf("ngram loop detection on large document took too long: %v", duration)
	}
}

func TestSecurity_AbruptDisconnect_ResourceReclaim(t *testing.T) {
	pool := NewWorkerPool()
	ctx, cancel := context.WithCancel(context.Background())

	worker := &ActiveWorker{
		ID:       "w-disconnect-test",
		Type:     "task",
		ToolName: "subagent_task",
		HostName: "test-host",
	}
	pool.RegisterWorker(worker, cancel)

	// Simulate client abrupt termination (e.g. host died)
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatalf("expected ctx to be cancelled")
	}

	// Verify kill / cleanup
	_ = pool.KillWorker("w-disconnect-test")

	workers := pool.ListActiveWorkers()
	if len(workers) != 0 {
		t.Fatalf("expected active workers to be 0 after disconnect cleanup, got %d", len(workers))
	}
}
