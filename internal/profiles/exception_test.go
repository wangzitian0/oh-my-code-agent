package profiles

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const exceptionTemplate = `
apiVersion: omca.dev/v1alpha1
kind: Exception
metadata:
  id: %s
assetId: %s
scope: %s
justification: %s
expiresAt: %s
`

func fmtException(id, assetID, scope, justification, expiresAt string) string {
	return fmt.Sprintf(exceptionTemplate, id, assetID, scope, justification, expiresAt)
}

// TestLoadExceptions_LiveAndExpiredSplit is issue #16's round-2 AC: "An
// expired exception is inert and reported as expired (test)." It loads two
// real Exception YAML documents from disk, one whose expiresAt is after the
// reference time and one whose expiresAt is before it, and proves
// LoadExceptions splits them into Live and Expired rather than either
// silently dropping the expired one or treating it as still live.
func TestLoadExceptions_LiveAndExpiredSplit(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "live.yaml"), fmtException(
		"exception:live", "banned-tool", "company:policy",
		"temporary security-reviewed allowance", "2026-06-01T00:00:00Z",
	))
	mustWriteFile(t, filepath.Join(dir, "expired.yaml"), fmtException(
		"exception:expired", "release-production", "company:policy",
		"allowance that has since lapsed", "2025-01-01T00:00:00Z",
	))

	referenceTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	result, err := LoadExceptions([]string{dir}, referenceTime)
	if err != nil {
		t.Fatalf("LoadExceptions: %v", err)
	}

	if len(result.Live) != 1 || result.Live[0].AssetID != "banned-tool" {
		t.Fatalf("Live = %+v, want exactly one entry for banned-tool", result.Live)
	}
	if len(result.Expired) != 1 || result.Expired[0].AssetID != "release-production" {
		t.Fatalf("Expired = %+v, want exactly one entry for release-production", result.Expired)
	}
}

// TestLoadExceptions_ExpiryBoundaryIsExclusive mirrors internal/resolve.
// findException's own strict now.Before(ExpiresAt) rule: an exception whose
// expiresAt is exactly the reference time is expired, not live, so the two
// packages never disagree about a boundary instant.
func TestLoadExceptions_ExpiryBoundaryIsExclusive(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "boundary.yaml"), fmtException(
		"exception:boundary", "banned-tool", "company:policy",
		"expires exactly at the reference instant", "2026-01-01T00:00:00Z",
	))

	referenceTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	result, err := LoadExceptions([]string{dir}, referenceTime)
	if err != nil {
		t.Fatalf("LoadExceptions: %v", err)
	}
	if len(result.Live) != 0 {
		t.Errorf("Live = %+v, want none: an exception expiring exactly at referenceTime is expired", result.Live)
	}
	if len(result.Expired) != 1 {
		t.Errorf("Expired = %+v, want the boundary exception", result.Expired)
	}
}

// TestLoadExceptions_InvalidDocument_ActionableError proves a structurally
// invalid Exception (missing justification) fails closed with an error
// naming the file and field, matching LoadProfiles/LoadBindings.
func TestLoadExceptions_InvalidDocument_ActionableError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yaml")
	mustWriteFile(t, path, `
apiVersion: omca.dev/v1alpha1
kind: Exception
metadata:
  id: exception:broken
assetId: banned-tool
scope: company:policy
expiresAt: "2026-01-01T00:00:00Z"
`)
	_, err := LoadExceptions([]string{dir}, time.Now())
	if err == nil {
		t.Fatal("expected an error for a missing justification")
	}
	msg := err.Error()
	if !strings.Contains(msg, path) {
		t.Errorf("error %q does not name the file path %q", msg, path)
	}
	if !strings.Contains(msg, "justification") {
		t.Errorf("error %q does not name the offending field (justification)", msg)
	}
}

// TestLoadExceptions_MissingDirIsNotAnError matches the tolerance every
// other loader in this package has for docs/architecture/README.md §7's
// optional directories (e.g. no exceptions/ directory at all).
func TestLoadExceptions_MissingDirIsNotAnError(t *testing.T) {
	result, err := LoadExceptions([]string{filepath.Join(t.TempDir(), "does-not-exist")}, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Live) != 0 || len(result.Expired) != 0 {
		t.Errorf("result = %+v, want empty", result)
	}
}
