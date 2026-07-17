package drift

import (
	"fmt"
	"reflect"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Classify turns one Signal into an Assertion, given the Exception set that
// may authorize it and a reference instant now. now is an explicit
// parameter — never time.Now() internally — for the same reason
// internal/resolve.Resolve takes one: an Exception's expiry is meaningless
// without a reference clock, and calling time.Now() internally would make
// identical inputs produce different output across calls, breaking the
// determinism this package guarantees (see determinism_test.go).
//
// ok is false, with a nil error, when sig does not represent an observable
// difference worth reporting: for the three diff-based categories
// (CONFIG_DRIFT, EFFECTIVE_DRIFT, SOURCE_DRIFT), that means
// reflect.DeepEqual(sig.Expected, sig.Observed). Gap-style categories
// (CAPABILITY_GAP, KNOWLEDGE_DRIFT, CONTEXT_DRIFT) and explicit UNKNOWN
// signals are never skipped this way: the signal existing at all is the
// drift being reported, independent of whatever descriptive Expected/
// Observed values the caller attached.
//
// Classify returns a non-nil error only for a structurally invalid Signal
// (missing EntityID/Field/RootCause, an unrecognized Category, or a Category
// of domain.DriftException — which Classify computes itself and must never
// receive as input).
//
// # EXCEPTION classification and expiry
//
// After the base category is determined, Classify checks whether a valid,
// unexpired domain.Exception applies: one whose AssetID matches sig.AssetID
// (or sig.EntityID, if AssetID is empty) and whose Scope is one of
// sig.ExceptionScopes (or []string{sig.RootCause}, if that is empty), with
// now strictly before its ExpiresAt (docs/architecture/reporting.md §6,
// "EXCEPTION: authorized, documented, and unexpired difference"). If found,
// the returned Assertion's Category is domain.DriftException and
// ExceptionRef names the exception's metadata.id, while UnderlyingCategory
// keeps the pre-exception classification. If no such exception is found —
// whether none was ever authorized, or one exists but now is at or after
// its ExpiresAt — Category is simply the underlying base category:  an
// expired exception is not a distinct state Classify tracks, it is just the
// absence of a currently-valid one, so classification "reverts" to the
// underlying drift class by falling through to the same path a signal with
// no exception at all takes.
func Classify(sig Signal, exceptions []domain.Exception, now time.Time) (Assertion, bool, error) {
	if sig.EntityID == "" {
		return Assertion{}, false, fmt.Errorf("drift: signal entityId is required")
	}
	if sig.Field == "" {
		return Assertion{}, false, fmt.Errorf("drift: signal field is required")
	}
	if sig.RootCause == "" {
		return Assertion{}, false, fmt.Errorf("drift: signal rootCause is required")
	}

	underlying := sig.Category
	if underlying == "" {
		underlying = domain.DriftUnknown
	}
	if underlying == domain.DriftException {
		return Assertion{}, false, fmt.Errorf("drift: signal %s/%s: category must be a base drift category, not EXCEPTION (Classify computes EXCEPTION itself)", sig.EntityID, sig.Field)
	}
	if !underlying.Valid() {
		return Assertion{}, false, fmt.Errorf("drift: signal %s/%s: %q is not a valid drift category", sig.EntityID, sig.Field, sig.Category)
	}

	if isDiffCategory(underlying) && reflect.DeepEqual(sig.Expected, sig.Observed) {
		return Assertion{}, false, nil
	}

	category := underlying
	var exceptionRef string
	if underlying != domain.DriftUnknown {
		assetID := sig.AssetID
		if assetID == "" {
			assetID = sig.EntityID
		}
		scopes := sig.ExceptionScopes
		if len(scopes) == 0 {
			scopes = []string{sig.RootCause}
		}
		if ex, ok := findException(exceptions, assetID, scopes, now); ok {
			category = domain.DriftException
			exceptionRef = ex.Metadata.ID
		}
	}

	a := Assertion{
		DriftAssertion: domain.DriftAssertion{
			EntityID:      sig.EntityID,
			Field:         sig.Field,
			Category:      category,
			Expected:      sig.Expected,
			Observed:      sig.Observed,
			RootCause:     sig.RootCause,
			Remediation:   sig.Remediation,
			ContextCell:   contextCell(sig),
			EvidenceLevel: sig.EvidenceLevel,
			Guarantee:     sig.Guarantee,
		},
		UnderlyingCategory: underlying,
		Project:            sig.Project,
		Host:               sig.Host,
		HostVersion:        sig.HostVersion,
		AdapterVersion:     sig.AdapterVersion,
		ExceptionRef:       exceptionRef,
	}
	return a, true, nil
}

// ClassifyAll classifies every signal, in order, dropping any signal
// Classify reports as not-a-difference (ok == false) and failing closed on
// the first structurally invalid signal, with its index named in the error
// so a bad fixture or a bad PR-17 adapter translation is easy to locate.
func ClassifyAll(signals []Signal, exceptions []domain.Exception, now time.Time) ([]Assertion, error) {
	out := make([]Assertion, 0, len(signals))
	for i, sig := range signals {
		a, ok, err := Classify(sig, exceptions, now)
		if err != nil {
			return nil, fmt.Errorf("drift: signal[%d]: %w", i, err)
		}
		if !ok {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// isDiffCategory reports whether c is one of the three categories whose
// drift is defined as an expected/observed difference (as opposed to
// CAPABILITY_GAP/KNOWLEDGE_DRIFT/CONTEXT_DRIFT, whose drift is the signal's
// existence, not necessarily a value mismatch).
func isDiffCategory(c domain.DriftCategory) bool {
	switch c {
	case domain.DriftConfigDrift, domain.DriftEffectiveDrift, domain.DriftSourceDrift:
		return true
	default:
		return false
	}
}

// contextCell formats sig's project/host dimensions into the single
// human-readable "host/version/context cell" string
// docs/architecture/reporting.md §6's assertion form calls for (e.g.
// "infra2 / codex" in §7's worked example).
func contextCell(sig Signal) string {
	switch {
	case sig.Project != "" && sig.Host != "":
		return sig.Project + " / " + sig.Host
	case sig.Project != "":
		return sig.Project
	default:
		return sig.Host
	}
}

// findException returns the first Exception in exceptions that is
// structurally valid (domain.Exception.Valid()), names assetID, has a Scope
// in scopes, and is not expired as of now (now.Before(ExpiresAt), matching
// internal/resolve's findException strictness: an Exception whose ExpiresAt
// is exactly now or earlier no longer applies).
func findException(exceptions []domain.Exception, assetID string, scopes []string, now time.Time) (domain.Exception, bool) {
	if assetID == "" || len(scopes) == 0 {
		return domain.Exception{}, false
	}
	scopeSet := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		if s != "" {
			scopeSet[s] = true
		}
	}
	for _, ex := range exceptions {
		if !ex.Valid() {
			continue
		}
		if ex.AssetID != assetID {
			continue
		}
		if !scopeSet[ex.Scope] {
			continue
		}
		if !now.Before(ex.ExpiresAt) {
			continue
		}
		return ex, true
	}
	return domain.Exception{}, false
}
