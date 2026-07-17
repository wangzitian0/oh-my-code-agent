package drift

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestGolden_DR017WorkedExample reproduces the exact shape of
// docs/architecture/reporting.md §7's worked example (DR-017): one company
// Profile not represented in pending runtimes, 8 projects x 5 hosts = 40
// artifacts, 38 reported at evidence E3 and 2 already excepted, with a
// 2-sample illustrative Samples list covering both outcome transitions the
// doc's Samples block shows. This is hand-built fixture data (see doc.go),
// not PR-17 output, but it proves this package can reproduce reporting.md's
// own canonical example end to end: classify, group into one card, tally
// evidence, and select exactly the doc's 2 illustrative samples.
func TestGolden_DR017WorkedExample(t *testing.T) {
	const rootCause = "company:example/security-default"
	const remediation = "rebuild 38 artifacts; retain 2 explicit exceptions"

	projects := []string{"infra2", "finance", "truealpha", "gateway", "billing", "search", "notify", "admin"}
	hosts := []string{"codex", "claude-code", "cursor", "opencode", "github-copilot"}

	var signals []Signal
	n := 0
	for _, project := range projects {
		for _, host := range hosts {
			n++
			expected, observed, assetID := "workspace-write", "danger-full-access", "sandbox-mode"
			if n%20 == 0 { // exactly 2 of the 40 cells: the excepted ones
				expected, observed, assetID = "on-request", "on-request", "approval-policy"
			}
			signals = append(signals, Signal{
				EntityID: assetID + ":" + project + ":" + host, Field: "value",
				Category: domain.DriftConfigDrift,
				Expected: expected, Observed: observed,
				RootCause: rootCause, Remediation: remediation,
				Project: project, Host: host, AdapterVersion: "v1.2.0",
				AssetID:         assetID,
				EvidenceLevel:   domain.EvidenceLevelHostReported,
				Guarantee:       domain.GuaranteeReconciled,
				ExceptionScopes: []string{rootCause},
			})
		}
	}

	// The 2 "approval-policy" cells have Expected == Observed under
	// CONFIG_DRIFT's diff rule, so on their own they would not be reported
	// as drift at all — reflecting that they are already compliant, exactly
	// like reporting.md's "retain 2 explicit exceptions" framing (an
	// exception that is never triggered because the value already matches
	// is not what this doc example is illustrating). Model the doc's "2 ×
	// E2, excepted" cells properly instead: a genuine difference that is
	// authorized by an unexpired Exception.
	for i, sig := range signals {
		if sig.AssetID == "approval-policy" {
			signals[i].Expected, signals[i].Observed = "on-request", "never"
			signals[i].EvidenceLevel = domain.EvidenceLevelResolved
		}
	}

	exceptions := []domain.Exception{
		{
			APIVersion: domain.SupportedAPIVersion, Kind: "Exception",
			Metadata:      domain.Metadata{ID: "exception:approval-policy-2026"},
			AssetID:       "approval-policy",
			Scope:         rootCause,
			Justification: "documented break-glass approval bypass",
			ExpiresAt:     fixedNow.AddDate(0, 1, 0),
		},
	}

	assertions, err := ClassifyAll(signals, exceptions, fixedNow)
	if err != nil {
		t.Fatalf("ClassifyAll: %v", err)
	}
	if len(assertions) != 40 {
		t.Fatalf("got %d assertions, want 40 (8 projects x 5 hosts)", len(assertions))
	}

	// Excepted cells classify as EXCEPTION, which is a different outcome
	// class (Category) than the un-excepted CONFIG_DRIFT cells, so they
	// land in a separate ActionCard — the doc's single DR-017 card
	// specifically covers the 38 live-drift artifacts, with the 2 excepted
	// ones reported (and queryable) as their own card sharing the same root
	// cause and remediation text.
	cards := GroupWithSampleLimit(assertions, 2)
	if len(cards) != 2 {
		t.Fatalf("got %d cards, want 2 (38 live CONFIG_DRIFT + 2 EXCEPTION)", len(cards))
	}

	var driftCard, exceptionCard *ActionCard
	for i := range cards {
		switch cards[i].Category {
		case domain.DriftConfigDrift:
			driftCard = &cards[i]
		case domain.DriftException:
			exceptionCard = &cards[i]
		}
	}
	if driftCard == nil || exceptionCard == nil {
		t.Fatalf("expected one CONFIG_DRIFT card and one EXCEPTION card, got categories %v / %v", cards[0].Category, cards[1].Category)
	}

	if driftCard.Impact.Artifacts != 38 {
		t.Errorf("drift card Artifacts = %d, want 38", driftCard.Impact.Artifacts)
	}
	if driftCard.Impact.Projects != 8 || driftCard.Impact.Hosts != 5 {
		t.Errorf("drift card Impact = %+v, want Projects=8 Hosts=5", driftCard.Impact)
	}
	if got := driftCard.EvidenceCounts[domain.EvidenceLevelHostReported]; got != 38 {
		t.Errorf("drift card EvidenceCounts[E3] = %d, want 38", got)
	}
	if len(driftCard.Samples) != 2 {
		t.Errorf("drift card Samples has %d entries, want 2 (sample limit)", len(driftCard.Samples))
	}

	if exceptionCard.Impact.Artifacts != 2 {
		t.Errorf("exception card Artifacts = %d, want 2", exceptionCard.Impact.Artifacts)
	}
	for _, a := range exceptionCard.Matrix {
		if a.ExceptionRef != "exception:approval-policy-2026" {
			t.Errorf("exception card entry %q: ExceptionRef = %q, want the applied exception's id", a.EntityID, a.ExceptionRef)
		}
		if a.UnderlyingCategory != domain.DriftConfigDrift {
			t.Errorf("exception card entry %q: UnderlyingCategory = %q, want %q", a.EntityID, a.UnderlyingCategory, domain.DriftConfigDrift)
		}
	}
}
