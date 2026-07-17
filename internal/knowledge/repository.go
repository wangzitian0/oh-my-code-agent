package knowledge

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Repository is an immutable, loaded set of Knowledge Packs, typically every
// pack under knowledge/hosts/ at one commit.
type Repository struct {
	packs []Pack
}

// LoadRepository walks hostsRoot for every file named PackFileName and loads
// each with LoadPack. A structurally invalid pack anywhere under hostsRoot,
// or two packs declaring the same metadata.id, fails the whole load closed
// rather than silently skipping or shadowing a broken/duplicate pack
// (mirroring internal/ontology.LoadRegistry's discipline for a duplicate
// conceptId).
func LoadRepository(hostsRoot string) (Repository, error) {
	var packs []Pack
	seenIDs := make(map[string]string, 8) // metadata.id -> path

	err := filepath.WalkDir(hostsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != PackFileName {
			return nil
		}
		p, err := LoadPack(path)
		if err != nil {
			return err
		}
		if existing, ok := seenIDs[p.Knowledge.Metadata.ID]; ok {
			return fmt.Errorf("knowledge: LoadRepository: metadata.id %q is declared by both %s and %s; a duplicate Pack id must fail closed rather than silently shadow an earlier one", p.Knowledge.Metadata.ID, existing, path)
		}
		seenIDs[p.Knowledge.Metadata.ID] = path
		packs = append(packs, p)
		return nil
	})
	if err != nil {
		return Repository{}, fmt.Errorf("knowledge: LoadRepository: %w", err)
	}

	sort.Slice(packs, func(i, j int) bool {
		return packs[i].Knowledge.Metadata.ID < packs[j].Knowledge.Metadata.ID
	})
	return Repository{packs: packs}, nil
}

// Packs returns a copy of every loaded Pack, sorted by metadata.id.
func (r Repository) Packs() []Pack {
	out := make([]Pack, len(r.packs))
	copy(out, r.packs)
	return out
}

// defaultHostsDir locates knowledge/hosts/ relative to this source file's
// own location (the same runtime.Caller trick internal/ontology's
// defaultConceptsDir and internal/qualify's repoFixturesDir use), so it
// resolves correctly regardless of the caller's working directory.
func defaultHostsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "knowledge", "hosts")
}

var (
	defaultOnce sync.Once
	defaultRepo Repository
	defaultErr  error
)

// Default loads (once, memoized) and returns the repository's real committed
// Knowledge Packs under knowledge/hosts/.
func Default() (Repository, error) {
	defaultOnce.Do(func() {
		defaultRepo, defaultErr = LoadRepository(defaultHostsDir())
	})
	return defaultRepo, defaultErr
}

// ReconcileModeObserved is the reconcile mode an unqualified Resolution
// degrades every capability operation to: OMCA reports but does not write
// (docs/architecture/README.md §10; docs/knowledge/README.md §11: "No
// matching Pack means observation-only behavior for unresolved
// operations.").
const ReconcileModeObserved = "OBSERVED"

// Resolution is the pinned result of resolving one detected host+surface+
// exact-version against a Repository: either exactly one qualified Pack
// (PackID+Digest), or an honest "no qualified pack" outcome — never an
// error, and never an optimistic guess from a mismatched or ambiguous set
// of Packs (docs/knowledge/README.md §11).
type Resolution struct {
	Host      string `json:"host"`
	Surface   string `json:"surface"`
	Version   string `json:"version"`
	Qualified bool   `json:"qualified"`
	PackID    string `json:"packId,omitempty"`
	Digest    string `json:"digest,omitempty"`
	Reason    string `json:"reason,omitempty"`

	// pack is the matched Pack when Qualified, kept unexported so it never
	// leaks into the stable wire shape above (PackID+Digest already pin it
	// uniquely for any external consumer) while still letting CapabilityFor
	// look up real capability data in-process.
	pack *Pack
}

