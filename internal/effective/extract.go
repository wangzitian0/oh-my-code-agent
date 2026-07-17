package effective

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// ExtractCandidates turns every Observation into one or more Candidates
// (extraction is 1:1 for most concepts; mcp_server can be 1:many when one
// registration file's Observation carries several server definitions —
// internal/observe's doc.go safety property 3: a JSON-shaped MCP file is
// decoded losslessly into OpaqueVendorFields rather than split at
// observation time). Observations for a concept this package does not know
// how to extract sub-entities from (anything other than instruction, skill,
// mcp_server) are skipped, not errored: this package's scope is the three
// concepts the committed Knowledge Packs and qualification fixtures cover
// (docs/ontology/README.md §1.1's instruction/skill/mcp_server), matching
// ontology/concepts/*.json's currently-loaded registry.
func ExtractCandidates(observations []domain.Observation) ([]Candidate, error) {
	var out []Candidate
	for _, obs := range observations {
		switch obs.Spec.Concept {
		case "instruction":
			out = append(out, extractInstructionCandidate(obs))
		case "skill":
			out = append(out, extractSkillCandidate(obs))
		case "mcp_server":
			cands, err := extractMCPServerCandidates(obs)
			if err != nil {
				return nil, fmt.Errorf("effective: ExtractCandidates: %w", err)
			}
			out = append(out, cands...)
		default:
			// Not one of this package's three scoped concepts (hook,
			// policy, plugin, or anything future) — deliberately ignored
			// here rather than erroring; a caller building duplicate
			// capability sources from plugin/builtin tool inventories does
			// so separately (duplicate.go), not through this function.
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Concept != out[j].Concept {
			return out[i].Concept < out[j].Concept
		}
		if out[i].LogicalID != out[j].LogicalID {
			return out[i].LogicalID < out[j].LogicalID
		}
		return out[i].Ref < out[j].Ref
	})
	return out, nil
}

// contentDigest prefers the Observation's own ParsedDigest (the digest of
// whatever OpaqueVendorFields retained), falling back to RawDigest and then
// the raw source digest, so a Candidate always has a stable, reproducible
// digest even for an E0 (discovered-only) record with no readable content.
func contentDigest(obs domain.Observation) string {
	switch {
	case obs.Spec.ParsedDigest != "":
		return obs.Spec.ParsedDigest
	case obs.Spec.RawDigest != "":
		return obs.Spec.RawDigest
	default:
		return obs.Spec.Source.Digest
	}
}

func extractInstructionCandidate(obs domain.Observation) Candidate {
	return Candidate{
		Concept:       "instruction",
		LogicalID:     obs.Spec.Scope.Root + "|" + obs.Spec.Source.Path,
		Ref:           obs.Spec.Source.Path,
		Scope:         obs.Spec.Scope,
		Source:        obs.Spec.Source,
		Disposition:   obs.Spec.Disposition,
		EvidenceLevel: obs.Spec.EvidenceLevel,
		ContentDigest: contentDigest(obs),
	}
}

// skillNameFromPath derives a skill's discoverable name from its SKILL.md
// path: the immediate parent directory (docs/ontology/README.md §6.1/§6.2:
// "<id>/SKILL.md"). A path not ending in SKILL.md falls back to its own base
// name, so a source this package cannot confidently name still gets a
// deterministic, non-empty LogicalID rather than colliding on "".
func skillNameFromPath(path string) string {
	base := filepath.Base(path)
	if base == "SKILL.md" {
		return filepath.Base(filepath.Dir(path))
	}
	return base
}

func extractSkillCandidate(obs domain.Observation) Candidate {
	name := skillNameFromPath(obs.Spec.Source.Path)
	sourceKind := obs.Spec.Source.Kind
	return Candidate{
		Concept:       "skill",
		LogicalID:     name + "|" + sourceKind,
		Ref:           obs.Spec.Source.Path,
		Scope:         obs.Spec.Scope,
		Source:        obs.Spec.Source,
		Disposition:   obs.Spec.Disposition,
		EvidenceLevel: obs.Spec.EvidenceLevel,
		ContentDigest: contentDigest(obs),
	}
}

// extractMCPServerCandidates pulls individual server definitions out of one
// mcp_server Observation. Claude Code's sources are JSON, decoded losslessly
// by internal/observe into OpaqueVendorFields["content"] as a generic
// map[string]any: this function reads the "mcpServers" table directly.
// Codex's config.toml is deliberately never structurally parsed by
// internal/observe (walk.go's parseContent doc comment: "this project has no
// TOML dependency"), so OpaqueVendorFields["content"] is the raw TOML text;
// this function reuses that same no-new-dependency stance and scrapes only
// "[mcp_servers.<id>]" table headers and their immediate "key = value" lines
// with a narrow regexp — not a general TOML parser — sufficient for identity
// and content-equality, never claimed as a lossless structural decode.
//
// An Observation with no recognizable server table (including one with no
// OpaqueVendorFields at all, e.g. an E0 discovered-only record, or a
// qualify.ObserveSandbox-style Observation from the older, file-level-only
// harness fixture format) still produces exactly one Candidate for the whole
// file, keyed by its path: extraction never silently drops a source it
// cannot subdivide.
func extractMCPServerCandidates(obs domain.Observation) ([]Candidate, error) {
	whole := Candidate{
		Concept:       "mcp_server",
		LogicalID:     obs.Spec.Source.Path,
		Ref:           obs.Spec.Source.Path,
		Scope:         obs.Spec.Scope,
		Source:        obs.Spec.Source,
		Disposition:   obs.Spec.Disposition,
		EvidenceLevel: obs.Spec.EvidenceLevel,
		ContentDigest: contentDigest(obs),
	}

	content, ok := obs.Spec.OpaqueVendorFields["content"]
	if !ok {
		return []Candidate{whole}, nil
	}

	switch v := content.(type) {
	case map[string]any:
		table, _ := firstNonNilTable(v, "mcpServers", "mcp_servers")
		if table == nil {
			return []Candidate{whole}, nil
		}
		return mcpCandidatesFromJSONTable(obs, table)
	case string:
		table := scrapeTOMLMCPServers(v)
		if len(table) == 0 {
			return []Candidate{whole}, nil
		}
		return mcpCandidatesFromScrapedTable(obs, table)
	default:
		return []Candidate{whole}, nil
	}
}

