package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

func TestRunDrift_Empty_Human(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runDrift(&stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runDrift = %d; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no drift") {
		t.Errorf("expected 'no drift' for a clean environment, got:\n%s", stdout.String())
	}
}

func TestRunDrift_And_DriftShow_RealCollision(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	writeCodexMCPCollision(t, env)

	var listOut, stderr bytes.Buffer
	if code := runDrift(&listOut, &stderr, []string{"--json"}); code != 0 {
		t.Fatalf("runDrift --json = %d; stderr:\n%s", code, stderr.String())
	}
	var cards []report.DriftCard
	if err := json.Unmarshal(listOut.Bytes(), &cards); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput:\n%s", err, listOut.String())
	}
	if len(cards) == 0 {
		t.Fatalf("expected at least one drift card; stderr:\n%s", stderr.String())
	}
	id := cards[0].ID

	var showOut bytes.Buffer
	if code := runDrift(&showOut, &stderr, []string{"show", id}); code != 0 {
		t.Fatalf("runDrift show %s = %d; stderr:\n%s", id, code, stderr.String())
	}
	if !strings.Contains(showOut.String(), id) {
		t.Errorf("drift show output missing the card's own ID %q:\n%s", id, showOut.String())
	}
	if !strings.Contains(showOut.String(), "Matrix") {
		t.Errorf("drift show output missing 'Matrix' section:\n%s", showOut.String())
	}
}

func TestRunDrift_Show_UnknownID(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runDrift(&stdout, &stderr, []string{"show", "DR-deadbeef"})
	if code != 1 {
		t.Fatalf("runDrift show <unknown> = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no drift card") {
		t.Errorf("expected an explanatory error, got:\n%s", stderr.String())
	}
}

func TestRunDrift_Show_MissingID(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runDrift(&stdout, &stderr, []string{"show"})
	if code != 2 {
		t.Fatalf("runDrift show (no id) = %d, want 2", code)
	}
}
