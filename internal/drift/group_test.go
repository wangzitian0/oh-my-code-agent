package drift

import (
	"fmt"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// eightProjectsTwoHosts builds the round-3-note fixture: one root cause
// (one company Profile not represented in pending runtimes, mirroring
// reporting.md §7's DR-017 worked example) manifesting across 8 projects x 2
// hosts = 16 affected entities. It is hand-built, PR-17-shaped synthetic
// data (see doc.go's "Scope and PR-17 dependency" section) — not real
// resolver output. Half the cells (the "codex" host) show one outcome
// transition, half (the "claude-code" host) show a different one, so the
// fixture also exercises sample-bucket coverage across more than one
// distinct outcome.
func eightProjectsTwoHosts() []Signal {
	projects := []string{"infra2", "finance", "truealpha", "gateway", "billing", "search", "notify", "admin"}
	hosts := []string{"codex", "claude-code"}

	var signals []Signal
	for _, project := range projects {
		for _, host := range hosts {
			expected, observed := "workspace-write", "danger-full-access"
			if host == "claude-code" {
				expected, observed = "on-request", "never"
			}
			signals = append(signals, Signal{
				EntityID: fmt.Sprintf("asset:%s:%s:sandbox-mode", project, host),
				Field:    "value",
				Category: domain.DriftConfigDrift,
				Expected: expected, Observed: observed,
				RootCause:   "company:example/security-default",
				Remediation: "rebuild pending runtime with company baseline",
				Project:     project, Host: host, AdapterVersion: "v1.2.0",
				EvidenceLevel: domain.EvidenceLevelHostReported,
				Guarantee:     domain.GuaranteeReconciled,
			})
		}
	}
	return signals
}

// TestGroup_EightProjectsTwoHosts_OneActionCard is issue #22's named
// grouping AC: "one root cause across 8 projects x 2 hosts produces exactly
// one action card with correct impact counts." The integration form of this
// AC (real PR-17 graphs feeding this engine) is not yet testable — PR-17
// has not landed (see doc.go) — so this proves the grouping/impact-counting
// logic itself against a fixture shaped like that eventual real input.
func TestGroup_EightProjectsTwoHosts_OneActionCard(t *testing.T) {
	assertions, err := ClassifyAll(eightProjectsTwoHosts(), nil, fixedNow)
	if err != nil {
		t.Fatalf("ClassifyAll: %v", err)
	}
	if len(assertions) != 16 {
		t.Fatalf("ClassifyAll: got %d assertions, want 16 (8 projects x 2 hosts)", len(assertions))
	}

	cards := Group(assertions)
	if len(cards) != 1 {
		t.Fatalf("Group: got %d action cards, want exactly 1; cards=%+v", len(cards), cards)
	}

	card := cards[0]
	wantImpact := Impact{Projects: 8, Hosts: 2, Artifacts: 16}
	if card.Impact != wantImpact {
		t.Errorf("Impact = %+v, want %+v", card.Impact, wantImpact)
	}
	if card.RootCause != "company:example/security-default" {
		t.Errorf("RootCause = %q, want %q", card.RootCause, "company:example/security-default")
	}
	if card.Category != domain.DriftConfigDrift {
		t.Errorf("Category = %q, want %q", card.Category, domain.DriftConfigDrift)
	}
	if len(card.Matrix) != 16 {
		t.Fatalf("Matrix has %d entries, want 16", len(card.Matrix))
	}
	if got := card.EvidenceCounts[domain.EvidenceLevelHostReported]; got != 16 {
		t.Errorf("EvidenceCounts[E3] = %d, want 16", got)
	}
	if card.Guarantee != domain.GuaranteeReconciled {
		t.Errorf("Guarantee = %q, want %q (uniform across the fixture)", card.Guarantee, domain.GuaranteeReconciled)
	}
}

// TestActionCard_MatrixIsQueryableForEveryEntity is the "full matrix stays
// queryable from every card" AC (reporting.md §7 and §14's debug
// invariant): every one of the 16 affected entities must be individually
// findable through the single action card's Query method.
func TestActionCard_MatrixIsQueryableForEveryEntity(t *testing.T) {
	signals := eightProjectsTwoHosts()
	assertions, err := ClassifyAll(signals, nil, fixedNow)
	if err != nil {
		t.Fatalf("ClassifyAll: %v", err)
	}
	cards := Group(assertions)
	if len(cards) != 1 {
		t.Fatalf("want 1 card, got %d", len(cards))
	}
	card := cards[0]

	for _, sig := range signals {
		got := card.Query(sig.EntityID)
		if len(got) != 1 {
			t.Errorf("Query(%q) returned %d entries, want 1", sig.EntityID, len(got))
			continue
		}
		if got[0].Project != sig.Project || got[0].Host != sig.Host {
			t.Errorf("Query(%q) = project %q host %q, want project %q host %q", sig.EntityID, got[0].Project, got[0].Host, sig.Project, sig.Host)
		}
	}

	if got := card.Query("asset:does-not-exist"); got != nil {
		t.Errorf("Query for an absent entity returned %v, want nil", got)
	}
}

// TestSelectSamples_CoversDistinctBucketsBeforeRedundancy proves sample
// selection is deterministic and lists one representative of each distinct
// outcome bucket before any redundant repeat (reporting.md §7). The fixture
// has exactly 2 distinct outcome transitions (one per host), so a limit of
// 2 must yield exactly those two representatives, and a limit of 3 must add
// exactly one redundant entry after both buckets are covered.
func TestSelectSamples_CoversDistinctBucketsBeforeRedundancy(t *testing.T) {
	assertions, err := ClassifyAll(eightProjectsTwoHosts(), nil, fixedNow)
	if err != nil {
		t.Fatalf("ClassifyAll: %v", err)
	}

	cardsLimit2 := GroupWithSampleLimit(assertions, 2)
	samples2 := cardsLimit2[0].Samples
	if len(samples2) != 2 {
		t.Fatalf("limit=2: got %d samples, want 2", len(samples2))
	}
	buckets := map[string]bool{}
	for _, s := range samples2 {
		buckets[outcomeBucketKey(s)] = true
	}
	if len(buckets) != 2 {
		t.Errorf("limit=2: samples cover %d distinct buckets, want 2 (both outcome transitions before any redundancy)", len(buckets))
	}

	cardsLimit3 := GroupWithSampleLimit(assertions, 3)
	samples3 := cardsLimit3[0].Samples
	if len(samples3) != 3 {
		t.Fatalf("limit=3: got %d samples, want 3", len(samples3))
	}
	// The first two samples must be identical to the limit=2 case (bucket
	// coverage is prioritized identically regardless of how much redundancy
	// budget follows).
	if samples3[0] != samples2[0] || samples3[1] != samples2[1] {
		t.Errorf("limit=3: first two samples differ from limit=2's bucket-covering samples:\n%+v\nvs\n%+v", samples3[:2], samples2)
	}
}

// TestGroup_DifferentRootCausesProduceSeparateCards is a basic negative
// control on the grouping key: two assertions with different root causes
// (even if everything else matches) must never collapse into one card.
func TestGroup_DifferentRootCausesProduceSeparateCards(t *testing.T) {
	signals := []Signal{
		{EntityID: "a", Field: "f", Category: domain.DriftConfigDrift, Expected: "x", Observed: "y", RootCause: "cause-1", Project: "p", Host: "h"},
		{EntityID: "b", Field: "f", Category: domain.DriftConfigDrift, Expected: "x", Observed: "y", RootCause: "cause-2", Project: "p", Host: "h"},
	}
	assertions, err := ClassifyAll(signals, nil, fixedNow)
	if err != nil {
		t.Fatalf("ClassifyAll: %v", err)
	}
	cards := Group(assertions)
	if len(cards) != 2 {
		t.Fatalf("got %d cards, want 2 (different root causes)", len(cards))
	}
}
