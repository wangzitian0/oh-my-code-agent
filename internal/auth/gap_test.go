package auth

import (
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestQualificationGapSources_MatchesGenerationSourceEntryConvention proves
// this package's follow-up-issue linkage mirrors internal/runtime/
// compile.go's claudeConfigDirExclusionGapSources pattern exactly: every
// entry sets CapabilityGap and a non-empty TrackingIssue pointing at the
// real filed issue, and would pass domain.ValidateGeneration's per-entry
// rule ("capabilityGap is true but trackingIssue is empty" is rejected).
func TestQualificationGapSources_MatchesGenerationSourceEntryConvention(t *testing.T) {
	for _, host := range []string{"codex", "claude-code"} {
		entries := QualificationGapSources(host)
		if len(entries) == 0 {
			t.Fatalf("QualificationGapSources(%q) is empty", host)
		}
		for i, e := range entries {
			if e.Concept == "" {
				t.Errorf("%s entries[%d]: Concept is empty", host, i)
			}
			if e.Reason == "" {
				t.Errorf("%s entries[%d]: Reason is empty", host, i)
			}
			if e.Included {
				t.Errorf("%s entries[%d]: Included = true, want false (this is a gap, not an inclusion)", host, i)
			}
			if !e.CapabilityGap {
				t.Errorf("%s entries[%d]: CapabilityGap = false, want true", host, i)
			}
			if e.TrackingIssue != LoginQualificationIssueURL {
				t.Errorf("%s entries[%d]: TrackingIssue = %q, want %q", host, i, e.TrackingIssue, LoginQualificationIssueURL)
			}
			if e.Host != host {
				t.Errorf("%s entries[%d]: Host = %q, want %q", host, i, e.Host, host)
			}
		}
	}
}

func TestLoginQualificationIssueURL_PointsAtARealIssue(t *testing.T) {
	if !strings.HasPrefix(LoginQualificationIssueURL, "https://github.com/wangzitian0/oh-my-code-agent/issues/") {
		t.Errorf("LoginQualificationIssueURL = %q, does not look like a real issue URL", LoginQualificationIssueURL)
	}
}

// TestQualificationGapSources_ValidatesAsGenerationSourceEntries plugs the
// output directly into a minimal domain.Generation and runs the real
// ValidateGeneration over it, proving compatibility with the exact
// validation compile.go's own capability-gap entries must already satisfy
// (see internal/domain/generation.go's ValidateGeneration).
func TestQualificationGapSources_ValidatesAsGenerationSourceEntries(t *testing.T) {
	gen := domain.Generation{
		APIVersion: "omca.dev/v1alpha1",
		Kind:       "Generation",
		Metadata: domain.GenerationMetadata{
			ID:        "test-generation",
			Worktree:  "test-worktree",
			CreatedAt: "2026-07-18T00:00:00Z",
		},
		Spec: domain.GenerationSpec{
			DesiredGraphDigest: "sha256:" + strings.Repeat("a", 64),
			Hosts:              map[string]domain.GenerationHostEntry{},
			Sources:            QualificationGapSources("codex"),
		},
	}
	if err := domain.ValidateGeneration(gen); err != nil {
		t.Errorf("ValidateGeneration with QualificationGapSources entries: %v", err)
	}
}
