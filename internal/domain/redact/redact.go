// Package redact implements the redaction rules every OMCA output path must
// apply before a document leaves the process: reports, manifests, diffs,
// logs, and anything that could enter model context (init.md invariant
// "secrets do not enter reports, plans, manifests, or model context";
// docs/product/requirements.md FR-2, "Secret values must be redacted before
// persistence"; docs/knowledge/README.md §10, qualification suite item 10,
// "secret redaction and proof that observation did not execute content").
//
// Redaction never deletes a field: a likely-secret value is replaced with a
// stable "REDACTED:sha256:<hash-of-original>" marker, so two redacted
// documents can still be compared for whether a secret changed, and a human
// can still see that a value existed, without the original value ever
// appearing in output.
package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// sensitiveKeyPattern matches a map/struct field name that signals a secret
// regardless of the value's shape: docs/product/requirements.md FR-2 and
// the PR-04 issue name "token/secret/password/apiKey/authorization" as the
// literal field-name signal.
var sensitiveKeyPattern = regexp.MustCompile(`(?i)(token|secret|passwd|password|api[_-]?key|authorization|credential)`)

// secretShapePattern matches secret-shaped substrings anywhere inside a
// string value, independent of the key it is stored under, so a leak is
// still caught when it appears under an innocuous key name (e.g. a "notes"
// or "debug" field that happens to quote a header or env assignment).
var secretShapePattern = regexp.MustCompile(`(?i)` +
	`(bearer\s+[a-z0-9\-_.=]+` + // Authorization: Bearer <token>
	`|sk-[a-z0-9]{8,}` + // OpenAI/Anthropic-style API keys
	`|gh[pousr]_[a-z0-9]{20,}` + // GitHub personal/OAuth/user/server tokens
	`|xox[baprs]-[a-z0-9-]+` + // Slack tokens
	`|akia[0-9a-z]{16}` + // AWS access key IDs
	`|\b[a-z][a-z0-9_]*(?:key|token|secret|password)\b\s*=\s*\S+)`) // ENV_STYLE_NAME=value

// marker returns the stable, traceable-but-opaque replacement for s.
func marker(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "REDACTED:sha256:" + hex.EncodeToString(sum[:])
}

// Value walks v (any JSON-marshalable value) and returns an equivalent
// generic tree (map[string]any / []any / string / float64 / bool / nil)
// with every likely-secret value replaced by marker(originalValue). It
// round-trips through JSON first (the same technique domain.CanonicalDigest
// uses) so a typed struct and an equivalent freeform map redact identically.
func Value(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("redact: marshal: %w", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("redact: normalize: %w", err)
	}
	return walk(generic), nil
}

func walk(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, sub := range val {
			if sensitiveKeyPattern.MatchString(k) {
				out[k] = redactWhole(sub)
				continue
			}
			out[k] = walk(sub)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, sub := range val {
			out[i] = walk(sub)
		}
		return out
	case string:
		return redactShapes(val)
	default:
		return v
	}
}

// redactWhole replaces an entire value with one marker, used when the
// containing key name itself signals sensitivity regardless of the value's
// type or shape (a short PIN-like password, or a nested credentials object).
func redactWhole(v any) any {
	switch val := v.(type) {
	case string:
		return marker(val)
	case nil:
		return v
	default:
		raw, err := json.Marshal(val)
		if err != nil {
			return marker(fmt.Sprintf("%v", val))
		}
		return marker(string(raw))
	}
}

// redactShapes replaces every secret-shaped substring inside s with its own
// marker, leaving the surrounding non-secret text intact.
func redactShapes(s string) string {
	return secretShapePattern.ReplaceAllStringFunc(s, marker)
}

// JSON redacts v and returns its sanitized JSON encoding. This is the output
// path for both a typed struct and a generic map[string]any: both round-trip
// through Value first, so both redact identically.
func JSON(v any) ([]byte, error) {
	sanitized, err := Value(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(sanitized)
}

// Report redacts v and renders it as an indented "key: value" text block.
// It redacts internally rather than accepting an already-sanitized value,
// so a text-report output path cannot forget the step: whatever v contains,
// the returned text has already been through Value.
func Report(v any) (string, error) {
	sanitized, err := Value(v)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	renderText(&b, "", sanitized, 0)
	return b.String(), nil
}

func renderText(b *strings.Builder, key string, v any, depth int) {
	indent := strings.Repeat("  ", depth)
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if key != "" {
			fmt.Fprintf(b, "%s%s:\n", indent, key)
			depth++
		}
		for _, k := range keys {
			renderText(b, k, val[k], depth)
		}
	case []any:
		if key != "" {
			fmt.Fprintf(b, "%s%s:\n", indent, key)
		}
		for _, item := range val {
			renderText(b, "-", item, depth+1)
		}
	default:
		fmt.Fprintf(b, "%s%s: %v\n", indent, key, val)
	}
}
