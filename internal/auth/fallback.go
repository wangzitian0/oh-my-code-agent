package auth

import (
	"context"
	"fmt"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Rung identifies which of ADR 0003 decision item 2's four fallback rungs a
// Decision selected. Values are 1-indexed (not iota-from-0) so a zero
// Decision.Rung is visibly "unset" rather than silently reading as a valid
// rung 1.
type Rung int

const (
	// RungOSCredentialStore: the platform keyring/credential manager,
	// queried per isolated home once the platform and mechanism have been
	// qualified.
	RungOSCredentialStore Rung = iota + 1
	// RungDirenvSecretReference: an API key or secret reference exported
	// into the environment by the worktree's .envrc, never written to disk
	// by OMCA.
	RungDirenvSecretReference
	// RungIdentityRuntimeLogin: the host's own native login flow, run once
	// per isolated identity/generation family.
	RungIdentityRuntimeLogin
	// RungExplicitReviewedMigration: a human-approved, one-time, logged
	// import of a specific credential when none of the above apply. Never
	// selected automatically by Decide — see ProposeMigration.
	RungExplicitReviewedMigration
)

// String renders r using ADR 0003's own vocabulary for each rung, so a
// Decision's Reason/logging never has to duplicate this mapping.
func (r Rung) String() string {
	switch r {
	case RungOSCredentialStore:
		return "os-credential-store"
	case RungDirenvSecretReference:
		return "direnv-provided-secret-reference"
	case RungIdentityRuntimeLogin:
		return "identity-specific-runtime-login"
	case RungExplicitReviewedMigration:
		return "explicit-reviewed-migration"
	default:
		return fmt.Sprintf("unknown-rung(%d)", int(r))
	}
}

// Decision is the outcome of one Decide (or ProposeMigration) call: which
// rung was selected, why, and enough detail for a caller to act on it —
// but, per ADR 0003 decision item 1 ("auth/credential state is never
// copied into a generation ... manifests may store a reference ... but
// never the secret material itself"), NEVER the credential material itself.
// KeyringAccount/KeyringService are references (a keyring entry's own
// identifying name, not its value); EnvVar names an environment variable,
// never its value; Invocation carries a command and flags, never a secret
// argument (nativeLoginInvocation only ever returns flag-only argument
// lists). See TestDecision_NeverCarriesSecretMaterial for the leak-test
// proof of this property.
type Decision struct {
	Host   string
	Rung   Rung
	Reason string

	// KeyringService/KeyringAccount are set when Rung == RungOSCredentialStore.
	KeyringService string
	KeyringAccount string

	// EnvVar is set when Rung == RungDirenvSecretReference: the name of the
	// environment variable that supplied the secret reference, never its
	// value.
	EnvVar string

	// Invocation is set when Rung == RungIdentityRuntimeLogin: the native
	// login command this rung would run.
	Invocation *InvocationPlan

	// MigrationNote is set when Rung == RungExplicitReviewedMigration: the
	// human-supplied justification ProposeMigration required as an
	// argument, never a value Decide can synthesize on its own.
	MigrationNote string
}

// direnvSecretEnvVar names, per host, the environment variable ADR 0003
// decision item 2's rung 2 expects a worktree's .envrc to export. Verified
// against real CLI --help output: `codex login --help` documents
// `--with-api-key` as reading "the API key from stdin (e.g. `printenv
// OPENAI_API_KEY | codex login --with-api-key`)"; `claude --help`'s --bare
// flag documents "Anthropic auth is strictly ANTHROPIC_API_KEY or
// apiKeyHelper via --settings."
var direnvSecretEnvVar = map[string]string{
	"codex":       "OPENAI_API_KEY",
	"claude-code": "ANTHROPIC_API_KEY",
}

// keyringServiceAccount names the service/account rung 1 would look up for
// host, once a platform is qualified (KeyringQualification) and a real
// KeyringProbe is wired in. "omca-<host>"/"default" is a placeholder,
// OMCA-owned reference name — ADR 0003 decision item 1 allows a generation
// manifest to record "a reference to where a credential lives (e.g. 'OS
// keyring, service X, account Y')"; the real service/account naming
// convention a qualified implementation actually uses is part of the
// deferred qualification (LoginQualificationIssueURL), not fixed here.
func keyringServiceAccount(host string) (service, account string) {
	return "omca-" + host, "default"
}

// SupportedHosts are the canonical host IDs this package implements
// fallback decision logic for.
var SupportedHosts = []string{"codex", "claude-code"}

// Decide implements ADR 0003 decision item 2's four-rung fallback order for
// host, stopping at the first rung that is actually satisfied:
//
//	OS credential store
//	  -> direnv-provided secret reference
//	  -> identity-specific runtime login
//
// Rung 4 (explicit, reviewed migration) is never returned by Decide — see
// ProposeMigration's doc comment for why it is a separate, human-invoked
// function instead. qual is the platform's rung-1 qualification result —
// production callers pass KeyringQualification(platform) explicitly
// (production behavior is therefore always Qualified: false today, per
// keyring.go); tests construct their own PlatformKeyringQualification{
// Qualified: true, ...} directly to exercise the rung-1 branch against a
// mock KeyringProbe without needing to override a hardcoded internal call.
// keyring may be nil (treated as "no keyring probe available," equivalent
// to Qualified: false for the purpose of this call).
func Decide(ctx context.Context, host string, env hostcontext.Environment, keyring KeyringProbe, qual PlatformKeyringQualification) (Decision, error) {
	if err := domain.ValidateHostID(host); err != nil {
		return Decision{}, fmt.Errorf("auth: Decide: %w", err)
	}
	envVar, ok := direnvSecretEnvVar[host]
	if !ok {
		return Decision{}, fmt.Errorf("auth: Decide: host %q has no known fallback wiring in this package (only %v)", host, SupportedHosts)
	}

	// Rung 1: OS credential store, only ever consulted once the platform is
	// qualified (see keyring.go — hardcoded unqualified everywhere today).
	if qual.Qualified && keyring != nil {
		service, account := keyringServiceAccount(host)
		exists, err := keyring.Lookup(ctx, service, account)
		if err != nil {
			return Decision{}, fmt.Errorf("auth: Decide: rung 1 keyring lookup for host %q: %w", host, err)
		}
		if exists {
			return Decision{
				Host:           host,
				Rung:           RungOSCredentialStore,
				Reason:         fmt.Sprintf("OS credential store is qualified for platform %q and an entry exists for service %q account %q", qual.Platform, service, account),
				KeyringService: service,
				KeyringAccount: account,
			}, nil
		}
	}

	// Rung 2: direnv-provided secret reference. Only the environment
	// variable's NAME is ever recorded — never Get's returned value.
	if env.Get(envVar) != "" {
		return Decision{
			Host:   host,
			Rung:   RungDirenvSecretReference,
			Reason: fmt.Sprintf("environment variable %s is set (a worktree .envrc-provided secret reference; the value itself is never recorded in a Decision)", envVar),
			EnvVar: envVar,
		}, nil
	}

	// Rung 3: identity-specific runtime login. Always reachable as a
	// decision (this package never fails closed here) — whether the host
	// binary is actually installed is a separate concern Invoke reports at
	// execution time (a Skipped InvocationResult), not something Decide
	// needs to pre-check to correctly identify which rung applies.
	plan, err := nativeLoginInvocation(host)
	if err != nil {
		return Decision{}, fmt.Errorf("auth: Decide: %w", err)
	}
	return Decision{
		Host: host,
		Rung: RungIdentityRuntimeLogin,
		Reason: fmt.Sprintf(
			"no OS-credential-store entry (platform %q qualified=%v) and %s is not set; falling back to identity-specific runtime login",
			qual.Platform, qual.Qualified, envVar,
		),
		Invocation: &plan,
	}, nil
}

// ProposeMigration builds a rung-4 Decision: ADR 0003's "explicit, reviewed
// migration" — "a deliberate exception path, not a default, and must be
// reviewable." Decide never returns this rung on its own; only an explicit
// caller (standing in for a human reviewer) can request it, and only with a
// non-empty reason, so a rung-4 Decision is always attributable to a stated
// justification rather than a silent fallback (ADR 0003 consequence: "any
// future explicit reviewed migration path must be logged and
// attributable").
func ProposeMigration(host, reason string) (Decision, error) {
	if err := domain.ValidateHostID(host); err != nil {
		return Decision{}, fmt.Errorf("auth: ProposeMigration: %w", err)
	}
	if reason == "" {
		return Decision{}, fmt.Errorf("auth: ProposeMigration: reason is required -- rung 4 must be reviewable, never a silent default")
	}
	return Decision{
		Host:          host,
		Rung:          RungExplicitReviewedMigration,
		Reason:        "explicit, reviewed migration requested by a human reviewer",
		MigrationNote: reason,
	}, nil
}
