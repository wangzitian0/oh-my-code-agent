package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

func TestRunCompare_NativeVsCurrent_Human(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runCompare(&stdout, &stderr, []string{"--native", "--current"})
	if code != 0 {
		t.Fatalf("runCompare --native --current = %d; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "NATIVE") || !strings.Contains(stdout.String(), "CURRENT") {
		t.Errorf("output missing plane names:\n%s", stdout.String())
	}
}

func TestRunCompare_JSON(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runCompare(&stdout, &stderr, []string{"--observed", "--desired", "--json"})
	if code != 0 {
		t.Fatalf("runCompare --observed --desired --json = %d; stderr:\n%s", code, stderr.String())
	}
	var result report.CompareResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput:\n%s", err, stdout.String())
	}
	if result.PlaneA != report.PlaneObserved || result.PlaneB != report.PlaneDesired {
		t.Errorf("PlaneA/PlaneB = %q/%q", result.PlaneA, result.PlaneB)
	}
}

func TestRunCompare_WrongNumberOfPlaneFlags(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runCompare(&stdout, &stderr, []string{"--native"})
	if code != 2 {
		t.Fatalf("runCompare --native (only one plane) = %d, want 2", code)
	}
}