func firstNonNilTable(m map[string]any, keys ...string) (map[string]any, bool) {
	for _, k := range keys {
		if raw, ok := m[k]; ok {
			if table, ok := raw.(map[string]any); ok {
				return table, true
			}
		}
	}
	return nil, false
}

func mcpTransport(def map[string]any) string {
	if _, ok := def["url"]; ok {
		if t, ok := def["type"].(string); ok && (t == "sse" || t == "http") {
			return t
		}
		return "http"
	}
	return "stdio"
}

func mcpCandidatesFromJSONTable(obs domain.Observation, table map[string]any) ([]Candidate, error) {
	ids := make([]string, 0, len(table))
	for id := range table {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]Candidate, 0, len(ids))
	for _, id := range ids {
		def, _ := table[id].(map[string]any)
		digest, err := domain.CanonicalDigest(def)
		if err != nil {
			return nil, fmt.Errorf("digest mcp server %q at %s: %w", id, obs.Spec.Source.Path, err)
		}
		out = append(out, Candidate{
			Concept:       "mcp_server",
			LogicalID:     mcpTransport(def) + "|" + id,
			Ref:           fmt.Sprintf("%s#mcpServers.%s", obs.Spec.Source.Path, id),
			Scope:         obs.Spec.Scope,
			Source:        obs.Spec.Source,
			Disposition:   obs.Spec.Disposition,
			EvidenceLevel: obs.Spec.EvidenceLevel,
			Fields:        def,
			ContentDigest: digest,
			Tools:         stringSliceField(def, "tools"),
		})
	}
	return out, nil
}

// tomlTableHeader matches a "[mcp_servers.<id>]" table header line. Dotted
// or quoted server IDs are out of scope — real fixture content (and Codex's
// documented config shape) uses plain bareword IDs.
var tomlTableHeader = regexp.MustCompile(`^\[mcp_servers\.([A-Za-z0-9_-]+)\]\s*$`)

// tomlKeyValue matches a simple "key = value" assignment line within a
// table, capturing the raw (still-quoted/bracketed) value text verbatim —
// this is a scrape for identity/equality purposes, not a TOML value decoder.
var tomlKeyValue = regexp.MustCompile(`^([A-Za-z0-9_]+)\s*=\s*(.+?)\s*$`)

// scrapeTOMLMCPServers returns, for each "[mcp_servers.<id>]" table found in
// raw, an ordered list of its immediate "key = value" line pairs (as raw
// text). It stops one table at the next "[" header line (any table, not
// just another mcp_servers one) or end of input.
func scrapeTOMLMCPServers(raw string) map[string][][2]string {
	tables := map[string][][2]string{}
	var currentID string
	inMCPTable := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if m := tomlTableHeader.FindStringSubmatch(trimmed); m != nil {
				currentID = m[1]
				inMCPTable = true
				if _, ok := tables[currentID]; !ok {
					tables[currentID] = nil
				}
				continue
			}
			inMCPTable = false
			continue
		}
		if !inMCPTable || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := tomlKeyValue.FindStringSubmatch(trimmed); m != nil {
			tables[currentID] = append(tables[currentID], [2]string{m[1], m[2]})
		}
	}
	return tables
}

func mcpCandidatesFromScrapedTable(obs domain.Observation, tables map[string][][2]string) ([]Candidate, error) {
	ids := make([]string, 0, len(tables))
	for id := range tables {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]Candidate, 0, len(ids))
	for _, id := range ids {
		pairs := tables[id]
		fields := make(map[string]any, len(pairs))
		transport := "stdio"
		for _, kv := range pairs {
			fields[kv[0]] = kv[1]
			if kv[0] == "url" {
				transport = "http"
			}
		}
		digest, err := domain.CanonicalDigest(fields)
		if err != nil {
			return nil, fmt.Errorf("digest scraped mcp server %q at %s: %w", id, obs.Spec.Source.Path, err)
		}
		out = append(out, Candidate{
			Concept:       "mcp_server",
			LogicalID:     transport + "|" + id,
			Ref:           fmt.Sprintf("%s#mcp_servers.%s", obs.Spec.Source.Path, id),
			Scope:         obs.Spec.Scope,
			Source:        obs.Spec.Source,
			Disposition:   obs.Spec.Disposition,
			EvidenceLevel: obs.Spec.EvidenceLevel,
			Fields:        fields,
			ContentDigest: digest,
		})
	}
	return out, nil
}

func stringSliceField(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
