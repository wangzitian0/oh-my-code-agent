package effective

import (
	"regexp"
	"sort"
	"strings"
)

// ToolSourceKind names which physical distribution mechanism exposes a
// tool, per docs/ontology/README.md §8's safety invariant: "Same-brand
// connector, MCP server, plugin tool, and built-in tool are separate
// sources. Duplicate logical capabilities must be shown before launch."
type ToolSourceKind string

const (
	ToolSourceBuiltin ToolSourceKind = "builtin"
	ToolSourceMCP     ToolSourceKind = "mcp"
	ToolSourcePlugin  ToolSourceKind = "plugin"
)

// ToolSource is one tool as exposed by one physical source: a host's native
// built-in tool, one MCP server's advertised tool, or one plugin-bundled
// tool.
type ToolSource struct {
	Kind ToolSourceKind
	// Owner identifies the exposing source: the host ID for a builtin tool,
	// the mcp_server LogicalGroup's LogicalID for an MCP-exposed tool, or
	// the plugin ID for a plugin tool.
	Owner string
	// Ref is the originating Candidate.Ref (or an equivalent stable
	// reference for a caller-supplied builtin/plugin tool), for provenance.
	Ref string
	// Tool is the tool name exactly as this source exposes it.
	Tool string
}

// fingerprintCleaner strips punctuation/separators a tool name commonly
// varies by across transports (snake_case vs kebab-case vs a namespaced
// "mcp__server__tool" form) so genuinely-the-same capability fingerprints
// identically regardless of surface naming convention.
var fingerprintCleaner = regexp.MustCompile(`[^a-z0-9]+`)

// Fingerprint normalizes a tool name into the identity duplicate detection
// compares on: lowercased, with every run of non-alphanumeric characters
// collapsed to nothing (so "web_search", "web-search", and "WebSearch" all
// fingerprint identically, while genuinely different names like
// "web_search" and "websearch_v2" still do not collide).
func Fingerprint(tool string) string {
	return fingerprintCleaner.ReplaceAllString(strings.ToLower(tool), "")
}

// DuplicateCapability is one logical tool capability found exposed by more
// than one distinct ToolSourceKind — a built-in tool also reachable through
// an MCP server, for example. This must be surfaced before launch
// (docs/ontology/README.md §8), never silently deduplicated or silently
// left as two independently-invokable tools.
type DuplicateCapability struct {
	Fingerprint string
	Sources     []ToolSource
}

// DetectDuplicateCapabilities computes each tool's Fingerprint across every
// supplied ToolSource and reports every fingerprint reachable through more
// than one distinct ToolSourceKind (built-in, MCP, or plugin) — the "same
// logical tool exposed through two transports" case issue #21's round-2
// audit names explicitly. Two sources of the SAME kind sharing a fingerprint
// (e.g. two different MCP servers both naming a tool "search") are not
// flagged here: that is an ordinary same-transport name collision the
// concept's own merge operator (docs/ontology/README.md §3.1's NAMESPACE/
// UNION_BY_ID) already governs, not a cross-transport duplicate capability.
func DetectDuplicateCapabilities(sources []ToolSource) []DuplicateCapability {
	byFingerprint := map[string][]ToolSource{}
	var order []string
	for _, s := range sources {
		fp := Fingerprint(s.Tool)
		if fp == "" {
			continue
		}
		if _, ok := byFingerprint[fp]; !ok {
			order = append(order, fp)
		}
		byFingerprint[fp] = append(byFingerprint[fp], s)
	}
	sort.Strings(order)

	var out []DuplicateCapability
	for _, fp := range order {
		group := byFingerprint[fp]
		kinds := map[ToolSourceKind]bool{}
		for _, s := range group {
			kinds[s.Kind] = true
		}
		if len(kinds) < 2 {
			continue
		}
		sorted := append([]ToolSource(nil), group...)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Kind != sorted[j].Kind {
				return sorted[i].Kind < sorted[j].Kind
			}
			if sorted[i].Owner != sorted[j].Owner {
				return sorted[i].Owner < sorted[j].Owner
			}
			return sorted[i].Ref < sorted[j].Ref
		})
		out = append(out, DuplicateCapability{Fingerprint: fp, Sources: sorted})
	}
	return out
}

// MCPToolSources projects every mcp_server Candidate's advertised Tools
// (ontology/concepts/mcp_server.json's "tools" field) into ToolSource
// values, for combining with caller-supplied builtin/plugin ToolSources
// (this package has no builtin-tool or plugin-tool observation source of
// its own yet — a future PR that adds one feeds directly into
// DetectDuplicateCapabilities alongside this).
func MCPToolSources(candidates []Candidate) []ToolSource {
	var out []ToolSource
	for _, c := range candidates {
		if c.Concept != "mcp_server" {
			continue
		}
		for _, tool := range c.Tools {
			out = append(out, ToolSource{Kind: ToolSourceMCP, Owner: c.LogicalID, Ref: c.Ref, Tool: tool})
		}
	}
	return out
}
