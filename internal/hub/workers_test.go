package hub

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHubStaticGuard_NoInstructionRenderOrCheckoutWrite verifies issue #125's
// first acceptance criterion: no file under internal/hub imports any
// instruction-rendering package or executes a checkout-writing command other
// than calling `ws-render` / `ws-publish` / `ws-mem-distill`.
func TestHubStaticGuard_NoInstructionRenderOrCheckoutWrite(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.): %v", err)
	}

	forbiddenImports := map[string]bool{
		"github.com/wangzitian0/oh-my-code-agent/internal/compile":  true,
		"github.com/wangzitian0/oh-my-code-agent/internal/profiles": true,
		"github.com/wangzitian0/oh-my-code-agent/internal/resolve":  true,
		"text/template": true,
		"html/template": true,
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("ParseFile(%s): %v", name, err)
		}

		for _, imp := range f.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			if forbiddenImports[impPath] {
				t.Errorf("%s imports forbidden rendering/compilation package %q", name, impPath)
			}
		}

		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val := strings.Trim(lit.Value, "`\"")
			for _, marker := range []string{"WS_STATIC_START", "WS_HOST_START", "CLAUDE.md", "AGENTS.md"} {
				if strings.Contains(val, marker) {
					t.Errorf("%s contains instruction-rendering literal %q; internal/hub must delegate all checkout rendering to ws-render", name, val)
				}
			}
			return true
		})
	}
}

