package profiles

import (
	"fmt"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// ExceptionLoadResult separates the Exception documents LoadExceptions found
// into those still live at referenceTime and those that are structurally
// valid but expired, so a caller can hand only Live to resolve.Resolve
// while still surfacing Expired for a report/doctor-style consumer (issue
// #16 round-2 AC: "An expired exception is inert and reported as expired
// (test)" — the "reported" half needs the expired ones to still be visible
// somewhere, not silently dropped).
type ExceptionLoadResult struct {
	Live    []domain.Exception
	Expired []domain.Exception
}

// LoadExceptions reads and validates every Exception YAML document under
// each of dirs (docs/architecture/README.md §7:
// ~/.config/omca/exceptions/ and <repository>/.omca/exceptions/), then
// splits them into Live and Expired relative to referenceTime.
//
// The liveness test mirrors internal/resolve.findException's own expiry
// check exactly (now.Before(ExpiresAt) must hold strictly): an Exception
// whose ExpiresAt is at or before referenceTime is expired and must not be
// handed to resolve.Resolve as a live exception, or it would incorrectly
// except a DENIED/REQUIRED asset that should be enforced again.
//
// Loading fails closed on the first structurally invalid document (missing
// scope/justification/expiresAt, bad apiVersion, etc. — domain.
// ValidateException), the same actionable-error discipline LoadProfiles and
// LoadBindings use. Expiry is not a loading failure: an expired Exception is
// a normal, expected outcome that must remain visible, not an error.
func LoadExceptions(dirs []string, referenceTime time.Time) (ExceptionLoadResult, error) {
	var result ExceptionLoadResult
	for _, dir := range dirs {
		files, err := discoverYAMLFiles(dir)
		if err != nil {
			return ExceptionLoadResult{}, err
		}
		for _, f := range files {
			var e domain.Exception
			if err := decodeYAMLDocument(f, &e); err != nil {
				return ExceptionLoadResult{}, err
			}
			if err := domain.ValidateException(e); err != nil {
				return ExceptionLoadResult{}, fmt.Errorf("profiles: %s: %w", f, err)
			}
			if referenceTime.Before(e.ExpiresAt) {
				result.Live = append(result.Live, e)
			} else {
				result.Expired = append(result.Expired, e)
			}
		}
	}
	return result, nil
}
