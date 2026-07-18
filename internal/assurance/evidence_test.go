package assurance

import (
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
)

var testObservedAt = time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

func TestBuildEvidence_EveryRecordValidates(t *testing.T) {
	graph := effective.EffectiveGraph{
		Host: "codex",
		Entries: []effective.EffectiveEntry{
			trivialEntry("skill", domain.EvidenceLevelParsed),
		},
		Conflicts: []effective.Conflict{
			{Concept: "mcp_server", LogicalID: "x", EvidenceLevel: domain.EvidenceLevelDiscovered, Reason: "no qualified resolver"},
		},
	}
	hk := domain.HostKnowledge{Metadata: domain.HostKnowledgeMetadata{ID: "codex:cli:0.144"}}

	records := BuildEvidence("codex", graph, hk, testObservedAt)
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	for _, r := range records {
		if err := domain.ValidateEvidence(r); err != nil {
			t.Errorf("ValidateEvidence(%+v): %v", r, err)
		}
	}

	entryRecord := records[0]
	if entryRecord.Spec.Subject.Concept != "skill" || entryRecord.Spec.Subject.LogicalID != "id-1" {
		t.Errorf("entry record subject = %+v", entryRecord.Spec.Subject)
	}
	if entryRecord.Spec.Level != domain.EvidenceLevelParsed {
		t.Errorf("entry record level = %s, want E1", entryRecord.Spec.Level)
	}
	if entryRecord.Spec.KnowledgeRef.ID != "codex:cli:0.144" {
		t.Errorf("entry record KnowledgeRef.ID = %q, want %q", entryRecord.Spec.KnowledgeRef.ID, "codex:cli:0.144")
	}

	conflictRecord := records[1]
	if conflictRecord.Spec.Guarantee != "" {
		t.Errorf("conflict record Guarantee = %q, want empty (a Conflict has no resolved outcome to guarantee anything about)", conflictRecord.Spec.Guarantee)
	}
}

// TestBuildEvidence_UnqualifiedHost_KnowledgeRefIsHonestlyEmpty proves an
// Evidence record never claims a Knowledge Pack backing it that does not
// exist: a zero-valued domain.HostKnowledge (what internal/report/build.go
// passes for a host with no qualified Pack) must produce an empty
// KnowledgeRef, not a fabricated one.
func TestBuildEvidence_UnqualifiedHost_KnowledgeRefIsHonestlyEmpty(t *testing.T) {
	graph := effective.EffectiveGraph{
		Host:    "codex",
		Entries: []effective.EffectiveEntry{trivialEntry("skill", domain.EvidenceLevelParsed)},
	}
	records := BuildEvidence("codex", graph, domain.HostKnowledge{}, testObservedAt)
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if ref := records[0].Spec.KnowledgeRef; ref != (domain.KnowledgeRef{}) {
		t.Errorf("KnowledgeRef = %+v, want zero value for an unqualified host", ref)
	}
}

func TestHostVersionEvidence_Installed(t *testing.T) {
	det := hostcontext.HostDetection{Host: "codex", Installed: true, Version: "0.144.5", BinaryPath: "/usr/local/bin/codex"}
	got, ok := HostVersionEvidence(det, testObservedAt)
	if !ok {
		t.Fatal("expected a record for an installed host with a resolved version")
	}
	if err := domain.ValidateEvidence(got); err != nil {
		t.Errorf("ValidateEvidence: %v", err)
	}
	if got.Spec.Level != domain.EvidenceLevelHostReported {
		t.Errorf("Level = %s, want E3", got.Spec.Level)
	}
	if got.Spec.Subject.Concept != HostConceptClaim || got.Spec.Subject.LogicalID != "codex" {
		t.Errorf("Subject = %+v", got.Spec.Subject)
	}
}

func TestHostVersionEvidence_NotInstalled(t *testing.T) {
	det := hostcontext.HostDetection{Host: "codex", Installed: false}
	if _, ok := HostVersionEvidence(det, testObservedAt); ok {
		t.Error("expected no record for a host that is not installed")
	}
}

func TestHostVersionEvidence_ProbeError(t *testing.T) {
	det := hostcontext.HostDetection{Host: "codex", Installed: true, Error: "could not parse --version output"}
	if _, ok := HostVersionEvidence(det, testObservedAt); ok {
		t.Error("expected no record when the version probe recorded a non-fatal error -- an unparsed version is not a claim to make")
	}
}
