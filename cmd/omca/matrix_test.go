package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/drift"
)

func TestRunMatrix_RealCollision(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	writeCodexMCPCollision(t, env)

	var driftOut, stderr bytes.Buffer
	if code := runDrift(&driftOut, &stderr, []string{"--json"}); code != 0 {
		t.Fatalf("runDrift --json = %d; stderr:\n%s", code, stderr.String())
	}
	var cards []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(driftOut.Bytes(), &cards); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(cards) == 0 {
		t.Fatalf("no drift cards to test matrix against")
	}

	var matrixOut bytes.Buffer
	if code := runMatrix(&matrixOut, &stderr, []string{cards[0].ID, "--json"}); code != 0 {
		t.Fatalf("runMatrix = %d; stderr:\n%s", code, stderr.String())
	}
	var matrix []drift.Assertion
	if err := json.Unmarshal(matrixOut.Bytes(), &matrix); err != nil {
		t.Fatalf("json.Unmarshal matrix: %v\noutput:\n%s", err, matrixOut.String())
	}
	if len(matrix) == 0 {
		t.Error("matrix is empty")
	}
}

func TestRunMatrix_UnknownID(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runMatrix(&stdout, &stderr, []string{"DR-deadbeef"})
	if code != 1 {
		t.Fatalf("runMatrix <unknown> = %d, want 1", code)
	}
}

func TestRunMatrix_MissingArg(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runMatrix(&stdout, &stderr, nil)
	if code != 2 {
		t.Fatalf("runMatrix (no id) = %d, want 2", code)
	}
}
