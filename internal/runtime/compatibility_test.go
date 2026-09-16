package runtime

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func writeAuditManifest(t *testing.T, fx activateFixture) {
	t.Helper()
	path := filepath.Join(fx.outputDir, "manifest.json")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatal(err)
		}
	}()
	data, err := json.Marshal(fx.gen)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fx.outputDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRollback_CompileTargetRejectsWithoutMutatingState(t *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.GenerationHostEntry, *hostcontext.HostDetection)
	}{
		{"upgraded", func(e *domain.GenerationHostEntry, d *hostcontext.HostDetection) { d.Version = "999.0.0" }},
		{"downgraded", func(e *domain.GenerationHostEntry, d *hostcontext.HostDetection) { d.Version = "0.1.0" }},
		{"unknown-current", func(e *domain.GenerationHostEntry, d *hostcontext.HostDetection) { d.Version = "" }},
		{"legacy-parent", func(e *domain.GenerationHostEntry, d *hostcontext.HostDetection) { e.HostVersion = "" }},
		{"unknown-surface", func(e *domain.GenerationHostEntry, d *hostcontext.HostDetection) { e.Surface = "" }},
		{"wrong-surface", func(e *domain.GenerationHostEntry, d *hostcontext.HostDetection) { d.Surface = "vscode" }},
		{"wrong-host", func(e *domain.GenerationHostEntry, d *hostcontext.HostDetection) { d.Host = "claude-code" }},
		{"detection-error", func(e *domain.GenerationHostEntry, d *hostcontext.HostDetection) { d.Error = "version probe failed" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
			state := t.TempDir()
			parent := compileFixture(t, state, nil, nil, now)
			id := parent.gen.Metadata.ID
			child := compileFixture(t, state, []domain.Profile{requiredSkillProfile("personal:audit", "review")}, &id, now)
			detection := child.req.Hosts[0].Detection
			if err := SetCurrentGeneration(state, "codex", child.outputDir, child.gen, detection, now); err != nil {
				t.Fatal(err)
			}
			entry := parent.gen.Spec.Hosts["codex"]
			tc.change(&entry, &detection)
			parent.gen.Spec.Hosts["codex"] = entry
			writeAuditManifest(t, parent)
			recordBefore, err := os.ReadFile(pointerRecordPath(state, "current", "codex"))
			if err != nil {
				t.Fatal(err)
			}
			manifestBefore, err := os.ReadFile(filepath.Join(parent.outputDir, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := AppendLedgerEntry(state, "codex", LedgerEntry{Host: "codex", GenerationID: child.gen.Metadata.ID, Kind: "activated", RecordedAt: now.Format(time.RFC3339)}); err != nil {
				t.Fatal(err)
			}
			ledgerBefore, err := os.ReadFile(ledgerPath(state, "codex"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Rollback(state, filepath.Join(state, "generations"), "codex", detection, now); err == nil {
				t.Fatal("accepted unknown/incompatible rollback")
			}
			current, err := CurrentGenerationDir(state, "codex")
			if err != nil || current != child.outputDir {
				t.Fatalf("current changed: %s %v", current, err)
			}
			for path, want := range map[string][]byte{pointerRecordPath(state, "current", "codex"): recordBefore, filepath.Join(parent.outputDir, "manifest.json"): manifestBefore, ledgerPath(state, "codex"): ledgerBefore} {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("state changed after rejection: %s %v", path, err)
				}
			}
		})
	}
}

func TestLaunch_CompileTargetCannotBeRelabeledByCurrentRecord(t *testing.T) {
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	state := t.TempDir()
	fx := compileFixture(t, state, []domain.Profile{requiredSkillProfile("personal:audit", "review")}, nil, now)
	detection := fx.req.Hosts[0].Detection
	detection.Version = "999.0.0"
	if err := SetCurrentGeneration(state, "codex", fx.outputDir, fx.gen, detection, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureLaunchGeneration(BootstrapRequest{Detection: detection, Worktree: fx.req.Worktree, Now: now}, state); err == nil {
		t.Fatal("mutable record relabeled compiled host target")
	}
}

func TestLaunch_LegacySelectedGenerationRemainsUsable(t *testing.T) {
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	state := t.TempDir()
	fx := compileFixture(t, state, []domain.Profile{requiredSkillProfile("personal:audit", "review")}, nil, now)
	entry := fx.gen.Spec.Hosts["codex"]
	entry.HostVersion = ""
	fx.gen.Spec.Hosts["codex"] = entry
	writeAuditManifest(t, fx)
	detection := fx.req.Hosts[0].Detection
	if err := SetCurrentGeneration(state, "codex", fx.outputDir, fx.gen, detection, now); err != nil {
		t.Fatal(err)
	}
	got, _, err := EnsureLaunchGeneration(BootstrapRequest{Detection: detection, Worktree: fx.req.Worktree, Now: now}, state)
	if err != nil || got.Metadata.ID != fx.gen.Metadata.ID {
		t.Fatalf("legacy launch regressed: %v", err)
	}
	detection.Version = "999.0.0"
	if _, _, err := EnsureLaunchGeneration(BootstrapRequest{Detection: detection, Worktree: fx.req.Worktree, Now: now}, state); err == nil {
		t.Fatal("legacy record version guard lost")
	}
}

func TestAutomaticRollback_RejectsUnprovenParent(t *testing.T) {
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	state := t.TempDir()
	parent := compileFixture(t, state, nil, nil, now)
	id := parent.gen.Metadata.ID
	child := compileFixture(t, state, []domain.Profile{requiredSkillProfile("personal:audit", "review")}, &id, now)
	entry := parent.gen.Spec.Hosts["codex"]
	entry.HostVersion = "999.0.0"
	parent.gen.Spec.Hosts["codex"] = entry
	writeAuditManifest(t, parent)
	if err := SetPendingGeneration(state, "codex", child.outputDir, child.gen, child.req.Hosts[0].Detection, now); err != nil {
		t.Fatal(err)
	}
	tamperArtifact(t, child.outputDir, codexConfigTOMLRelPath)
	result, err := ActivateAndVerify(ActivateRequest{WorktreeStateDir: state, Host: "codex", Fresh: child.req, Now: now}, filepath.Join(state, "generations"))
	if err == nil || result.RolledBack {
		t.Fatalf("false automatic recovery: %+v %v", result, err)
	}
	current, err := CurrentGenerationDir(state, "codex")
	if err != nil || current != child.outputDir {
		t.Fatalf("unexpected parent switch: %s %v", current, err)
	}
	entries, err := ReadLedger(state, "codex")
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for _, entry := range entries {
		if entry.Kind == "rolledback" {
			t.Fatal("false rollback success entry")
		}
		if entry.Kind == "verification-failed" {
			failed = true
		}
	}
	if !failed {
		t.Fatal("verification failure not retained")
	}
}
