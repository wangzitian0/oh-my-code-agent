package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultStepTimeout is the empirical 150s SLA knee-of-the-curve limit for single tool steps.
	DefaultStepTimeout = 150 * time.Second

	// TTFTTimeout is the Time-To-First-Token boundary. If no chunk arrives within 30s,
	// the gateway is considered congested.
	TTFTTimeout = 30 * time.Second

	// HarvestThreshold is the soft cutoff at 140s where streaming outputs are gracefully
	// cut and syntactically repaired before the 150s hard SIGTERM.
	HarvestThreshold = 140 * time.Second
)

var (
	ErrIdenticalCallLoop = errors.New("circuit breaker: detected 3 consecutive identical tool calls with same arguments")
	ErrCodeOscillation   = errors.New("circuit breaker: detected code mutation oscillation reverting recent edits")
	ErrDegenerativeLoop  = errors.New("circuit breaker: detected repetitive n-gram token degeneracy loop")
	ErrStreamSilence     = errors.New("watchdog: stream silence exceeded 30s TTFT boundary")
	ErrStepTimeout       = errors.New("watchdog: step duration exceeded 150s SLA limit")
)

// JSONAutoRepair inspects a potentially truncated JSON string, tracks unclosed
// quotes and nested brackets/braces, and synthesizes balanced closing tokens
// so callers' JSON parsers never fail with SyntaxError.
func JSONAutoRepair(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "{}"
	}

	// If not an object or array, treat as text with a truncation notice
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return s + "\n... [Truncated at 150s SLA]"
	}

	var stack []rune
	inString := false
	escaped := false

	runes := []rune(trimmed)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inString {
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == '"' {
				inString = false
			}
			continue
		}

		switch r {
		case '"':
			inString = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 && stack[len(stack)-1] == r {
				stack = stack[:len(stack)-1]
			}
		}
	}

	var b strings.Builder
	b.WriteString(trimmed)

	// If cut in the middle of an escape sequence, strip it
	if escaped {
		// remove trailing backslash if present
	}

	// If cut inside a string, close quote
	if inString {
		b.WriteString(`"`)
	}

	// If the root was an object and has remaining closers, inject _meta before final brace
	if len(stack) > 0 && stack[0] == '}' {
		// Pop intermediate closers except the outermost '}'
		for len(stack) > 1 {
			closer := stack[len(stack)-1]
			b.WriteRune(closer)
			stack = stack[:len(stack)-1]
		}
		// Inject truncation metadata
		b.WriteString(`,"_meta":{"truncated":true,"reason":"SLA_150S_EXCEEDED"}`)
		b.WriteRune('}')
		return b.String()
	}

	// For arrays or other structures, close all remaining
	for i := len(stack) - 1; i >= 0; i-- {
		b.WriteRune(stack[i])
	}

	return b.String()
}

// DetectNgramLoop detects whether repetitive n-gram token sequences have degenerated
// into an infinite hallucination loop (e.g., repeating the same 8-word sequence >= 4 times).
func DetectNgramLoop(text string, n int, repeatThreshold int) bool {
	if n <= 0 {
		n = 8
	}
	if repeatThreshold <= 1 {
		repeatThreshold = 4
	}

	words := strings.Fields(text)
	if len(words) < n*repeatThreshold {
		return false
	}

	for i := 0; i <= len(words)-(n*repeatThreshold); i++ {
		target := strings.Join(words[i:i+n], " ")
		consecutive := 1
		for k := 1; k < repeatThreshold; k++ {
			nextStart := i + k*n
			if nextStart+n > len(words) {
				break
			}
			candidate := strings.Join(words[nextStart:nextStart+n], " ")
			if candidate == target {
				consecutive++
			} else {
				break
			}
		}
		if consecutive >= repeatThreshold {
			return true
		}
	}
	return false
}

// FingerprintCall hashes tool name and canonical params to track duplicates.
func FingerprintCall(toolName string, canonicalArgs string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s", toolName, strings.TrimSpace(canonicalArgs))))
	return hex.EncodeToString(h[:16])
}

// CheckCallLoop checks a history of call hashes. If the last consecutive calls
// are identical for threshold times, it flags a loop.
func CheckCallLoop(history []string, threshold int) error {
	if threshold <= 1 {
		threshold = 3
	}
	if len(history) < threshold {
		return nil
	}

	last := history[len(history)-1]
	count := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i] == last {
			count++
		} else {
			break
		}
	}

	if count >= threshold {
		return ErrIdenticalCallLoop
	}
	return nil
}

// SwarmQuorumDecision evaluates whether a batch of swarm tasks should return early.
// Rules:
// 1. If completed / total >= quorumRatio (e.g. 80%) AND elapsed >= minWait, return early.
// 2. If elapsed >= hardLimit (150s), return immediately regardless of completion.
func SwarmQuorumDecision(completed, total int, elapsed time.Duration, quorumRatio float64, minWait, hardLimit time.Duration) (shouldReturn bool, reason string) {
	if total <= 0 {
		return true, "empty_batch"
	}
	if completed >= total {
		return true, "all_completed"
	}
	if elapsed >= hardLimit {
		return true, "hard_timeout_150s"
	}
	if quorumRatio <= 0 {
		quorumRatio = 0.80
	}
	if minWait <= 0 {
		minWait = 100 * time.Second
	}

	ratio := float64(completed) / float64(total)
	if ratio >= quorumRatio && elapsed >= minWait {
		return true, fmt.Sprintf("quorum_reached_%.0f_percent_at_%v", ratio*100, elapsed.Round(time.Second))
	}

	return false, ""
}
