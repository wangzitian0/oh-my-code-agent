package perf

import (
	"fmt"
	"sort"
	"time"
)

// Stats summarizes a small set of repeated timing samples for one measured
// phase (e.g. "first bootstrap" or "steady-state shim entry"). Reporting
// Min/Mean/Max/P95 rather than a single number is deliberate: a single
// sample on a shared, potentially loaded machine (a laptop with other
// processes running, a CI runner under contention) is not representative,
// and the M1 exit gate's own language ("the measured numbers are part of
// the report, not a hidden benchmark") asks for numbers a reader can
// actually trust, not one cherry-picked run. There is deliberately no
// exported Samples field carrying the raw per-run slice: nothing in this
// package or its callers (perf_test.go's own t.Logf output, the CI ceiling
// assertions) consumes anything finer-grained than these four summary
// values, and this project's own stated quality bar rejects speculative
// fields ahead of an actual caller (see cmd/omca/statedir.go's identical
// judgment call for why it has no realCacheRoot counterpart).
type Stats struct {
	Count int           `json:"count"`
	Min   time.Duration `json:"min"`
	Mean  time.Duration `json:"mean"`
	P95   time.Duration `json:"p95"`
	Max   time.Duration `json:"max"`
}

// computeStats builds Stats from samples. An empty input returns a
// zero-valued Stats (Count: 0) rather than panicking or dividing by zero —
// the expected shape when, e.g., a real-environment measurement finds zero
// installed hosts to time.
func computeStats(samples []time.Duration) Stats {
	if len(samples) == 0 {
		return Stats{}
	}
	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var sum time.Duration
	for _, s := range sorted {
		sum += s
	}
	p95Index := (len(sorted)*95 + 99) / 100 // ceil(95th percentile), clamped below
	if p95Index >= len(sorted) {
		p95Index = len(sorted) - 1
	}

	return Stats{
		Count: len(sorted),
		Min:   sorted[0],
		Mean:  sum / time.Duration(len(sorted)),
		P95:   sorted[p95Index],
		Max:   sorted[len(sorted)-1],
	}
}

// String renders Stats as a compact, human-readable one-liner for `make
// perf`'s own console output.
func (s Stats) String() string {
	if s.Count == 0 {
		return "no samples"
	}
	return fmt.Sprintf("n=%d min=%s mean=%s p95=%s max=%s", s.Count, s.Min, s.Mean, s.P95, s.Max)
}
