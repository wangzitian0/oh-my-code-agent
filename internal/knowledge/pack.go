package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// PackFileName is the file name LoadRepository looks for inside each
// knowledge/hosts/<host>/<surface>/<version-or-range>/ directory (see
// doc.go for why this is one combined document rather than the six
// docs/knowledge/README.md §3 split files).
const PackFileName = "manifest.json"

// floatingVersionTags are reserved words that name a moving target rather
// than one immutable, content-addressed version
// (docs/knowledge/README.md §7: "A floating `latest` selector may discover
// candidates but can never be recorded as the Knowledge dependency of a
// generation."). Matching is case-insensitive.
var floatingVersionTags = map[string]bool{
	"latest": true,
	"stable": true,
	"head":   true,
}

// ValidatePackReference rejects a would-be Knowledge Pack reference (a
// metadata.id or a dependency naming one) that is a floating tag rather than
// an immutable identity. It is exported so any future caller that records
// "the Knowledge dependency of a generation" (docs/architecture/runtime.md
// §5.3) can reuse this exact rule instead of re-deriving it.
func ValidatePackReference(field, value string) error {
	if floatingVersionTags[strings.ToLower(strings.TrimSpace(value))] {
		return fmt.Errorf("knowledge: %s %q is a floating reference and can never be recorded as a Knowledge Pack dependency (docs/knowledge/README.md §7)", field, value)
	}
	return nil
}

// Pack is one loaded, content-addressed Knowledge Pack: the validated
// domain.HostKnowledge document plus the digest that pins it
// (docs/knowledge/README.md §1, §4; internal/domain/digest.go's
// CanonicalDigest reused here rather than reinvented).
type Pack struct {
	Knowledge domain.HostKnowledge
	Digest    string
	Path      string
}

// LoadPack reads, validates, rejects-if-floating, and content-addresses one
// Knowledge Pack file at path. The file is JSON (see doc.go for why, not
// YAML as docs/knowledge/README.md §4's worked example shows).
func LoadPack(path string) (Pack, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Pack{}, fmt.Errorf("knowledge: LoadPack: %w", err)
	}
	var hk domain.HostKnowledge
	if err := json.Unmarshal(raw, &hk); err != nil {
		return Pack{}, fmt.Errorf("knowledge: LoadPack: %s: %w", path, err)
	}
	if err := domain.ValidateHostKnowledge(hk); err != nil {
		return Pack{}, fmt.Errorf("knowledge: LoadPack: %s: %w", path, err)
	}
	if err := ValidatePackReference("metadata.id", hk.Metadata.ID); err != nil {
		return Pack{}, fmt.Errorf("knowledge: LoadPack: %s: %w", path, err)
	}
	if err := ValidatePackReference("metadata.versionRange", hk.Metadata.VersionRange); err != nil {
		return Pack{}, fmt.Errorf("knowledge: LoadPack: %s: %w", path, err)
	}
	// A structural sanity check beyond ValidatePackReference's reserved-word
	// list: metadata.versionRange must actually parse as this package's
	// comparator syntax. This both catches other, unenumerated floating
	// words (e.g. a typo'd tag this package does not know to name) and
	// fails a Pack closed if its range is simply malformed, rather than
	// letting it load successfully and then silently never matching
	// anything in Resolve.
	if _, err := parseVersionRange(hk.Metadata.VersionRange); err != nil {
		return Pack{}, fmt.Errorf("knowledge: LoadPack: %s: metadata.versionRange: %w", path, err)
	}

	digest, err := domain.CanonicalDigest(hk)
	if err != nil {
		return Pack{}, fmt.Errorf("knowledge: LoadPack: %s: %w", path, err)
	}
	return Pack{Knowledge: hk, Digest: digest, Path: path}, nil
}
