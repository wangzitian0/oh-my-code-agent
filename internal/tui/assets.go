package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// assetBucket names one of docs/architecture/reporting.md §9's four Assets
// sub-sections.
type assetBucket int

const (
	bucketActive assetBucket = iota
	bucketAvailable
	bucketExcluded
	bucketUnknown
)

var bucketTitles = [...]string{"Active", "Available", "Excluded", "Unknown"}

// bucketFor maps a physical Candidate's full domain.SourceDisposition
// (docs/architecture/reporting.md §3 names ten values) onto the four
// buckets issue #34's own AC names. ACTIVE/AVAILABLE/EXCLUDED map exactly
// onto their own bucket; DENIED and SHADOWED are also "not effective right
// now, and why is known," so they join Excluded rather than getting a
// fifth bucket the AC never asked for. Every other disposition
// (DISCOVERED, IMPORTED, ORPHANED, OPAQUE, UNKNOWN itself) means this
// package cannot yet honestly say the asset is active, available, or
// deliberately excluded — Unknown, matching "identity, precedence, or
// behavior is not proven" (disposition.go's own doc comment for UNKNOWN).
func bucketFor(d domain.SourceDisposition) assetBucket {
	switch d {
	case domain.DispositionActive:
		return bucketActive
	case domain.DispositionAvailable:
		return bucketAvailable
	case domain.DispositionExcluded, domain.DispositionDenied, domain.DispositionShadowed:
		return bucketExcluded
	default:
		return bucketUnknown
	}
}

// RenderAssets projects every host's physical Candidate inventory
// (a.Debug[host].Candidates) into the four Active/Available/Excluded/
// Unknown buckets. Each line shows only Concept + LogicalID (the logical
// identity docs/architecture/reporting.md §9 asks the default view to
// use) — never Candidate.Ref, which is a native file path/fragment and
// belongs to Explain/Debug (issue #36), not here.
func RenderAssets(a report.Artifact) string {
	var b strings.Builder

	hosts := make([]string, 0, len(a.Debug))
	for host := range a.Debug {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	if len(hosts) == 0 {
		fmt.Fprintln(&b, "No host debug data available for this report.")
		return b.String()
	}

	for i, host := range hosts {
		if i > 0 {
			fmt.Fprintln(&b)
		}
		fmt.Fprintf(&b, "Assets — %s\n", host)
		renderHostAssets(&b, a.Debug[host].Candidates)
	}

	return b.String()
}

func renderHostAssets(b *strings.Builder, candidates []effective.Candidate) {
	var buckets [4][]effective.Candidate
	for _, c := range candidates {
		bucket := bucketFor(c.Disposition)
		buckets[bucket] = append(buckets[bucket], c)
	}

	for bucket, title := range bucketTitles {
		items := buckets[bucket]
		sort.Slice(items, func(i, j int) bool {
			if items[i].Concept != items[j].Concept {
				return items[i].Concept < items[j].Concept
			}
			return items[i].LogicalID < items[j].LogicalID
		})
		fmt.Fprintf(b, "  %s (%d)\n", title, len(items))
		for _, c := range items {
			fmt.Fprintf(b, "    %-16s %s\n", c.Concept, c.LogicalID)
		}
	}
}
