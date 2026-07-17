package drift

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// shuffledCopy returns a copy of signals with elements reordered by r,
// mirroring internal/resolve/determinism_test.go's technique of varying
// input construction order and proving the output is unaffected: Group must
// not depend on the order ClassifyAll's caller happened to build or supply
// signals in.
func shuffledCopy(signals []Signal, r *rand.Rand) []Signal {
	out := append([]Signal(nil), signals...)
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// TestGroup_Deterministic is this package's determinism proof, matching
// internal/resolve's TestResolve_Deterministic rigor: Group (fed by
// ClassifyAll) must be a pure function of the *set* of input signals, not
// their order. It runs the full ClassifyAll->Group pipeline many times on
// randomly reordered copies of the same logical signal set (the 8
// projects x 2 hosts fixture, which also exercises multi-bucket sample
// selection) and asserts every run is both reflect.DeepEqual and
// byte-identical via domain.CanonicalDigest.
func TestGroup_Deterministic(t *testing.T) {
	base := eightProjectsTwoHosts()
	r := rand.New(rand.NewSource(42))

	const iterations = 25
	var allCards [][]ActionCard
	var digests []string
	for i := 0; i < iterations; i++ {
		signals := shuffledCopy(base, r)

		assertions, err := ClassifyAll(signals, nil, fixedNow)
		if err != nil {
			t.Fatalf("iteration %d: ClassifyAll: %v", i, err)
		}
		// Classification output order tracks input order by design
		// (ClassifyAll preserves order); Group is what must normalize it.
		cards := Group(assertions)
		allCards = append(allCards, cards)

		digest, err := domain.CanonicalDigest(cards)
		if err != nil {
			t.Fatalf("iteration %d: CanonicalDigest: %v", i, err)
		}
		digests = append(digests, digest)
	}

	for i := 1; i < len(allCards); i++ {
		if !reflect.DeepEqual(allCards[0], allCards[i]) {
			t.Fatalf("run 0 and run %d produced different ActionCards for the same signal set (different order):\n%+v\nvs\n%+v", i, allCards[0], allCards[i])
		}
		if digests[0] != digests[i] {
			t.Fatalf("run 0 and run %d produced different digests for the same signal set: %s vs %s", i, digests[0], digests[i])
		}
	}
}

// TestGroup_Deterministic_MultipleRootCauses extends the determinism proof
// to a set of signals spanning several distinct root causes, categories,
// and adapter versions (several ActionCards, not just one), so card
// *ordering* — not only each card's internal Matrix/Samples ordering — is
// proven order-independent too.
func TestGroup_Deterministic_MultipleRootCauses(t *testing.T) {
	var base []Signal
	base = append(base, eightProjectsTwoHosts()...)
	base = append(base,
		Signal{
			EntityID: "mcpServer:internal-docs", Field: "compile", Category: domain.DriftCapabilityGap,
			Expected: "adapter can normalize", Observed: "unsupported transport",
			RootCause: "codex adapter cannot compile stdio+oauth", Remediation: "file adapter capability issue",
			Project: "infra2", Host: "codex", AdapterVersion: "v1.2.0",
		},
		Signal{
			EntityID: "host:codex", Field: "version", Category: domain.DriftKnowledgeDrift,
			Expected: "qualified <= 1.2.0", Observed: "1.4.0",
			RootCause: "host version exceeds qualified Knowledge Pack", Remediation: "qualify 1.4.0",
			Project: "finance", Host: "codex", AdapterVersion: "v1.3.0",
		},
	)

	r := rand.New(rand.NewSource(7))
	const iterations = 15
	var allCards [][]ActionCard
	for i := 0; i < iterations; i++ {
		signals := shuffledCopy(base, r)
		assertions, err := ClassifyAll(signals, nil, fixedNow)
		if err != nil {
			t.Fatalf("iteration %d: ClassifyAll: %v", i, err)
		}
		allCards = append(allCards, Group(assertions))
	}
	if len(allCards[0]) != 3 {
		t.Fatalf("want 3 distinct action cards (3 distinct root causes), got %d", len(allCards[0]))
	}
	for i := 1; i < len(allCards); i++ {
		if !reflect.DeepEqual(allCards[0], allCards[i]) {
			t.Fatalf("run 0 and run %d produced different card sets/order for the same signals:\n%+v\nvs\n%+v", i, allCards[0], allCards[i])
		}
	}
}