// TestRenderCheckWorker_DetectsDriftAndRestoreClosesCard verifies issue #125's
// second acceptance criterion: editing one line of a rendered file causes a
// drift card to appear within one check interval, and calling Restore rewrites
// it via ws-render and closes the drift card.
func TestRenderCheckWorker_DetectsDriftAndRestoreClosesCard(t *testing.T) {
	tmp := t.TempDir()
	wsRoot := filepath.Join(tmp, "zitian")
	checkout := filepath.Join(wsRoot, "demo-repo")
	if err := os.MkdirAll(filepath.Join(checkout, ".ws-publish"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	expectedContent := "<!-- WS_STATIC_START -->\ncanonical rule line\n<!-- WS_STATIC_END -->\n"
	renderedFile := filepath.Join(checkout, "AGENTS.md")
	expectedFile := filepath.Join(checkout, ".ws-publish", "expected.txt")
	if err := os.WriteFile(renderedFile, []byte(expectedContent), 0644); err != nil {
		t.Fatalf("WriteFile(renderedFile): %v", err)
	}
	if err := os.WriteFile(expectedFile, []byte(expectedContent), 0644); err != nil {
		t.Fatalf("WriteFile(expectedFile): %v", err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".ws-publish", "manifest.json"), []byte(`{"schema":2}`), 0644); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	// Build a fake ws-render script that checks or restores AGENTS.md against expected.txt.
	fakeRender := filepath.Join(tmp, "ws-render")
	script := `#!/bin/sh
set -e
MODE="restore"
CHECKOUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --check|--check-files)
      MODE="check"
      shift
      ;;
    --all|--json)
      shift
      ;;
    --checkout)
      CHECKOUT="$2"
      shift 2
      ;;
    --checkout=*)
      CHECKOUT="${1#*=}"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

if [ "$MODE" = "check" ] && [ -z "$CHECKOUT" ] && [ -n "$WS_PATH" ]; then
  CO="$WS_PATH/demo-repo"
  if cmp -s "$CO/AGENTS.md" "$CO/.ws-publish/expected.txt"; then
    printf '{"checkouts":[{"path":"%s","status":"current","detail":"files-current"}]}\n' "$CO"
    exit 0
  else
    printf '{"checkouts":[{"path":"%s","status":"drifted","detail":"AGENTS.md hand-edited"}]}\n' "$CO"
    exit 3
  fi
fi

if [ "$MODE" = "restore" ] && [ -n "$CHECKOUT" ]; then
  cp "$CHECKOUT/.ws-publish/expected.txt" "$CHECKOUT/AGENTS.md"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(fakeRender, []byte(script), 0755); err != nil {
		t.Fatalf("WriteFile(fakeRender): %v", err)
	}

	cfg := &Config{
		RenderCheckInterval: "20ms",
		Profiles: map[string]ProfileConfig{
			"zitian": {
				WorkspaceRoots: []string{wsRoot},
			},
		},
	}

	worker := NewRenderCheckWorker(cfg)
	worker.SetRenderBinary(fakeRender)
	worker.SetInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Initially clean: 0 drift cards.
	if cards := worker.CheckOnce(ctx); len(cards) != 0 {
		t.Fatalf("initial CheckOnce returned %d cards, want 0: %+v", len(cards), cards)
	}

	// 2. Edit one line of the rendered file -> within one interval a drift card appears.
	if err := os.WriteFile(renderedFile, []byte("hand-edited line\n"), 0644); err != nil {
		t.Fatalf("WriteFile(hand-edit): %v", err)
	}
	worker.Start(ctx)
	defer worker.Stop()

	deadline := time.Now().Add(500 * time.Millisecond)
	var cards []RenderDriftCard
	for time.Now().Before(deadline) {
		cards = worker.Cards()
		if len(cards) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 drift card after editing rendered file, got %d: %+v", len(cards), cards)
	}
	if cards[0].CheckoutPath != checkout || cards[0].Profile != "zitian" {
		t.Errorf("unexpected drift card: %+v", cards[0])
	}

	// 3. Restore rewrites the file via ws-render and closes the drift card.
	if err := worker.Restore(ctx, checkout); err != nil {
		t.Fatalf("Restore(%s): %v", checkout, err)
	}
	if cards := worker.Cards(); len(cards) != 0 {
		t.Fatalf("expected 0 drift cards after Restore, got %d: %+v", len(cards), cards)
	}
	gotBytes, err := os.ReadFile(renderedFile)
	if err != nil {
		t.Fatalf("ReadFile(renderedFile): %v", err)
	}
	if string(gotBytes) != expectedContent {
		t.Errorf("restored file content = %q, want %q", string(gotBytes), expectedContent)
	}
}

// TestSessionWatcherWorker_DebouncesAndIsolatesProfiles verifies issue #125's
// third acceptance criterion: touching a session file runs ws-mem-distill once
// after the debounce window (advancing the ledger watermark) for the matching
// profile only, never cross-triggering another profile.
func TestSessionWatcherWorker_DebouncesAndIsolatesProfiles(t *testing.T) {
	tmp := t.TempDir()
	zitianRoot := filepath.Join(tmp, "zitian")
	shopeeRoot := filepath.Join(tmp, "workspace")

	zitianSessions := filepath.Join(zitianRoot, ".ws", ".codex", "sessions")
	shopeeSessions := filepath.Join(shopeeRoot, ".ws", ".codex", "sessions")
	if err := os.MkdirAll(zitianSessions, 0755); err != nil {
		t.Fatalf("MkdirAll(zitianSessions): %v", err)
	}
	if err := os.MkdirAll(shopeeSessions, 0755); err != nil {
		t.Fatalf("MkdirAll(shopeeSessions): %v", err)
	}

	zitianSessionFile := filepath.Join(zitianSessions, "s1.jsonl")
	shopeeSessionFile := filepath.Join(shopeeSessions, "s2.jsonl")
	baseTime := time.Now().Add(-10 * time.Minute)
	if err := os.WriteFile(zitianSessionFile, []byte(`{"step":1}`+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile(zitianSessionFile): %v", err)
	}
	if err := os.WriteFile(shopeeSessionFile, []byte(`{"step":1}`+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile(shopeeSessionFile): %v", err)
	}
	_ = os.Chtimes(zitianSessionFile, baseTime, baseTime)
	_ = os.Chtimes(shopeeSessionFile, baseTime, baseTime)

	ledgerLog := filepath.Join(tmp, "ledger.log")
	fakeDistill := filepath.Join(tmp, "ws-mem-distill")
	script := `#!/bin/sh
printf "%s|%s|%s\n" "$WS_WORKSPACE_ID" "$WS_PATH" "$1" >> "` + ledgerLog + `"
`
	if err := os.WriteFile(fakeDistill, []byte(script), 0755); err != nil {
		t.Fatalf("WriteFile(fakeDistill): %v", err)
	}

	cfg := &Config{
		Profiles: map[string]ProfileConfig{
			"zitian": {
				WorkspaceRoots: []string{zitianRoot},
			},
			"workspace": {
				WorkspaceRoots: []string{shopeeRoot},
			},
		},
	}

	watcher := NewSessionWatcherWorker(cfg)
	watcher.SetDistillBinary(fakeDistill)
	watcher.SetDebounce(50*time.Millisecond, 500*time.Millisecond)

	ctx := context.Background()
	t0 := time.Now()

	// Baseline poll: no modifications yet.
	if triggered := watcher.PollOnce(ctx, t0); len(triggered) != 0 {
		t.Fatalf("baseline PollOnce triggered %v, want none", triggered)
	}

	// Touch only the shopee ("workspace") session file.
	touchTime := time.Now().Add(time.Second)
	if err := os.WriteFile(shopeeSessionFile, []byte(`{"step":2}`+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile(shopeeSessionFile): %v", err)
	}
	_ = os.Chtimes(shopeeSessionFile, touchTime, touchTime)

	// Poll immediately at t0+10ms (before 50ms debounce): detects activity, does not fire yet.
	if triggered := watcher.PollOnce(ctx, t0.Add(10*time.Millisecond)); len(triggered) != 0 {
		t.Fatalf("PollOnce before debounce elapsed triggered %v, want none", triggered)
	}

	// Poll after debounce (t0+70ms): triggers once for "workspace" only.
	triggered := watcher.PollOnce(ctx, t0.Add(70*time.Millisecond))
	if len(triggered) != 1 || triggered[0] != "workspace" {
		t.Fatalf("PollOnce after debounce triggered %v, want [workspace]", triggered)
	}
	if got := watcher.DistillCount("workspace"); got != 1 {
		t.Errorf("DistillCount(workspace) = %d, want 1", got)
	}
	if got := watcher.DistillCount("zitian"); got != 0 {
		t.Errorf("DistillCount(zitian) = %d, want 0 (shopee session must never distill into zitian)", got)
	}

	// Subsequent poll without new file activity does not re-trigger.
	if again := watcher.PollOnce(ctx, t0.Add(150*time.Millisecond)); len(again) != 0 {
		t.Fatalf("PollOnce with no new activity triggered %v, want none", again)
	}

	logBytes, err := os.ReadFile(ledgerLog)
	if err != nil {
		t.Fatalf("ReadFile(ledgerLog): %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(logBytes)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected ledger watermark to advance once, got %d lines: %q", len(lines), string(logBytes))
	}
	wantLine := "workspace|" + shopeeRoot + "|--scan"
	if lines[0] != wantLine {
		t.Errorf("ledger line = %q, want %q", lines[0], wantLine)
	}
}
