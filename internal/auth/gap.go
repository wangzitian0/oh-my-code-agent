package auth

import (
	"fmt"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// LoginQualificationIssueURL is the follow-up GitHub issue filed per issue
// #27 (PR-23)'s round-3 pre-dispatch safety audit: the real, one-time,
// human-performed identity-specific runtime login (ADR 0003 rung 3) and the
// OS-keyring safely-reusable-across-isolated-homes qualification (ADR 0003
// rung 1; docs/architecture/runtime.md §8) are both still open. This
// package builds and tests the fallback DECISION logic only, against a fake
// host binary and a mock KeyringProbe (see doc.go); it never performs a real
// login or exercises the real OS keyring. Mirrors
// internal/runtime/policy.go's ClaudeConfigDirExclusionGapIssueURL /
// compile.go's claudeConfigDirExclusionGapSources pattern for issue #47: a
// shipped capability gap is tracked, never hidden.
const LoginQualificationIssueURL = "https://github.com/wangzitian0/oh-my-code-agent/issues/65"

// QualificationGapSources returns the capability-gap
// domain.GenerationSourceEntry records this package's two still-open
// qualification questions would attach to a compiled generation for host,
// in exactly the shape internal/runtime/compile.go's
// claudeConfigDirExclusionGapSources uses for issue #47 (CapabilityGap:
// true, non-empty TrackingIssue — domain.ValidateGeneration rejects the
// opposite combination outright).
//
// Not yet wired into internal/runtime's compiler output. internal/runtime's
// existing TestBootstrap_Codex_NoCapabilityGapEntries asserts a Codex
// bootstrap generation carries zero capability-gap entries today; folding
// this package's gap entries into every generation unconditionally would
// silently break that already-established, deliberate contract without a
// coordinated change to internal/runtime itself, which is out of this PR's
// scope (internal/auth is not yet consulted by any real Decide call inside
// the compiler — that integration is future work once a Compile call
// actually plans to invoke Decide for a live credential decision). This
// function is exported now so that future integration, and this package's
// own tests, share exactly one canonical gap description instead of two
// copies that could drift apart.
func QualificationGapSources(host string) []domain.GenerationSourceEntry {
	const reasonTemplate = "capability gap: %s -- see internal/auth/doc.go and issue #27's round-3 safety scoping for why this PR builds and tests the decision logic only, never the real thing; capability-gap shipping is allowed, hiding is not (issue #13 round-2 audit)"
	return []domain.GenerationSourceEntry{
		{
			Concept:  "credential_fallback_rung1_os_keyring",
			Scope:    "user",
			Host:     host,
			Included: false,
			Reason: fmt.Sprintf(reasonTemplate,
				"whether OS-keyring credential reuse is safe across isolated homes on this platform has not been qualified (docs/architecture/runtime.md §8); rung 1 is never selected until a human qualifies it, and every Decide call falls through toward rung 3"),
			CapabilityGap: true,
			TrackingIssue: LoginQualificationIssueURL,
		},
		{
			Concept:  "credential_fallback_rung3_identity_login",
			Scope:    "user",
			Host:     host,
			Included: false,
			Reason: fmt.Sprintf(reasonTemplate,
				"rung 3's identity-specific runtime login has only been proven as detection/fallback decision logic against a fake host binary fixture; the real, one-time, human-performed login end-to-end proof is a manual qualification step tracked separately"),
			CapabilityGap: true,
			TrackingIssue: LoginQualificationIssueURL,
		},
	}
}
