package knowledge

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// semver is a minimal MAJOR.MINOR.PATCH version, sufficient for this
// package's real inputs (codex 0.144.5, claude-code 2.1.211 — see
// fixtures/README.md). It deliberately does not implement full
// semver.org precedence (prerelease/build-metadata ordering rules): a
// -prerelease or +build suffix is accepted on input but stripped before
// comparison, which is a known simplification rather than a claim of
// complete semver compliance.
type semver struct {
	major, minor, patch int
}

var semverPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// parseSemver parses s as MAJOR.MINOR.PATCH, ignoring any trailing
// -prerelease or build-metadata (+...) suffix.
func parseSemver(s string) (semver, error) {
	core := strings.TrimSpace(s)
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	m := semverPattern.FindStringSubmatch(core)
	if m == nil {
		return semver{}, fmt.Errorf("knowledge: parseSemver: %q is not a MAJOR.MINOR.PATCH version", s)
	}
	major, err1 := strconv.Atoi(m[1])
	minor, err2 := strconv.Atoi(m[2])
	patch, err3 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return semver{}, fmt.Errorf("knowledge: parseSemver: %q: invalid numeric component", s)
	}
	return semver{major: major, minor: minor, patch: patch}, nil
}

// compare returns -1, 0, or 1 as v is less than, equal to, or greater than
// o, comparing major, then minor, then patch.
func (v semver) compare(o semver) int {
	if v.major != o.major {
		return sign(v.major - o.major)
	}
	if v.minor != o.minor {
		return sign(v.minor - o.minor)
	}
	return sign(v.patch - o.patch)
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}

// versionRangeTerm is one "<op><version>" comparator, e.g. ">=0.144.0". op
// is "=" when the term carries no explicit operator (a bare version means
// exact match).
type versionRangeTerm struct {
	op      string
	version semver
}

var rangeTermPattern = regexp.MustCompile(`^(>=|<=|>|<|=)?(\d+\.\d+\.\d+)$`)

// parseVersionRange splits rangeExpr on whitespace into one or more
// comparator terms (docs/knowledge/README.md §4's versionRange syntax, e.g.
// ">=0.144.0 <0.145.0"). Every term must hold for a version to satisfy the
// range (AND semantics) — there is no OR, caret, or tilde range syntax in
// scope, matching the two real Knowledge Packs this package ships.
func parseVersionRange(rangeExpr string) ([]versionRangeTerm, error) {
	fields := strings.Fields(rangeExpr)
	if len(fields) == 0 {
		return nil, fmt.Errorf("knowledge: parseVersionRange: empty range expression")
	}
	terms := make([]versionRangeTerm, 0, len(fields))
	for _, f := range fields {
		m := rangeTermPattern.FindStringSubmatch(f)
		if m == nil {
			return nil, fmt.Errorf("knowledge: parseVersionRange: %q: unsupported comparator term %q", rangeExpr, f)
		}
		op := m[1]
		if op == "" {
			op = "="
		}
		v, err := parseSemver(m[2])
		if err != nil {
			return nil, err
		}
		terms = append(terms, versionRangeTerm{op: op, version: v})
	}
	return terms, nil
}

// VersionSatisfiesRange reports whether exactVersion satisfies every
// comparator term in rangeExpr.
func VersionSatisfiesRange(exactVersion, rangeExpr string) (bool, error) {
	v, err := parseSemver(exactVersion)
	if err != nil {
		return false, err
	}
	terms, err := parseVersionRange(rangeExpr)
	if err != nil {
		return false, err
	}
	for _, t := range terms {
		cmp := v.compare(t.version)
		var ok bool
		switch t.op {
		case ">=":
			ok = cmp >= 0
		case "<=":
			ok = cmp <= 0
		case ">":
			ok = cmp > 0
		case "<":
			ok = cmp < 0
		case "=":
			ok = cmp == 0
		default:
			return false, fmt.Errorf("knowledge: VersionSatisfiesRange: unreachable comparator %q", t.op)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}
