package drift

import (
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

var fixedNow = time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

// TestClassify_AllCanonicalCategories proves the engine produces every one
// of reporting.md §6's canonical base categories (all but EXCEPTION, which
// is computed, never signaled directly) with the exact assertion form:
// entity ID, field, expected value, observed/effective value, root cause,
// remediation, context cell, and evidence (docs/architecture/reporting.md
// §6).
func TestClassify_AllCanonicalCategories(t *testing.T) {
	cases := []struct {
		name     string
		sig      Signal
		wantCat  domain.DriftCategory
		wantSkip bool
	}{
		{
			name: "CONFIG_DRIFT",
			sig: Signal{
				EntityID: "asset:sandbox-mode", Field: "value",
				Category: domain.DriftConfigDrift,
				Expected: "workspace-write", Observed: "danger-full-access",
				RootCause: "company:example/security-default", Remediation: "rebuild artifact",
				Project: "infra2", Host: "codex",
				EvidenceLevel: domain.EvidenceLevelHostReported, Guarantee: domain.GuaranteeReconciled,
			},
			wantCat: domain.DriftConfigDrift,
		},
		{
			name: "EFFECTIVE_DRIFT",
			sig: Signal{
				EntityID: "asset:approval-policy", Field: "value",
				Category: domain.DriftEffectiveDrift,
				Expected: "on-request", Observed: "never",
				RootCause: "company:example/security-default", Remediation: "restart runtime",
				Project: "finance", Host: "claude-code",
				EvidenceLevel: domain.EvidenceLevelResolved,
			},
			wantCat: domain.DriftEffectiveDrift,
		},
		{
			name: "SOURCE_DRIFT",
			sig: Signal{
				EntityID: "skill:code-review", Field: "digest",
				Category: domain.DriftSourceDrift,
				Expected: "sha256:aaa", Observed: "sha256:bbb",
				RootCause: "duplicate skill definitions", Remediation: "reconcile source of truth",
				Project: "infra2", Host: "codex",
			},
			wantCat: domain.DriftSourceDrift,
		},
		{
			name: "CAPABILITY_GAP",
			sig: Signal{
				EntityID: "mcpServer:internal-docs", Field: "compile",
				Category: domain.DriftCapabilityGap,
				Expected: "adapter can normalize", Observed: "unsupported transport",
				RootCause: "codex adapter cannot compile stdio+oauth", Remediation: "file adapter capability issue",
				Project: "infra2", Host: "codex",
			},
			wantCat: domain.DriftCapabilityGap,
		},
		{
			name: "KNOWLEDGE_DRIFT",
			sig: Signal{
				EntityID: "host:codex", Field: "version",
				Category: domain.DriftKnowledgeDrift,
				Expected: "qualified <= 1.2.0", Observed: "1.4.0",
				RootCause: "host version exceeds qualified Knowledge Pack", Remediation: "qualify 1.4.0",
				Project: "infra2", Host: "codex", HostVersion: "1.4.0",
			},
			wantCat: domain.DriftKnowledgeDrift,
		},
		{
			name: "CONTEXT_DRIFT",
			sig: Signal{
				EntityID: "invocation:worktree-x", Field: "context",
				Category: domain.DriftContextDrift,
				Expected: "worktree profile", Observed: "global fallback profile",
				RootCause: "invocation bypassed worktree context", Remediation: "re-invoke inside worktree",
				Project: "infra2", Host: "claude-code",
			},
			wantCat: domain.DriftContextDrift,
		},
		{
			name: "UNKNOWN explicit",
			sig: Signal{
				EntityID: "asset:mystery", Field: "value",
				Category:  domain.DriftUnknown,
				RootCause: "cannot safely classify",
				Project:   "infra2", Host: "codex",
			},
			wantCat: domain.DriftUnknown,
		},
		{
			name: "UNKNOWN implicit (empty Category)",
			sig: Signal{
				EntityID:  "asset:mystery2",
				Field:     "value",
				RootCause: "cannot safely classify",
				Project:   "infra2", Host: "codex",
			},
			wantCat: domain.DriftUnknown,
		},
		{
			name: "equal expected/observed diff category is not drift",
			sig: Signal{
				EntityID: "asset:sandbox-mode", Field: "value",
				Category: domain.DriftConfigDrift,
				Expected: "workspace-write", Observed: "workspace-write",
				RootCause: "company:example/security-default",
				Project:   "infra2", Host: "codex",
			},
			wantSkip: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok, err := Classify(tc.sig, nil, fixedNow)
			if err != nil {
				t.Fatalf("Classify: unexpected error: %v", err)
			}
			if tc.wantSkip {
				if ok {
					t.Fatalf("Classify: want no assertion (equal expected/observed), got %+v", a)
				}
				return
			}
			if !ok {
				t.Fatalf("Classify: want an assertion, got none")
			}
			if a.Category != tc.wantCat {
				t.Errorf("Category = %q, want %q", a.Category, tc.wantCat)
			}
			if a.UnderlyingCategory != tc.wantCat {
				t.Errorf("UnderlyingCategory = %q, want %q", a.UnderlyingCategory, tc.wantCat)
			}
			if a.EntityID != tc.sig.EntityID {
				t.Errorf("EntityID = %q, want %q", a.EntityID, tc.sig.EntityID)
			}
			if a.Field != tc.sig.Field {
				t.Errorf("Field = %q, want %q", a.Field, tc.sig.Field)
			}
			if a.RootCause != tc.sig.RootCause {
				t.Errorf("RootCause = %q, want %q", a.RootCause, tc.sig.RootCause)
			}
			if a.ContextCell == "" {
				t.Errorf("ContextCell is empty, want a populated host/version/context cell")
			}
			if err := domain.ValidateDriftCategory(a.Category); err != nil {
				t.Errorf("produced Category fails domain.ValidateDriftCategory: %v", err)
			}
		})
	}
}

