package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestJSONAutoRepair(t *testing.T) {
	t.Run("empty string returns empty object", func(t *testing.T) {
		got := JSONAutoRepair("")
		if got != "{}" {
			t.Fatalf("expected {}, got %q", got)
		}
	})

	t.Run("valid json untouched or valid", func(t *testing.T) {
		orig := `{"status":"ok","count":42}`
		repaired := JSONAutoRepair(orig)
		if !json.Valid([]byte(repaired)) {
			t.Fatalf("expected valid json, got %q", repaired)
		}
	})

	t.Run("truncated in middle of string", func(t *testing.T) {
		truncated := `{"status":"success","message":"the quick brown fox jumps over`
		repaired := JSONAutoRepair(truncated)
		if !json.Valid([]byte(repaired)) {
			t.Fatalf("failed to repair truncated json string: %s", repaired)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(repaired), &parsed); err != nil {
			t.Fatalf("unmarshal repaired json failed: %v", err)
		}
		if parsed["status"] != "success" {
			t.Errorf("expected status 'success', got %v", parsed["status"])
		}
		meta, ok := parsed["_meta"].(map[string]any)
		if !ok || meta["truncated"] != true {
			t.Errorf("expected _meta.truncated == true, got %v", parsed["_meta"])
		}
	})

	t.Run("truncated nested structures", func(t *testing.T) {
		truncated := `{"tasks":[{"id":"task-1","code":"func main() {"},{"id":"task-2","content":[`
		repaired := JSONAutoRepair(truncated)
		if !json.Valid([]byte(repaired)) {
			t.Fatalf("failed to repair nested truncated json: %s", repaired)
		}
	})

	t.Run("non-json plain text", func(t *testing.T) {
		text := "Running build command..."
		repaired := JSONAutoRepair(text)
		if !strings.Contains(repaired, "Truncated at 150s SLA") {
			t.Fatalf("expected truncation notice for plain text, got %q", repaired)
		}
	})
}

func TestDetectNgramLoop(t *testing.T) {
	t.Run("normal non-repeating text", func(t *testing.T) {
		text := "The worker processed the incoming request and dispatched all subtasks to the worker pool smoothly."
		if DetectNgramLoop(text, 4, 3) {
			t.Fatalf("expected false for non-repeating text")
		}
	})

	t.Run("repeating 8-word sequence 4 times", func(t *testing.T) {
		phrase := "alpha beta gamma delta epsilon zeta eta theta"
		text := fmt.Sprintf("Intro text %s %s %s %s trailing text", phrase, phrase, phrase, phrase)
		if !DetectNgramLoop(text, 8, 4) {
			t.Fatalf("expected true for repetitive hallucination loop")
		}
	})

	t.Run("repeating only twice does not trigger 4x threshold", func(t *testing.T) {
		phrase := "alpha beta gamma delta epsilon zeta eta theta"
		text := fmt.Sprintf("Intro text %s %s trailing text", phrase, phrase)
		if DetectNgramLoop(text, 8, 4) {
			t.Fatalf("expected false when repetition count is below threshold")
		}
	})
}

func TestCheckCallLoop(t *testing.T) {
	fp1 := FingerprintCall("subagent_task", `{"prompt":"task A"}`)
	fp2 := FingerprintCall("subagent_task", `{"prompt":"task B"}`)

	t.Run("alternating calls", func(t *testing.T) {
		history := []string{fp1, fp2, fp1, fp2}
		if err := CheckCallLoop(history, 3); err != nil {
			t.Fatalf("unexpected loop error: %v", err)
		}
	})

	t.Run("consecutive identical calls trigger circuit breaker", func(t *testing.T) {
		history := []string{fp1, fp2, fp1, fp1, fp1}
		if err := CheckCallLoop(history, 3); err != ErrIdenticalCallLoop {
			t.Fatalf("expected ErrIdenticalCallLoop, got %v", err)
		}
	})
}

