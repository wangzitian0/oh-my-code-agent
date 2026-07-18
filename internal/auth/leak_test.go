package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain/redact"
)

// plantedFakeSecret is an obviously-fake, test-scoped, secret-shaped value
// (never a real credential) planted into an Environment this test builds
// itself, so this file never has any real secret material to leak in the
// first place — mirrors internal/observe/redact_test.go's
// realSecretPlaceholder discipline, adapted here because this package has
// no existing committed fixture carrying a secret-shaped literal to reuse.
const plantedFakeSecret = "sk-test-fake-anthropic-key-do-not-use-1234567890"

// plantedRedactableSecret is a second planted, obviously-fake value shaped
// to actually match internal/domain/redact's secretShapePattern (a
// "Bearer <token>" shape, redact.go's first alternative) — plantedFakeSecret
// above intentionally contains hyphens redact's bare "sk-[a-z0-9]{8,}"
// shape does not match, which is fine for proving Decide/Decision never
// carry a value at all, but would make TestDecision_RedactionSafe_
// ViaSharedRedactPackage's specific "redact.JSON actually strips this"
// assertion vacuously depend on shape-matching quirks rather than the
// property under test.
const plantedRedactableSecret = "Bearer sk-fakeTestToken1234567890neverreal"

// TestDecision_NeverCarriesSecretMaterial is this PR's direct proof of AC
// #3, "a leak test proves credentials never appear in generated config or
// reports," scoped to this package's own new output type: even when Decide
// is given an Environment whose direnv-provided secret variable actually
// holds a secret-shaped value, the returned Decision's JSON serialization
// — the shape that would flow into a generation manifest or report — must
// never contain that value, only the environment variable's NAME.
func TestDecision_NeverCarriesSecretMaterial(t *testing.T) {
	env := hostcontext.Environment{Vars: []string{"ANTHROPIC_API_KEY=" + plantedFakeSecret}}
	qual := KeyringQualification("darwin-arm64")

	decision, err := Decide(context.Background(), "claude-code", env, nil, qual)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Rung != RungDirenvSecretReference {
		t.Fatalf("Rung = %v, want RungDirenvSecretReference (this test is vacuous otherwise)", decision.Rung)
	}

	raw, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("json.Marshal(decision): %v", err)
	}
	if strings.Contains(string(raw), plantedFakeSecret) {
		t.Fatalf("Decision JSON leaked the planted secret value:\n%s", raw)
	}
	if !strings.Contains(string(raw), "ANTHROPIC_API_KEY") {
		t.Error("Decision JSON does not name the env var at all -- EnvVar should still be recorded as a reference")
	}
}

// TestDecision_Rung1_NeverCarriesKeyringSecretMaterial is the rung-1
// analogue: KeyringProbe.Lookup only ever returns exists/err (see
// keyring.go's interface doc comment — "never returning ... secret
// material"), so there is structurally no secret value for a Decision to
// leak on this path either; this test proves the KeyringService/
// KeyringAccount reference fields alone are what get serialized.
func TestDecision_Rung1_NeverCarriesKeyringSecretMaterial(t *testing.T) {
	keyring := &mockKeyringProbe{entries: map[[2]string]bool{{"omca-codex", "default"}: true}}
	qual := PlatformKeyringQualification{Platform: "test-platform", Qualified: true}

	decision, err := Decide(context.Background(), "codex", hostcontext.Environment{}, keyring, qual)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	raw, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("json.Marshal(decision): %v", err)
	}
	// There is no secret value anywhere in this test's own fixtures to
	// search for (KeyringProbe never hands one back) — the meaningful
	// assertion is that only the reference fields (service/account NAMES)
	// appear, never anything shaped like a token.
	if strings.Contains(string(raw), "sk-") || strings.Contains(string(raw), "Bearer") {
		t.Fatalf("Decision JSON contains secret-shaped content it should never have: %s", raw)
	}
}

// TestDecision_RedactionSafe_ViaSharedRedactPackage reuses
// internal/domain/redact — the same generic secret-pattern-matching
// machinery every other output-safety test in this project relies on
// (init.md invariant "secrets do not enter reports, plans, manifests, or
// model context") — rather than inventing a second redaction mechanism,
// exactly as issue #27's own task brief directs. It proves redact.JSON
// would still catch a leak even if a future change to this package
// accidentally added a raw secret-shaped field to Decision.
func TestDecision_RedactionSafe_ViaSharedRedactPackage(t *testing.T) {
	// Construct a Decision carrying a secret-shaped value directly, standing
	// in for "a future bug added a field/value that shouldn't be there" —
	// Decide itself never does this (see the two tests above), so this is a
	// defense-in-depth proof that the shared redact package would still
	// catch it if some other code path in this package ever regressed.
	decision := Decision{
		Host:   "claude-code",
		Rung:   RungDirenvSecretReference,
		Reason: "leaked value for test purposes: " + plantedRedactableSecret,
		EnvVar: "ANTHROPIC_API_KEY",
	}

	rawJSON, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(rawJSON), plantedRedactableSecret) {
		t.Fatalf("raw (unredacted) Decision JSON does not contain the planted secret; this test would be vacuous:\n%s", rawJSON)
	}

	redactedJSON, err := redact.JSON(decision)
	if err != nil {
		t.Fatalf("redact.JSON: %v", err)
	}
	if strings.Contains(string(redactedJSON), plantedRedactableSecret) {
		t.Fatalf("redact.JSON(decision) leaked the planted secret:\n%s", redactedJSON)
	}
	if !strings.Contains(string(redactedJSON), "REDACTED:sha256:") {
		t.Error("expected the redacted output to contain at least one REDACTED marker")
	}

	redactedReport, err := redact.Report(decision)
	if err != nil {
		t.Fatalf("redact.Report: %v", err)
	}
	if strings.Contains(redactedReport, plantedRedactableSecret) {
		t.Fatalf("redact.Report(decision) leaked the planted secret:\n%s", redactedReport)
	}
}
