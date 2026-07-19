package profiles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// LoadActivation reads and validates the single Activation YAML document at
// path (docs/architecture/README.md §8:
// <worktree state dir>/desired/activation.yaml — worktree-local state, not
// repository configuration, per §7's "Personal identity selection and
// worktree activation are local state, not shared repository
// configuration").
//
// A path that does not exist is not an error: ok is false and the zero
// domain.Activation is returned, meaning "no worktree-specific choices have
// been recorded yet" — a perfectly normal state for a worktree that has
// never had `omca` activate anything locally. A present-but-invalid
// document is an error naming path and the offending field (issue #16 AC).
func LoadActivation(path string) (domain.Activation, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return domain.Activation{}, false, nil
		}
		return domain.Activation{}, false, fmt.Errorf("profiles: %s: %w", path, err)
	}

	var a domain.Activation
	if err := decodeYAMLDocument(path, &a); err != nil {
		return domain.Activation{}, false, err
	}
	if err := domain.ValidateActivation(a); err != nil {
		return domain.Activation{}, false, fmt.Errorf("profiles: %s: %w", path, err)
	}
	return a, true, nil
}

// PersistActivation writes a to path (<worktree state dir>/desired/
// activation.yaml) as a schema-valid Activation document -- the write-side
// counterpart LoadActivation's own doc comment describes this file as
// needing but this package never previously implemented (activation.yaml
// was, until this function, read-only: authored by hand, or not at all).
//
// A caller (issue #35's TUI action layer, cmd/omca/mcp.go's compileFuncForMCP
// today only ever merges an ActivationSelection in memory for the ONE
// CompileRequest it is about to compile) that wants a host's Enable/Disable
// selection to survive across a LATER, independent profiles.Compose call
// (activate.go's composeFreshCompileRequest, which reads this exact file)
// needs this durably written first: without it, a generation staged with an
// in-memory-only merged Activation can never pass a later Activate call's
// own CAS check (activate.go's freshSourceDigest), which recomposes desired
// state fresh from disk and has no way to see a choice that was only ever
// held in one caller's memory.
//
// a is validated (domain.ValidateActivation) before writing — this function
// refuses to durably persist a document its own reader would then reject.
// The write is atomic (temp file + rename within the same directory),
// matching PersistSelection's identical discipline for worktree-local
// state.
//
// Encoded as JSON, not gopkg.in/yaml.v3's own Marshal: domain.Activation is
// a JSON-tagged struct (`json:"apiVersion"`, not `yaml:"apiVersion"`), and
// decodeYAMLDocument's own doc comment explains why a direct yaml.Marshal/
// Unmarshal round-trip would silently drop every field for exactly that
// reason. Valid JSON is valid YAML (a JSON document parses as a YAML flow
// mapping), so writing json.MarshalIndent's own output to activation.yaml
// round-trips correctly through decodeYAMLDocument's
// yaml.Unmarshal-into-generic-then-json.Marshal-then-json.Unmarshal path —
// the same round-trip idiom this package already establishes, applied in
// the write direction instead of the read direction.
func PersistActivation(worktreeStateDir string, a domain.Activation) error {
	if worktreeStateDir == "" {
		return fmt.Errorf("profiles: PersistActivation: worktreeStateDir is required")
	}
	if err := domain.ValidateActivation(a); err != nil {
		return fmt.Errorf("profiles: PersistActivation: refusing to persist an invalid Activation: %w", err)
	}

	desiredDir := filepath.Join(worktreeStateDir, "desired")
	if err := os.MkdirAll(desiredDir, 0o755); err != nil {
		return fmt.Errorf("profiles: PersistActivation: %w", err)
	}

	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("profiles: PersistActivation: %w", err)
	}
	data = append(data, '\n')

	finalPath := activationPath(worktreeStateDir)
	tmpPath := finalPath + fmt.Sprintf(".tmp-%d", os.Getpid())
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("profiles: PersistActivation: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("profiles: PersistActivation: %w", err)
	}
	return nil
}

// activationPath computes the activation.yaml path under worktreeStateDir's
// desired/ subdirectory -- the same path CompositionInput.ActivationPath
// callers already build by hand (compositionDirsFor's own siblings,
// cmd/omca/activate.go/cmd/omca/mcp.go, and this package's own mirror in
// internal/tui/actions.go), factored out here so PersistActivation and any
// future caller agree on exactly one path.
func activationPath(worktreeStateDir string) string {
	return filepath.Join(worktreeStateDir, "desired", "activation.yaml")
}
