package effective

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/qualify"
)

// repoRoot locates this repository's root relative to this source file's own
// location (the same runtime.Caller trick internal/ontology's
// defaultConceptsDir and internal/qualify's repoFixturesDir use), so this
// test resolves correctly regardless of the caller's working directory.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// hostKnowledgePackPath is this test's own knowledge of where each
// first-party host's single currently-committed Knowledge Pack lives —
// deliberately hardcoded rather than routed through internal/knowledge's
// version-range Repository lookup, since this test's job is to prove this
// package's resolver against the exact, real, committed Pack content, not
// to re-prove internal/knowledge's own version resolution (already proven
// by internal/knowledge's own tests).
var hostKnowledgePackPath = map[string]string{
	"claude-code": filepath.Join("knowledge", "hosts", "claude-code", "cli", "2.1", "manifest.json"),
	"codex":       filepath.Join("knowledge", "hosts", "codex", "cli", "0.144", "manifest.json"),
}

// buildObserveRequest bridges a populated qualify.Sandbox into a real
// internal/observe.Request, so this test exercises the actual production
// observation pipeline (JSON parsed into OpaqueVendorFields, TOML retained
// opaquely) rather than qualify.ObserveSandbox's older, file-level-only
// harness format — this package's mcp_server extraction needs the former to
// find individual server entries inside one registration file (extract.go's
// doc comment).
func buildObserveRequest(sb *qualify.Sandbox, host, version string) observe.Request {
	req := observe.Request{
		Detection: hostcontext.HostDetection{
			Host:    host,
			Surface: "cli",
			Version: version,
		},
		WorktreeRoot: sb.Project,
	}
	switch host {
	case "claude-code":
		// This sandbox's ClaudeConfigDir stands in for an EXPLICITLY SET
		// CLAUDE_CONFIG_DIR (a dedicated fixture subdirectory, not bare
		// $HOME) — real Claude Code relocates .claude.json right along
		// with the asset directory in that case, so both NativeHome
		// entries deliberately collapse to the identical Path here (see
		// internal/context/host.go's claudeNativeHomes doc comment).
		req.Detection.NativeHomes = []hostcontext.NativeHome{
			{Name: "CLAUDE_CONFIG_DIR", Path: sb.ClaudeConfigDir},
			{Name: "HOME/.claude.json", Path: sb.ClaudeConfigDir},
		}
	case "codex":
		req.Detection.NativeHomes = []hostcontext.NativeHome{
			{Name: "CODEX_HOME", Path: sb.CodexHome},
			{Name: "HOME/.agents/skills", Path: filepath.Join(sb.Home, ".agents", "skills")},
		}
	}
	return req
}

// relabelObservationPaths rewrites every Observation's Source.Path from the
// real internal/observe pipeline's absolute sandbox path (e.g.
// "/tmp/.../claude-config/.claude.json") into the same fixture-relative
// label qualify.ObserveSandbox's older harness format uses and
// expected-effective.json's SourceRef.Path is committed as (e.g.
// "claude-config/.claude.json") — internal/observe itself has no reason to
// know about this test-only labeling convention (it walks whatever absolute
// NativeHome path a real caller hands it), so this test derives it exactly
// the way qualify.ObserveSandbox does: strip the sandbox root, prefix with
// the fixture's root label.
func relabelObservationPaths(observations []domain.Observation, sb *qualify.Sandbox) []domain.Observation {
	roots := map[string]string{
		sb.Home:    "home",
		sb.Project: "project",
	}
	if sb.CodexHome != "" {
		roots[sb.CodexHome] = "codex-home"
	}
	if sb.ClaudeConfigDir != "" {
		roots[sb.ClaudeConfigDir] = "claude-config"
	}
	// Longest-prefix match first (sb.Home is a prefix of
	// sb.Home+"/.agents/skills" which is not itself a distinct root here,
	// but this keeps the relabeling robust to any future nested root).
	prefixes := make([]string, 0, len(roots))
	for p := range roots {
		prefixes = append(prefixes, p)
	}
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })

	out := make([]domain.Observation, len(observations))
	for i, o := range observations {
		for _, prefix := range prefixes {
			if strings.HasPrefix(o.Spec.Source.Path, prefix+string(filepath.Separator)) {
				rel := strings.TrimPrefix(o.Spec.Source.Path, prefix+string(filepath.Separator))
				o.Spec.Source.Path = filepath.ToSlash(filepath.Join(roots[prefix], rel))
				break
			}
		}
		o.Metadata.ID = fmt.Sprintf("%s:%s:%s", o.Spec.Host.ID, o.Spec.Concept, o.Spec.Source.Path)
		out[i] = o
	}
	return out
}

