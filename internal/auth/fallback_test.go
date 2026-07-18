package auth

import (
	"context"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
)

// mockKeyringProbe is an in-memory KeyringProbe standing in for a real OS
// credential store — never touches this machine's actual keychain. Its
// entries map is exactly the closed set of (service, account) pairs it will
// report as existing; anything else reports false, nil.
type mockKeyringProbe struct {
	entries map[[2]string]bool
	err     error
}

func (m *mockKeyringProbe) Lookup(_ context.Context, service, account string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.entries[[2]string{service, account}], nil
}

func envWith(vars ...string) hostcontext.Environment {
	return hostcontext.Environment{Vars: vars}
}

// TestDecide_Rung1_OSCredentialStore proves the rung-1 branch is real,
// tested decision logic — exercised entirely through a mock KeyringProbe
// and an explicitly-constructed "qualified" PlatformKeyringQualification a
// test builds itself, never the real OS keychain (KeyringQualification, the
// function production code actually calls, is hardcoded unqualified; see
// keyring.go).
func TestDecide_Rung1_OSCredentialStore(t *testing.T) {
	keyring := &mockKeyringProbe{entries: map[[2]string]bool{
		{"omca-codex", "default"}: true,
	}}
	qual := PlatformKeyringQualification{Platform: "test-platform", Qualified: true}

	got, err := Decide(context.Background(), "codex", envWith(), keyring, qual)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got.Rung != RungOSCredentialStore {
		t.Fatalf("Rung = %v, want RungOSCredentialStore", got.Rung)
	}
	if got.KeyringService != "omca-codex" || got.KeyringAccount != "default" {
		t.Errorf("KeyringService/Account = %q/%q, want omca-codex/default", got.KeyringService, got.KeyringAccount)
	}
	if got.Reason == "" {
		t.Error("Reason is empty")
	}
}

// TestDecide_Rung1_QualifiedButNoEntry_FallsThrough proves rung 1 being
// qualified is not, by itself, enough to select it — an actual entry must
// exist, or the decision falls through toward rung 2/3.
func TestDecide_Rung1_QualifiedButNoEntry_FallsThrough(t *testing.T) {
	keyring := &mockKeyringProbe{entries: map[[2]string]bool{}} // qualified platform, but nothing stored
	qual := PlatformKeyringQualification{Platform: "test-platform", Qualified: true}

	got, err := Decide(context.Background(), "codex", envWith(), keyring, qual)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got.Rung != RungIdentityRuntimeLogin {
		t.Fatalf("Rung = %v, want RungIdentityRuntimeLogin (fell through past rungs 1 and 2)", got.Rung)
	}
}

// TestDecide_Rung1_UnqualifiedPlatform_NeverConsultsKeyring proves an
// unqualified platform (production's actual, hardcoded posture — see
// keyring.go's KeyringQualification) skips rung 1 entirely even when a
// keyring entry DOES exist, and never even calls Lookup: this is the
// concrete regression proof that "until qualified for a given platform,
// adapters fall through to rung three" (ADR 0003 consequence) is really
// enforced, not just documented.
func TestDecide_Rung1_UnqualifiedPlatform_NeverConsultsKeyring(t *testing.T) {
	calls := 0
	keyring := keyringProbeFunc(func(_ context.Context, _, _ string) (bool, error) {
		calls++
		return true, nil
	})
	qual := KeyringQualification("darwin-arm64") // production's real, hardcoded-unqualified path

	got, err := Decide(context.Background(), "codex", envWith(), keyring, qual)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if calls != 0 {
		t.Errorf("keyring.Lookup was called %d times, want 0 -- an unqualified platform must never consult the keyring at all", calls)
	}
	if got.Rung != RungIdentityRuntimeLogin {
		t.Fatalf("Rung = %v, want RungIdentityRuntimeLogin", got.Rung)
	}
}

// keyringProbeFunc adapts a function to KeyringProbe, for tests that only
// need to assert call count/arguments rather than maintain a map.
type keyringProbeFunc func(ctx context.Context, service, account string) (bool, error)

func (f keyringProbeFunc) Lookup(ctx context.Context, service, account string) (bool, error) {
	return f(ctx, service, account)
}

// TestDecide_Rung2_DirenvSecretReference proves rung 2 is selected when the
// host's documented env var is set and rung 1 does not apply (unqualified
// platform, the production default).
func TestDecide_Rung2_DirenvSecretReference(t *testing.T) {
	tests := []struct {
		host   string
		envVar string
	}{
		{"codex", "OPENAI_API_KEY"},
		{"claude-code", "ANTHROPIC_API_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			env := envWith(tt.envVar + "=sk-test-fake-value-never-real")
			qual := KeyringQualification("darwin-arm64")
			got, err := Decide(context.Background(), tt.host, env, nil, qual)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got.Rung != RungDirenvSecretReference {
				t.Fatalf("Rung = %v, want RungDirenvSecretReference", got.Rung)
			}
			if got.EnvVar != tt.envVar {
				t.Errorf("EnvVar = %q, want %q", got.EnvVar, tt.envVar)
			}
		})
	}
}

