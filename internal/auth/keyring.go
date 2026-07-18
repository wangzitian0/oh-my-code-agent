package auth

import "context"

// KeyringProbe is a read-only, non-destructive existence/metadata check
// against an OS credential store: whether an entry exists for service and
// account, never returning, logging, or asserting on the entry's own secret
// material. Implementations must never create, modify, or delete an entry
// (this project's own round-3 safety scoping for issue #27: "may only READ
// ... never creating, modifying, or deleting one, and never reading out
// actual secret material into a test assertion, log, or fixture").
//
// This package ships no real, platform-backed implementation — see doc.go
// and LoginQualificationIssueURL. Every test in this package that needs a
// KeyringProbe constructs a small in-memory fake (see fallback_test.go)
// rather than touching this machine's actual keychain.
type KeyringProbe interface {
	// Lookup reports whether an entry exists for service/account. exists is
	// only meaningful when err is nil.
	Lookup(ctx context.Context, service, account string) (exists bool, err error)
}

// PlatformKeyringQualification records ADR 0003's M5 qualification gate
// (docs/architecture/runtime.md §8: "before the first Codex managed
// milestone, qualification must determine whether OS-keyring credentials
// are safely reusable across isolated homes on the target platform") for
// one platform string (the "<goos>-<goarch>" shape internal/context's
// platformString already uses, e.g. "darwin-arm64").
type PlatformKeyringQualification struct {
	Platform  string
	Qualified bool
	Reason    string
}

// KeyringQualification reports rung 1's qualification status for platform.
// Hardcoded Qualified: false for every platform: this PR's own safety
// scoping requires the real qualification (an actual, human-supervised
// probe against the real OS credential store, per KeyringProbe's doc
// comment) to happen out-of-band, tracked by LoginQualificationIssueURL —
// never self-certified by an autonomous test run. Until a future change
// updates this function for a specific, qualified platform, Decide always
// falls through past rung 1 toward rung 2/3 (ADR 0003 consequence: "until
// qualified for a given platform, adapters fall through to rung three for
// that platform").
func KeyringQualification(platform string) PlatformKeyringQualification {
	return PlatformKeyringQualification{
		Platform:  platform,
		Qualified: false,
		Reason: "OS-keyring credential reuse across isolated homes has not been behaviorally qualified for any platform " +
			"(docs/architecture/runtime.md §8 M5 gate; ADR 0003 consequence); Decide falls through toward rung 3 until a " +
			"human performs this qualification out-of-band -- see " + LoginQualificationIssueURL,
	}
}
