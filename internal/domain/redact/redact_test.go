package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

// Fake secrets, never real credentials, embedded below at different nesting
// depths and in different field-name/shape combinations: some sit under a
// literally sensitive key name (token/password/apiKey/authorization), some
// are embedded as a substring inside an innocuously-named field (notes,
// header, note) so only the shape-based scan catches them.
// These string literals are shaped like real vendor secrets on purpose (the
// redact package's secretShapePattern must actually recognize sk-/ghp_/
// Bearer/env-style shapes), which also makes them look like real leaked
// credentials to a naive scanner; each line is marked gitleaks:allow for the
// repo's CI secret-scan job (.github/workflows/ci.yml) since these values
// never leave this test file and are not real credentials.
const (
	fakeAPIKey      = "sk-FAKESECRET1234567890ABCDEF"           // gitleaks:allow
	fakeBearerToken = "FAKEBEARERTOKENVALUE999"                 // gitleaks:allow
	fakePassword    = "hunter2FAKEPASSWORD"                     // gitleaks:allow
	fakeNestedKey   = "FAKEAPIKEYVALUEXYZ"                      // gitleaks:allow
	fakeGithubToken = "ghp_FAKEGITHUBTOKENabcdefghij1234567890" // gitleaks:allow
	fakeEnvToken    = "FAKEDBTOKENVALUE001"                     // gitleaks:allow
)

var fakeSecrets = []string{
	fakeAPIKey,
	fakeBearerToken,
	fakePassword,
	fakeNestedKey,
	fakeGithubToken,
	fakeEnvToken,
}

// innerCreds is nested two levels deep inside fixture, inside a slice, to
// exercise redaction below the top level.
type innerCreds struct {
	APIKey string `json:"apiKey"`
	Notes  string `json:"notes"`
}

type nestedAgent struct {
	Name  string     `json:"name"`
	Creds innerCreds `json:"creds"`
}

// fixture is the struct output path: a Go type whose JSON tags name some
// fields with sensitive key names directly, at top level and nested.
type fixture struct {
	Token         string        `json:"token"`
	Password      string        `json:"password"`
	Authorization string        `json:"authorization"`
	Agents        []nestedAgent `json:"agents"`
	Note          string        `json:"note"`
}

func buildFixture() fixture {
	return fixture{
		Token:         fakeAPIKey,
		Password:      fakePassword,
		Authorization: "Bearer " + fakeBearerToken,
		Agents: []nestedAgent{
			{
				Name: "codex",
				Creds: innerCreds{
					APIKey: fakeNestedKey,
					Notes:  "github token " + fakeGithubToken + " rotated last week",
				},
			},
		},
		Note: "debug: DATABASE_TOKEN=" + fakeEnvToken + " seen in logs",
	}
}

// buildGenericMap embeds the same fake secrets in a generic map[string]any
// tree instead of a typed struct, at comparable nesting depths, to exercise
// the "generic map's JSON output" path separately from the struct path.
func buildGenericMap() map[string]any {
	return map[string]any{
		"token": fakeAPIKey,
		"meta": map[string]any{
			"password": fakePassword,
			"nested": map[string]any{
				"apiKey": fakeNestedKey,
				"header": "Authorization: Bearer " + fakeBearerToken,
			},
		},
		"history": []any{
			map[string]any{"note": "rotated " + fakeGithubToken + " today"},
			"plain log line with DATABASE_TOKEN=" + fakeEnvToken + " embedded",
		},
	}
}

func assertNoSecrets(t *testing.T, label, output string) {
	t.Helper()
	for _, secret := range fakeSecrets {
		if strings.Contains(output, secret) {
			t.Errorf("%s: leaked fake secret substring %q\nfull output:\n%s", label, secret, output)
		}
	}
}

// assertFixtureIsVacuousProof fails the test if none of the fake secrets
// appear in the raw (unredacted) marshal of v — i.e. proves the fixture
// actually exercises the redaction logic instead of testing nothing.
func assertFixtureIsVacuousProof(t *testing.T, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal raw fixture: %v", err)
	}
	for _, secret := range fakeSecrets {
		if strings.Contains(string(raw), secret) {
			return
		}
	}
	t.Fatal("fixture's raw JSON contains none of the fake secrets; this test would be vacuous")
}

func TestValue_StructJSONOutput_NoSecretsLeak(t *testing.T) {
	f := buildFixture()
	assertFixtureIsVacuousProof(t, f)

	sanitized, err := JSON(f)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	assertNoSecrets(t, "struct JSON output", string(sanitized))
}

func TestValue_GenericMapJSONOutput_NoSecretsLeak(t *testing.T) {
	m := buildGenericMap()
	assertFixtureIsVacuousProof(t, m)

	sanitized, err := JSON(m)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	assertNoSecrets(t, "generic map JSON output", string(sanitized))
}

func TestReport_RenderedText_NoSecretsLeak(t *testing.T) {
	f := buildFixture()
	assertFixtureIsVacuousProof(t, f)

	text, err := Report(f)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	assertNoSecrets(t, "rendered report text", text)

	// Sanity: the report text is not empty/trivial (redaction did not
	// simply drop every field).
	if !strings.Contains(text, "REDACTED:sha256:") {
		t.Error("expected the report text to contain at least one REDACTED marker")
	}
	if !strings.Contains(text, "codex") {
		t.Error("expected non-secret content (agent name) to survive redaction")
	}
}

func TestReport_GenericMap_NoSecretsLeak(t *testing.T) {
	m := buildGenericMap()
	assertFixtureIsVacuousProof(t, m)

	text, err := Report(m)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	assertNoSecrets(t, "rendered report text (generic map)", text)
}

func TestMarker_StableAndTraceable(t *testing.T) {
	a := marker("same-input")
	b := marker("same-input")
	if a != b {
		t.Fatalf("marker is not stable: %q != %q", a, b)
	}
	c := marker("different-input")
	if a == c {
		t.Fatal("marker did not vary with input")
	}
	if !strings.HasPrefix(a, "REDACTED:sha256:") {
		t.Fatalf("marker %q missing REDACTED:sha256: prefix", a)
	}
}

func TestValue_NonSensitiveFieldsSurvive(t *testing.T) {
	m := map[string]any{"name": "codex", "count": float64(3)}
	sanitized, err := Value(m)
	if err != nil {
		t.Fatal(err)
	}
	out, ok := sanitized.(map[string]any)
	if !ok {
		t.Fatalf("Value returned %T, want map[string]any", sanitized)
	}
	if out["name"] != "codex" {
		t.Errorf("name = %v, want unredacted \"codex\"", out["name"])
	}
	if out["count"] != float64(3) {
		t.Errorf("count = %v, want unredacted 3", out["count"])
	}
}

func TestValue_RejectsUnmarshalable(t *testing.T) {
	if _, err := Value(make(chan int)); err == nil {
		t.Error("expected an error redacting an unmarshalable value")
	}
}
