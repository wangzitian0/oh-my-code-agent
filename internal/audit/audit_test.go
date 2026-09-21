package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBarrierInformationIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "barrier_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create files: 2 docs, 2 sources, 1 test
	_ = os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Title\nPromise raft"), 0644)
	docsSubdir := filepath.Join(tmpDir, "docs")
	_ = os.MkdirAll(docsSubdir, 0755)
	_ = os.WriteFile(filepath.Join(docsSubdir, "architecture.md"), []byte("Design doc"), 0644)

	_ = os.WriteFile(filepath.Join(tmpDir, "server.go"), []byte("package main\nfunc Run(){}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "util.py"), []byte("def help(): pass"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "server_test.go"), []byte("package main\nimport \"testing\"\nfunc TestRun(t *testing.T){}"), 0644)

	ctx, err := BuildBarrier(tmpDir)
	if err != nil {
		t.Fatalf("BuildBarrier failed: %v", err)
	}

	if len(ctx.DocFiles) != 2 {
		t.Errorf("expected 2 doc files, got %d", len(ctx.DocFiles))
	}

	// Verify hard information barrier: BlindfoldFiles must contain NO doc files
	for _, f := range ctx.BlindfoldFiles {
		if f.IsDoc {
			t.Errorf("information barrier breached! BlindfoldFiles contains doc: %s", f.RelPath)
		}
	}

	if len(ctx.BlindfoldFiles) != 3 { // server.go, util.py, server_test.go
		t.Errorf("expected 3 blindfold files, got %d", len(ctx.BlindfoldFiles))
	}
}

func TestSynthesizeDoomsdayAudit(t *testing.T) {
	findings := []ScoutFinding{
		{
			Category: CatModuleContract,
			Scout:    ScoutM1,
			Topic:    "Breaking API Change",
			Severity: "HIGH",
			Details:  "Renamed exported method Execute() to Run() without backward alias",
			Evidence: "server.go:12",
		},
		{
			Category: CatEngineeringBlind,
			Scout:    ScoutG1,
			Topic:    "Goroutine Leak",
			Severity: "CRITICAL",
			Details:  "Worker loop does not listen to ctx.Done()",
			Evidence: "pool.go:88",
		},
		{
			Category: CatGoalCompleteness,
			Scout:    ScoutT1,
			Topic:    "Happy Path Only",
			Severity: "MEDIUM",
			Details:  "Network retry not wired",
			Evidence: "client.go:34",
		},
	}

	res := SynthesizeDoomsdayAudit("/sample/project", findings)

	if res.TotalScoutsDeployed != 9 {
		t.Errorf("expected 9 scouts deployed, got %d", res.TotalScoutsDeployed)
	}

	if res.Verdict != VerdictBlocked {
		t.Errorf("expected VerdictBlocked due to critical findings, got %s", res.Verdict)
	}

	if len(res.CriticalBlockers) != 2 { // HIGH and CRITICAL count as blockers
		t.Errorf("expected 2 critical blockers, got %d", len(res.CriticalBlockers))
	}

	md := FormatMarkdown(res)
	if !strings.Contains(md, "9 马仔末日代码审计报告") {
		t.Errorf("markdown report missing header")
	}
	if !strings.Contains(md, "Goroutine Leak") {
		t.Errorf("markdown report missing finding")
	}
}