// TestDecide_Rung3_IdentityRuntimeLogin proves rung 3 is selected, with the
// exact native login command and arguments, when neither rung 1 nor rung 2
// apply -- this is the "correctly identify and invoke rung 3's native login
// command with the right arguments/environment" half of issue #27's round-3
// scoping, proven as decision logic here; TestInvoke_Rung3_RunsFakeLoginBinary
// (invoke_test.go) proves the "invoking" half against a fake binary.
func TestDecide_Rung3_IdentityRuntimeLogin(t *testing.T) {
	tests := []struct {
		host        string
		wantCommand string
		wantArgs    []string
	}{
		{"codex", "codex", []string{"login"}},
		{"claude-code", "claude", []string{"auth", "login"}},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			qual := KeyringQualification("darwin-arm64")
			got, err := Decide(context.Background(), tt.host, envWith(), nil, qual)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got.Rung != RungIdentityRuntimeLogin {
				t.Fatalf("Rung = %v, want RungIdentityRuntimeLogin", got.Rung)
			}
			if got.Invocation == nil {
				t.Fatal("Invocation is nil, want a populated InvocationPlan")
			}
			if got.Invocation.Command != tt.wantCommand {
				t.Errorf("Invocation.Command = %q, want %q", got.Invocation.Command, tt.wantCommand)
			}
			if strings.Join(got.Invocation.Args, " ") != strings.Join(tt.wantArgs, " ") {
				t.Errorf("Invocation.Args = %v, want %v", got.Invocation.Args, tt.wantArgs)
			}
		})
	}
}

// TestDecide_FallbackOrderPrecedence proves the RUNGS ARE TRIED IN ORDER:
// when both rung 1 (qualified + entry present) and rung 2 (env var set)
// would independently be satisfiable, rung 1 wins; when rung 1 does not
// apply but rung 2 does, rung 2 wins over rung 3. This is the single most
// direct test of ADR 0003 decision item 2's "fixed order ... stopping at
// the first source that satisfies the need."
func TestDecide_FallbackOrderPrecedence(t *testing.T) {
	keyring := &mockKeyringProbe{entries: map[[2]string]bool{{"omca-codex", "default"}: true}}
	env := envWith("OPENAI_API_KEY=sk-test-fake-value-never-real")

	// Both rung 1 and rung 2 satisfiable -> rung 1 wins.
	qualQualified := PlatformKeyringQualification{Platform: "test-platform", Qualified: true}
	got, err := Decide(context.Background(), "codex", env, keyring, qualQualified)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got.Rung != RungOSCredentialStore {
		t.Fatalf("Rung = %v, want RungOSCredentialStore (rung 1 must win over rung 2 when both apply)", got.Rung)
	}

	// Rung 1 unqualified, rung 2 satisfiable -> rung 2 wins over rung 3.
	qualUnqualified := KeyringQualification("darwin-arm64")
	got2, err := Decide(context.Background(), "codex", env, keyring, qualUnqualified)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got2.Rung != RungDirenvSecretReference {
		t.Fatalf("Rung = %v, want RungDirenvSecretReference (rung 2 must win over rung 3 when rung 1 is unqualified)", got2.Rung)
	}
}

func TestDecide_RejectsUnknownHost(t *testing.T) {
	qual := KeyringQualification("darwin-arm64")
	if _, err := Decide(context.Background(), "not-a-real-host", envWith(), nil, qual); err == nil {
		t.Error("Decide(unknown host) error = nil, want error")
	}
}

func TestDecide_RejectsHostWithNoFallbackWiring(t *testing.T) {
	// "opencode" is a valid domain.KnownHostIDs entry but this package does
	// not implement fallback wiring for it (SupportedHosts is only codex
	// and claude-code) -- Decide must fail loudly, not silently pick a
	// wrong rung.
	qual := KeyringQualification("darwin-arm64")
	if _, err := Decide(context.Background(), "opencode", envWith(), nil, qual); err == nil {
		t.Error("Decide(opencode) error = nil, want error (no fallback wiring for this host)")
	}
}

// TestProposeMigration_Rung4_NeverAutomatic proves rung 4 requires an
// explicit, non-empty, human-supplied reason and is never something Decide
// itself can produce.
func TestProposeMigration_Rung4_NeverAutomatic(t *testing.T) {
	got, err := ProposeMigration("codex", "one-time import approved by maintainer in PR review, see issue #NNN")
	if err != nil {
		t.Fatalf("ProposeMigration: %v", err)
	}
	if got.Rung != RungExplicitReviewedMigration {
		t.Fatalf("Rung = %v, want RungExplicitReviewedMigration", got.Rung)
	}
	if got.MigrationNote == "" {
		t.Error("MigrationNote is empty, want the supplied reason")
	}

	if _, err := ProposeMigration("codex", ""); err == nil {
		t.Error("ProposeMigration(host, \"\") error = nil, want error -- rung 4 must be reviewable, never silent")
	}
	if _, err := ProposeMigration("not-a-real-host", "reason"); err == nil {
		t.Error("ProposeMigration(unknown host, reason) error = nil, want error")
	}
}

func TestRungString(t *testing.T) {
	cases := map[Rung]string{
		RungOSCredentialStore:         "os-credential-store",
		RungDirenvSecretReference:     "direnv-provided-secret-reference",
		RungIdentityRuntimeLogin:      "identity-specific-runtime-login",
		RungExplicitReviewedMigration: "explicit-reviewed-migration",
	}
	for rung, want := range cases {
		if got := rung.String(); got != want {
			t.Errorf("Rung(%d).String() = %q, want %q", rung, got, want)
		}
	}
	if got := Rung(0).String(); got == "" {
		t.Error("Rung(0).String() is empty, want a non-empty unknown-rung message")
	}
}
