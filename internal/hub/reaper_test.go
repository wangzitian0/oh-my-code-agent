package hub

import (
	"context"
	"testing"
	"time"
)

func TestIdleReaper_ReapIdleTool(t *testing.T) {
	sup := NewSupervisor()
	sup.RegisterTool(ToolConfig{
		Name:    "dummy",
		Command: "cat", // stays open reading stdin
	})

	tool, ok := sup.GetTool("dummy")
	if !ok {
		t.Fatalf("expected tool dummy")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start dummy tool
	if err := tool.Start(ctx); err != nil {
		t.Fatalf("failed to start tool: %v", err)
	}
	if tool.Status() != "RUNNING" {
		t.Fatalf("expected tool to be RUNNING, got %s", tool.Status())
	}

	// Create reaper with short timeout (50ms)
	reaper := NewIdleReaper(sup, 50*time.Millisecond)
	reaper.SetInterval(20 * time.Millisecond)

	// Artificially age the lastActive timestamp
	tool.mu.Lock()
	tool.lastActive = time.Now().Add(-100 * time.Millisecond)
	tool.mu.Unlock()

	// Reap once
	reaped := reaper.ReapOnce()
	if reaped != 1 {
		t.Errorf("expected 1 tool reaped, got %d", reaped)
	}

	if tool.Status() != "REGISTERED" {
		t.Errorf("expected tool status to revert to REGISTERED, got %s", tool.Status())
	}

	if reaper.ReapedCount() != 1 {
		t.Errorf("expected reaper.ReapedCount() == 1, got %d", reaper.ReapedCount())
	}

	// Calling Touch and re-starting works seamlessly
	if err := tool.Start(ctx); err != nil {
		t.Fatalf("failed to re-start tool: %v", err)
	}
	if tool.Status() != "RUNNING" {
		t.Errorf("expected RUNNING after restart, got %s", tool.Status())
	}
	_ = tool.Stop()
}
