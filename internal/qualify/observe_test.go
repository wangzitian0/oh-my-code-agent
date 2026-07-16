package qualify

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func TestObserveSandboxFindsPlantedFiles(t *testing.T) {
	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(sb.homeAGENTSPath(), "user instructions", 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(sb.projectAGENTSPath(), "project instructions", 0o644); err != nil {
		t.Fatal(err)
	}

	rules := []ObservationRule{
		{Root: "home", Concept: "instruction", Scope: "user", Surface: "cli"},
		{Root: "project", Concept: "instruction", Scope: "workspace", Surface: "cli"},
	}

	observations, err := ObserveSandbox(sb, "codex", "0.144.5", rules)
	if err != nil {
		t.Fatalf("ObserveSandbox: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("len(observations) = %d, want 2: %+v", len(observations), observations)
	}
	for _, o := range observations {
		if o.Spec.Host.ID != "codex" || o.Spec.Host.Version != "0.144.5" {
			t.Errorf("observation host = %+v, want codex/0.144.5", o.Spec.Host)
		}
		if o.Spec.Disposition != domain.DispositionDiscovered {
			t.Errorf("disposition = %v, want DISCOVERED", o.Spec.Disposition)
		}
		if o.Spec.EvidenceLevel != domain.EvidenceLevelParsed {
			t.Errorf("evidenceLevel = %v, want E1", o.Spec.EvidenceLevel)
		}
		if err := domain.ValidateObservation(o); err != nil {
			t.Errorf("built observation failed ValidateObservation: %v", err)
		}
	}
	// Deterministic, path-relative (not absolute-temp-dir) source paths.
	if observations[0].Spec.Source.Path != "home/AGENTS.md" && observations[0].Spec.Source.Path != "project/AGENTS.md" {
		t.Errorf("unexpected source path %q", observations[0].Spec.Source.Path)
	}
}

func TestObserveSandboxMissingRootIsSkipped(t *testing.T) {
	sb, err := NewSandbox(t.TempDir(), "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	rules := []ObservationRule{
		{Root: "claude-config", Concept: "skill", Scope: "user", Surface: "cli"},
	}
	observations, err := ObserveSandbox(sb, "claude-code", "2.1.211", rules)
	if err != nil {
		t.Fatalf("ObserveSandbox: %v", err)
	}
	if len(observations) != 0 {
		t.Errorf("observations = %v, want empty (empty root)", observations)
	}
}

func TestObserveSandboxUnknownRootNameYieldsNothing(t *testing.T) {
	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	rules := []ObservationRule{
		{Root: "claude-config", Concept: "skill", Scope: "user", Surface: "cli"},
	}
	observations, err := ObserveSandbox(sb, "codex", "0.144.5", rules)
	if err != nil {
		t.Fatalf("ObserveSandbox: %v", err)
	}
	if len(observations) != 0 {
		t.Errorf("observations = %v, want empty (codex sandbox has no claude-config root)", observations)
	}
}

// homeAGENTSPath / projectAGENTSPath are tiny test-only helpers to keep the
// tests above readable.
func (sb *Sandbox) homeAGENTSPath() string {
	return sb.Home + "/AGENTS.md"
}

func (sb *Sandbox) projectAGENTSPath() string {
	return sb.Project + "/AGENTS.md"
}