// entrySourceRefs returns every Candidate.Ref an EffectiveEntry's
// Provenance names, active or ignored — the full set of physical sources
// this package considered for that logical entity (or composition).
func entrySourceRefs(e EffectiveEntry) []string {
	refs := append([]string(nil), e.Provenance.ActiveSources...)
	refs = append(refs, e.Provenance.IgnoredSources...)
	sort.Strings(refs)
	return refs
}

func conflictSourceRefs(c Conflict) []string {
	refs := make([]string, 0, len(c.Candidates))
	for _, cand := range c.Candidates {
		refs = append(refs, cand.Ref)
	}
	sort.Strings(refs)
	return refs
}

func refsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func goldenSourceRefs(entry qualify.ExpectedEffectiveEntry) []string {
	refs := make([]string, 0, len(entry.Sources))
	for _, s := range entry.Sources {
		refs = append(refs, s.Path)
	}
	sort.Strings(refs)
	return refs
}

// TestFixtureCorpus_ReproducesExpectedEffective is the issue #21 acceptance
// criterion "Resolver output reproduces the fixture goldens for both
// hosts": for every committed fixtures/{claude-code,codex}/*/*/
// expected-effective.json entry, this package's ComputeEffectiveGraph,
// driven by the real internal/observe pipeline over the fixture's own
// input/ tree and the real, committed Knowledge Pack, must reach the same
// outcome the fixture records — a concrete winner when the golden names one
// (necessarily a trivial single-physical-source case in every one of
// today's fixtures, since every concept's resolve capability is honestly
// UNKNOWN in both committed Packs), and an unresolved Conflict whenever the
// golden's selectedSource is UNKNOWN.
func TestFixtureCorpus_ReproducesExpectedEffective(t *testing.T) {
	cases := []string{
		filepath.Join("fixtures", "claude-code", "2.1.211", "instructions-collision"),
		filepath.Join("fixtures", "claude-code", "2.1.211", "mcp-merge"),
		filepath.Join("fixtures", "claude-code", "2.1.211", "skill-collision"),
		filepath.Join("fixtures", "codex", "0.144.5", "instructions-collision"),
		filepath.Join("fixtures", "codex", "0.144.5", "mcp-merge"),
		filepath.Join("fixtures", "codex", "0.144.5", "skill-collision"),
	}

	root := repoRoot()
	for _, rel := range cases {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			dir := filepath.Join(root, rel)
			c, err := qualify.LoadCase(dir)
			if err != nil {
				t.Fatalf("LoadCase(%s): %v", dir, err)
			}

			sb, err := qualify.NewSandbox(t.TempDir(), c.Host)
			if err != nil {
				t.Fatalf("NewSandbox: %v", err)
			}
			if err := sb.PopulateFromInput(c.InputDir()); err != nil {
				t.Fatalf("PopulateFromInput: %v", err)
			}

			observations, err := observe.Observe(buildObserveRequest(sb, c.Host, c.Version))
			if err != nil {
				t.Fatalf("observe.Observe: %v", err)
			}
			observations = relabelObservationPaths(observations, sb)

			packPath := filepath.Join(root, hostKnowledgePackPath[c.Host])
			pack, err := knowledge.LoadPack(packPath)
			if err != nil {
				t.Fatalf("knowledge.LoadPack(%s): %v", packPath, err)
			}

			graph, err := ComputeEffectiveGraph(c.Host, c.Version, observations, pack.Knowledge, Options{}, nil)
			if err != nil {
				t.Fatalf("ComputeEffectiveGraph: %v", err)
			}

			for _, golden := range c.ExpectedEffective.Entries {
				wantRefs := goldenSourceRefs(golden)

				var matchedEntry *EffectiveEntry
				for i := range graph.Entries {
					if refsEqual(entrySourceRefs(graph.Entries[i]), wantRefs) {
						matchedEntry = &graph.Entries[i]
						break
					}
				}
				var matchedConflict *Conflict
				for i := range graph.Conflicts {
					if refsEqual(conflictSourceRefs(graph.Conflicts[i]), wantRefs) {
						matchedConflict = &graph.Conflicts[i]
						break
					}
				}

				switch {
				case golden.SelectedSource == qualify.Unknown:
					switch {
					case matchedConflict != nil:
						// Correctly unresolved as a Conflict.
					case matchedEntry != nil && matchedEntry.Provenance.SelectedSource == "":
						// Correctly unresolved: a composition (or
						// keep-both-style) entry with no single winner.
					case matchedEntry != nil:
						t.Errorf("golden %q (%s): expected UNKNOWN, but resolver selected %q", golden.LogicalID, rel, matchedEntry.Provenance.SelectedSource)
					default:
						t.Errorf("golden %q (%s): no matching resolver entry/conflict found for sources %v", golden.LogicalID, rel, wantRefs)
					}
				default:
					if matchedConflict != nil {
						t.Errorf("golden %q (%s): expected selectedSource %q, but resolver left it unresolved: %s", golden.LogicalID, rel, golden.SelectedSource, matchedConflict.Reason)
						continue
					}
					if matchedEntry == nil {
						t.Fatalf("golden %q (%s): no matching resolver entry found for sources %v", golden.LogicalID, rel, wantRefs)
					}
					if matchedEntry.Provenance.SelectedSource != golden.SelectedSource {
						t.Errorf("golden %q (%s): selectedSource = %q, want %q", golden.LogicalID, rel, matchedEntry.Provenance.SelectedSource, golden.SelectedSource)
					}
				}
			}
		})
	}
}

