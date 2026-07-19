package tui

import (
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// emptyArtifactForTest is the zero-valued report.Artifact every Render*
// function's own "nothing to show yet" degrade path is tested against —
// shared across overview_test.go/drift_test.go/assets_test.go/
// generations_test.go rather than redefined per file.
func emptyArtifactForTest() report.Artifact {
	return report.Artifact{}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