// Status returns the matched Pack's lifecycle state (domain.KnowledgeStatus,
// e.g. FRESH/DUE/STALE) and true when this Resolution is Qualified. An
// unqualified Resolution has no matched Pack to report a status for, so ok is
// false rather than returning a zero-value KnowledgeStatus that could be
// mistaken for a real (if unusual) lifecycle state — the same "degrade
// honestly, never guess" discipline CapabilityFor documents for its own
// unqualified/undeclared-concept cases. Exported for a report projection
// (internal/report, PR-19/issue #23's round-2 audit "per-host Knowledge
// status" requirement) to surface without reaching into Resolution's
// unexported pack field.
func (r Resolution) Status() (domain.KnowledgeStatus, bool) {
	if !r.Qualified || r.pack == nil {
		return "", false
	}
	return r.pack.Knowledge.Metadata.Status, true
}

// CapabilityFor returns the capability relations this Resolution proves for
// concept. An unqualified Resolution — or a concept the matched Pack never
// declared — degrades to ReconcileModeObserved with every capability level
// left empty, rather than an error or an optimistic guess from a mismatched
// Pack.
func (r Resolution) CapabilityFor(concept string) domain.CapabilityOps {
	if !r.Qualified || r.pack == nil {
		return domain.CapabilityOps{ReconcileMode: ReconcileModeObserved}
	}
	ops, declared := r.pack.Knowledge.Capabilities[concept]
	if !declared {
		// A Go map index on a missing key returns the zero CapabilityOps —
		// ReconcileMode: "" — which is not a valid value from
		// docs/knowledge/README.md §5's closed reconcileMode enum
		// (MANAGED/PATCHED/OBSERVED/OPAQUE/BLOCKED) and directly
		// contradicts this method's own documented degrade-to-observed
		// guarantee for a concept the matched Pack never declared (e.g.
		// "hook" or "agent" against either of this PR's two real Packs,
		// which only declare skill/instruction/mcp_server).
		return domain.CapabilityOps{ReconcileMode: ReconcileModeObserved}
	}
	return ops
}

// Resolve matches host+surface+exactVersion against every loaded Pack's
// metadata.host, metadata.surface, and metadata.versionRange
// (docs/knowledge/README.md §11's resolution formula, minus platform — see
// doc.go's discrepancy note on why platform is recorded by host detection
// but not yet enforced as a resolution filter here). Zero matches and more
// than one ambiguous match both degrade to an unqualified Resolution with an
// explanatory Reason; only exactly one match qualifies.
func (repo Repository) Resolve(host, surface, exactVersion string) Resolution {
	base := Resolution{Host: host, Surface: surface, Version: exactVersion}

	var candidates []Pack
	for _, p := range repo.packs {
		if p.Knowledge.Metadata.Host != host || p.Knowledge.Metadata.Surface != surface {
			continue
		}
		ok, err := VersionSatisfiesRange(exactVersion, p.Knowledge.Metadata.VersionRange)
		if err != nil || !ok {
			continue
		}
		candidates = append(candidates, p)
	}

	switch len(candidates) {
	case 0:
		base.Reason = fmt.Sprintf("no qualified pack: no Knowledge Pack's versionRange matches %s/%s version %s; downstream operations degrade to observed", host, surface, exactVersion)
		return base
	case 1:
		base.Qualified = true
		base.PackID = candidates[0].Knowledge.Metadata.ID
		base.Digest = candidates[0].Digest
		base.pack = &candidates[0]
		return base
	default:
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].Knowledge.Metadata.ID < candidates[j].Knowledge.Metadata.ID
		})
		ids := make([]string, len(candidates))
		for i, c := range candidates {
			ids[i] = c.Knowledge.Metadata.ID
		}
		base.Reason = fmt.Sprintf("no qualified pack: %d Knowledge Packs matched %s/%s version %s ambiguously (%s); refusing to guess, downstream operations degrade to observed", len(candidates), host, surface, exactVersion, strings.Join(ids, ", "))
		return base
	}
}