// TestFixtureCorpus_KnowledgePacksHaveConceptCoverage is a lighter sanity
// check backing the fixture reproduction test above: both real Knowledge
// Packs must declare a PrecedenceProgram for exactly the three concepts the
// committed fixture corpus exercises, so a future accidental removal of one
// (or a typo in the "<concept>." ID convention LookupProgram relies on)
// fails loudly here rather than only as a confusing fixture mismatch.
func TestFixtureCorpus_KnowledgePacksHaveConceptCoverage(t *testing.T) {
	root := repoRoot()
	for host, relPath := range hostKnowledgePackPath {
		pack, err := knowledge.LoadPack(filepath.Join(root, relPath))
		if err != nil {
			t.Fatalf("knowledge.LoadPack(%s): %v", relPath, err)
		}
		for _, concept := range []string{"instruction", "mcp_server", "skill"} {
			if _, ok := LookupProgram(pack.Knowledge, concept); !ok {
				t.Errorf("host %s: expected a precedence program for concept %q", host, concept)
			}
			capOps, ok := pack.Knowledge.Capabilities[concept]
			if !ok {
				t.Errorf("host %s: expected a capabilities entry for concept %q", host, concept)
				continue
			}
			if capOps.Resolve != domain.CapabilityUnknown {
				t.Errorf("host %s concept %q: resolve capability = %q, want UNKNOWN (this is exactly what makes the fixture goldens' UNKNOWN outcomes correct today)", host, concept, capOps.Resolve)
			}
		}
	}
}