// TestClassify_GapCategoriesIgnoreEquality proves CAPABILITY_GAP,
// KNOWLEDGE_DRIFT, and CONTEXT_DRIFT are always reported when signaled,
// unlike the three diff categories: the signal's existence is the drift,
// independent of whether Expected/Observed happen to compare equal.
func TestClassify_GapCategoriesIgnoreEquality(t *testing.T) {
	gapCats := []domain.DriftCategory{domain.DriftCapabilityGap, domain.DriftKnowledgeDrift, domain.DriftContextDrift}
	for _, cat := range gapCats {
		sig := Signal{
			EntityID: "asset:x", Field: "gap", Category: cat,
			Expected: "same", Observed: "same",
			RootCause: "gap present", Project: "infra2", Host: "codex",
		}
		a, ok, err := Classify(sig, nil, fixedNow)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", cat, err)
		}
		if !ok {
			t.Fatalf("%s: want an assertion even with equal Expected/Observed, got none", cat)
		}
		if a.Category != cat {
			t.Errorf("%s: Category = %q, want %q", cat, a.Category, cat)
		}
	}
}

func TestClassify_RejectsMissingRequiredFields(t *testing.T) {
	base := Signal{EntityID: "e", Field: "f", RootCause: "r", Category: domain.DriftConfigDrift, Expected: 1, Observed: 2}

	missingEntity := base
	missingEntity.EntityID = ""
	if _, _, err := Classify(missingEntity, nil, fixedNow); err == nil {
		t.Error("want error for missing EntityID")
	}

	missingField := base
	missingField.Field = ""
	if _, _, err := Classify(missingField, nil, fixedNow); err == nil {
		t.Error("want error for missing Field")
	}

	missingRootCause := base
	missingRootCause.RootCause = ""
	if _, _, err := Classify(missingRootCause, nil, fixedNow); err == nil {
		t.Error("want error for missing RootCause")
	}
}

func TestClassify_RejectsExceptionAsInputCategory(t *testing.T) {
	sig := Signal{EntityID: "e", Field: "f", RootCause: "r", Category: domain.DriftException, Expected: 1, Observed: 2}
	if _, _, err := Classify(sig, nil, fixedNow); err == nil {
		t.Error("want error when Signal.Category is domain.DriftException")
	}
}

func TestClassify_RejectsUnrecognizedCategory(t *testing.T) {
	sig := Signal{EntityID: "e", Field: "f", RootCause: "r", Category: domain.DriftCategory("NOT_A_REAL_CATEGORY"), Expected: 1, Observed: 2}
	if _, _, err := Classify(sig, nil, fixedNow); err == nil {
		t.Error("want error for an unrecognized drift category")
	}
}

func TestClassifyAll_SkipsNonDriftAndFailsClosedOnInvalidSignal(t *testing.T) {
	signals := []Signal{
		{EntityID: "a", Field: "f", RootCause: "r", Category: domain.DriftConfigDrift, Expected: "x", Observed: "y"},
		{EntityID: "b", Field: "f", RootCause: "r", Category: domain.DriftConfigDrift, Expected: "same", Observed: "same"},
	}
	out, err := ClassifyAll(signals, nil, fixedNow)
	if err != nil {
		t.Fatalf("ClassifyAll: unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("ClassifyAll: got %d assertions, want 1 (the equal-value signal should be dropped)", len(out))
	}
	if out[0].EntityID != "a" {
		t.Errorf("ClassifyAll: kept entity %q, want %q", out[0].EntityID, "a")
	}

	bad := append(append([]Signal{}, signals...), Signal{Field: "f", RootCause: "r", Category: domain.DriftConfigDrift})
	if _, err := ClassifyAll(bad, nil, fixedNow); err == nil {
		t.Error("ClassifyAll: want error when one signal is structurally invalid")
	}
}