func TestSwarmQuorumDecision(t *testing.T) {
	t.Run("empty batch returns immediately", func(t *testing.T) {
		ret, reason := SwarmQuorumDecision(0, 0, 10*time.Second, 0.8, 100*time.Second, 150*time.Second)
		if !ret || reason != "empty_batch" {
			t.Fatalf("expected empty_batch return, got %v, %s", ret, reason)
		}
	})

	t.Run("all completed returns immediately", func(t *testing.T) {
		ret, reason := SwarmQuorumDecision(5, 5, 30*time.Second, 0.8, 100*time.Second, 150*time.Second)
		if !ret || reason != "all_completed" {
			t.Fatalf("expected all_completed return, got %v, %s", ret, reason)
		}
	})

	t.Run("quorum reached before minWait does not return", func(t *testing.T) {
		ret, _ := SwarmQuorumDecision(4, 5, 50*time.Second, 0.8, 100*time.Second, 150*time.Second)
		if ret {
			t.Fatalf("should not return before minWait")
		}
	})

	t.Run("quorum reached after minWait returns early", func(t *testing.T) {
		ret, reason := SwarmQuorumDecision(4, 5, 105*time.Second, 0.8, 100*time.Second, 150*time.Second)
		if !ret || !strings.HasPrefix(reason, "quorum_reached") {
			t.Fatalf("expected quorum return, got %v, %s", ret, reason)
		}
	})

	t.Run("hard timeout 150s returns regardless of completion", func(t *testing.T) {
		ret, reason := SwarmQuorumDecision(1, 5, 151*time.Second, 0.8, 100*time.Second, 150*time.Second)
		if !ret || reason != "hard_timeout_150s" {
			t.Fatalf("expected hard_timeout_150s, got %v, %s", ret, reason)
		}
	})
}

func TestActiveWorker_LifecycleAndCancellation(t *testing.T) {
	pool := NewWorkerPool()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker := &ActiveWorker{
		ID:            "worker-test-1",
		Type:          "task",
		ToolName:      "subagent_task",
		HostName:      "antigravity",
		Model:         "glm-5.3",
		PromptPreview: "test prompt",
	}

	pool.RegisterWorker(worker, cancel)

	workers := pool.ListActiveWorkers()
	if len(workers) != 1 {
		t.Fatalf("expected 1 active worker, got %d", len(workers))
	}
	if workers[0].ID != "worker-test-1" {
		t.Fatalf("expected worker-test-1, got %s", workers[0].ID)
	}

	pool.TouchWorker("worker-test-1", "compiling", 300*time.Second)
	w, ok := pool.GetWorker("worker-test-1")
	if !ok || w.Phase != "compiling" || w.DeclaredLease != 300*time.Second {
		t.Fatalf("touch failed or mismatch: %+v", w)
	}

	if err := pool.KillWorker("worker-test-1"); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatalf("expected context to be cancelled after KillWorker")
	}

	workersAfter := pool.ListActiveWorkers()
	if len(workersAfter) != 0 {
		t.Fatalf("expected 0 active workers after kill, got %d", len(workersAfter))
	}
}

func TestSanitizeWorkerResult(t *testing.T) {
	t.Run("repairs truncated json in content text", func(t *testing.T) {
		raw := json.RawMessage(`{"content":[{"type":"text","text":"{\"summary\":\"done\",\"items\":[1,2"}]}`)
		sanitized := sanitizeWorkerResult(raw)

		var parsed struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(sanitized, &parsed); err != nil {
			t.Fatalf("failed to parse sanitized output: %v", err)
		}
		if !json.Valid([]byte(parsed.Content[0].Text)) {
			t.Fatalf("expected repaired valid json inside content text, got %q", parsed.Content[0].Text)
		}
	})

	t.Run("appends warning for degenerate repetitive loops", func(t *testing.T) {
		phrase := "one two three four five six seven eight"
		repeated := fmt.Sprintf("%s %s %s %s %s", phrase, phrase, phrase, phrase, phrase)
		raw := json.RawMessage(fmt.Sprintf(`{"content":[{"type":"text","text":%q}]}`, repeated))
		sanitized := sanitizeWorkerResult(raw)

		if !strings.Contains(string(sanitized), "repetitive degeneracy loop detected") {
			t.Fatalf("expected degeneracy loop warning in output, got %s", string(sanitized))
		}
	})
}
