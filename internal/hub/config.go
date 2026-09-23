// Package hub implements the OMCA resident harness hub: a local-first
// control plane daemon that multiplexes singleton MCP tool subprocesses,
// enforces global worker concurrency/rate-limits, arbitrates stateful storage,
// and serves multiple coding-agent hosts via stdio bridges, Unix domain sockets,
// and local SSE.
package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultSocketRelPath is the location of the resident Hub Unix domain socket
// relative to the user's home directory.
const DefaultSocketRelPath = ".omca/run/hub.sock"

// DefaultConfigRelPath is the location of the hub configuration file.
const DefaultConfigRelPath = ".omca/harness.json"

// ToolConfig defines a single resident MCP tool process managed by the hub.
type ToolConfig struct {
	Name       string            `json:"name"`
	Command    string            `json:"command"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	WorkingDir string            `json:"working_dir,omitempty"`
	// SharedAllow must be true for any tool listed under Config.SharedTools.
	// shared_tools is empty by default (issue #124 contract #4): it is the
	// cross-profile pool every profile can resolve (see ResolveServer), so
	// landing a tool there is a deliberate, explicit, per-tool opt-in, never
	// a side effect of merely listing it. LoadConfig refuses to load a
	// shared_tools entry that has not set this -- see
	// Config.validateSharedToolsAllowList.
	SharedAllow bool `json:"shared_allow,omitempty"`
}

// ProfileConfig defines workspace-scoped tool definitions and configuration.
type ProfileConfig struct {
	WorkspaceRoots []string              `json:"workspace_roots,omitempty"`
	EnvFiles       []string              `json:"env_files,omitempty"`
	Tools          map[string]ToolConfig `json:"tools,omitempty"`
}

// Config defines the complete harness hub configuration.
type Config struct {
	SocketPath  string                   `json:"socket_path"`
	Port        int                      `json:"port,omitempty"`         // Optional HTTP/SSE port (e.g. 8765)
	IdleTimeout string                   `json:"idle_timeout,omitempty"` // e.g. "5m", defaults to 5m
	SharedTools map[string]ToolConfig    `json:"shared_tools,omitempty"`
	Profiles    map[string]ProfileConfig `json:"profiles,omitempty"`
	Tools       map[string]ToolConfig    `json:"tools,omitempty"` // Legacy backward-compatibility
}

// GetIdleTimeout returns the parsed idle timeout duration, defaulting to 5 minutes.
func (c *Config) GetIdleTimeout() time.Duration {
	if c.IdleTimeout == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(c.IdleTimeout)
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

// ResolvedTool describes the resolution of a target tool request.
type ResolvedTool struct {
	InstanceKey string
	Config      ToolConfig
	Profile     string
	EnvFiles    []string
}

// ResolveServer resolves a tool request given an optional explicit profile,
// the caller's working directory, and the target server name.
func (c *Config) ResolveServer(profileName, cwd, serverName string) (*ResolvedTool, bool) {
	if serverName == "" {
		return nil, false
	}
	// Sanitize serverName: reject control characters, null bytes, and path traversals
	if strings.ContainsAny(serverName, "\r\n\t\x00/\\") {
		return nil, false
	}
	serverName = strings.TrimSpace(serverName)

	// 1. Explicit profile requested
	if profileName != "" {
		// profile = workspace is the only mapping (issue #124 contract #2):
		// profiles.<name>.workspace_roots is a workspace's identity, so an
		// explicit -profile that does not name a declared, non-empty root
		// is refused outright here -- it must NOT fall through to the
		// shared-tools branch below, which would silently resolve a typo'd
		// or undeclared profile name against the cross-profile pool instead
		// of reporting it as the invalid profile it is. Auto-detect from
		// CWD (branch 2 below) is unaffected; this check only applies when
		// the caller explicitly named a profile.
		prof, ok := c.Profiles[profileName]
		if !ok || len(prof.WorkspaceRoots) == 0 {
			return nil, false
		}
		if tc, ok := prof.Tools[serverName]; ok {
			return &ResolvedTool{
				InstanceKey: profileName + ":" + serverName,
				Config:      tc,
				Profile:     profileName,
				EnvFiles:    prof.EnvFiles,
			}, true
		}
		// Fallback to shared tools, but strictly never cross-leak to other profiles
		if tc, ok := c.SharedTools[serverName]; ok {
			return &ResolvedTool{
				InstanceKey: "shared:" + serverName,
				Config:      tc,
				Profile:     "shared",
			}, true
		}
		return nil, false
	}

	// 2. Auto-detect profile from CWD prefix match
	var matchedProfile string
	if cwd != "" {
		cleanCwd := filepath.Clean(cwd)
		var bestLen int
		for pName, pCfg := range c.Profiles {
			for _, root := range pCfg.WorkspaceRoots {
				cleanRoot := filepath.Clean(root)
				if strings.HasPrefix(cleanCwd, cleanRoot) && len(cleanRoot) > bestLen {
					matchedProfile = pName
					bestLen = len(cleanRoot)
				}
			}
		}
		if matchedProfile != "" {
			pCfg := c.Profiles[matchedProfile]
			if tc, ok := pCfg.Tools[serverName]; ok {
				return &ResolvedTool{
					InstanceKey: matchedProfile + ":" + serverName,
					Config:      tc,
					Profile:     matchedProfile,
					EnvFiles:    pCfg.EnvFiles,
				}, true
			}
		}
	}

	// 3. Check shared tools (global cross-profile pool)
	if tc, ok := c.SharedTools[serverName]; ok {
		return &ResolvedTool{
			InstanceKey: "shared:" + serverName,
			Config:      tc,
			Profile:     "shared",
		}, true
	}

	// 4. Check legacy top-level tools
	if tc, ok := c.Tools[serverName]; ok {
		return &ResolvedTool{
			InstanceKey: serverName,
			Config:      tc,
			Profile:     "",
		}, true
	}

	// 5. Fallback: ONLY if CWD did not match any declared profile
	if matchedProfile == "" {
		for pName, pCfg := range c.Profiles {
			if tc, ok := pCfg.Tools[serverName]; ok {
				return &ResolvedTool{
					InstanceKey: pName + ":" + serverName,
					Config:      tc,
					Profile:     pName,
					EnvFiles:    pCfg.EnvFiles,
				}, true
			}
		}
	}

	return nil, false
}

// secretLiteralSuffixes are the ToolConfig.Env key suffixes, checked
// case-insensitively, that mark a value as credential-shaped.
var secretLiteralSuffixes = []string{"KEY", "TOKEN", "SECRET", "PAT"}

// minSecretLiteralLen is the shortest value length treated as "long enough
// to be a real credential" rather than a short flag or placeholder.
const minSecretLiteralLen = 32

// looksLikeSecretLiteral reports whether an env key/value pair looks like a
// hand-embedded secret rather than an env_files reference (issue #124
// contract #3): the key name reads as credential-shaped, the value is long
// enough to be a real secret, and it is not a reference -- `$FOO`/`${FOO}`
// (env_files-backed, see vault.go ExpandEnv) or `op://...` (1Password,
// resolved outside OMCA).
func looksLikeSecretLiteral(key, value string) bool {
	upper := strings.ToUpper(key)
	nameMatches := false
	for _, suf := range secretLiteralSuffixes {
		if strings.HasSuffix(upper, suf) {
			nameMatches = true
			break
		}
	}
	if !nameMatches {
		return false
	}
	if len(value) < minSecretLiteralLen {
		return false
	}
	if strings.HasPrefix(value, "$") || strings.HasPrefix(value, "op://") {
		return false
	}
	return true
}

// validateSecretLiterals walks every ToolConfig.Env in the config and fails
// with the offending JSON path the moment it finds a literal secret.
// env_files is the only sanctioned path for a secret to reach a tool (issue
// #124 contract #3); this is the config-load-stage gate that refuses to
// start a worker with one embedded in harness.json instead.
//
// Traversal is over sorted keys so the reported path -- and which one of
// several violations is reported first -- is deterministic, not dependent
// on Go's randomized map iteration order.
func (c *Config) validateSecretLiterals() error {
	check := func(pathPrefix string, env map[string]string) error {
		for _, k := range sortedStringKeys(env) {
			v := env[k]
			if looksLikeSecretLiteral(k, v) {
				return fmt.Errorf(
					"hub: config validation failed at %s.%s: value looks like a literal secret (%d chars, key ends in KEY/TOKEN/SECRET/PAT, and does not start with $ or op://); secrets must come from env_files, not env",
					pathPrefix, k, len(v),
				)
			}
		}
		return nil
	}

	for _, name := range sortedToolConfigKeys(c.Tools) {
		if err := check(fmt.Sprintf("tools.%s.env", name), c.Tools[name].Env); err != nil {
			return err
		}
	}
	for _, name := range sortedToolConfigKeys(c.SharedTools) {
		if err := check(fmt.Sprintf("shared_tools.%s.env", name), c.SharedTools[name].Env); err != nil {
			return err
		}
	}
	for _, pName := range sortedProfileConfigKeys(c.Profiles) {
		prof := c.Profiles[pName]
		for _, tName := range sortedToolConfigKeys(prof.Tools) {
			path := fmt.Sprintf("profiles.%s.tools.%s.env", pName, tName)
			if err := check(path, prof.Tools[tName].Env); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateSharedToolsAllowList fails config loading if any shared_tools
// entry has not explicitly opted into the cross-profile pool (issue #124
// contract #4). shared_tools is empty by default; every profile can reach
// whatever ends up in it (ResolveServer), so a tool lands there only by
// explicit, per-tool consent, never by merely being listed.
func (c *Config) validateSharedToolsAllowList() error {
	for _, name := range sortedToolConfigKeys(c.SharedTools) {
		if !c.SharedTools[name].SharedAllow {
			return fmt.Errorf(
				"hub: config validation failed at shared_tools.%s: shared_tools is empty by default; a listed tool must set \"shared_allow\": true to opt into the cross-profile pool",
				name,
			)
		}
	}
	return nil
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedToolConfigKeys(m map[string]ToolConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedProfileConfigKeys(m map[string]ProfileConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// digestSidecarSuffix names the sidecar LoadConfig checks harness.json
// against: <path>.sha256 next to <path>.
const digestSidecarSuffix = ".sha256"

func sidecarPath(path string) string {
	return path + digestSidecarSuffix
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// parseDigestSidecar extracts a sha256 hex digest from sidecar content,
// accepting both a bare digest and the "<digest>  <filename>" form standard
// `sha256sum`/`shasum -a 256` output uses.
func parseDigestSidecar(data []byte) string {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

// DriftRecord is returned by LoadConfig when --accept-hand-edit let a
// harness.json load despite not matching its digest sidecar. It is the
// "logs and reports drift" half of issue #124 contract #1: the override
// starts the hub, but never silently -- the caller is expected to log or
// otherwise surface this.
type DriftRecord struct {
	Path           string `json:"path"`
	SidecarPath    string `json:"sidecar_path"`
	RecordedDigest string `json:"recorded_digest"`
	ActualDigest   string `json:"actual_digest"`
}

func (d *DriftRecord) String() string {
	return fmt.Sprintf(
		"drift accepted: %s no longer matches its digest sidecar %s (sidecar recorded %s, file is now %s) -- proceeding because --accept-hand-edit was passed",
		d.Path, d.SidecarPath, d.RecordedDigest, d.ActualDigest,
	)
}

// DigestMismatchError is returned by LoadConfig when harness.json's content
// does not match its digest sidecar and acceptHandEdit was not set.
// harness.json is a rendered artifact (dev_env's ws-apply renders it); a
// mismatch means something else wrote to it after the sidecar was last
// established, most likely a hand edit.
type DigestMismatchError struct {
	Path           string
	SidecarPath    string
	RecordedDigest string
	ActualDigest   string
}

func (e *DigestMismatchError) Error() string {
	return fmt.Sprintf(
		"hub: refusing to start: %s does not match its digest sidecar %s (sidecar recorded %s, file is now %s); harness.json is a rendered artifact and this looks like a hand edit. Re-render it, or pass --accept-hand-edit to start anyway and record the drift",
		e.Path, e.SidecarPath, e.RecordedDigest, e.ActualDigest,
	)
}

// checkDigestSidecar enforces issue #124 contract #1: harness.json is a
// rendered artifact, and a content change since the last known-good load is
// refused unless acceptHandEdit is set. A missing sidecar is trust-on-
// first-use, not a refusal -- there is nothing yet to have drifted from --
// and this call establishes that baseline by writing one, the same way a
// render pipeline would alongside the file it just rendered.
//
// Returns a non-nil *DriftRecord only when a mismatch was found and
// accepted via acceptHandEdit; the caller is expected to log/report it. On
// acceptance the sidecar is rewritten to match, so the operator's explicit
// override is not re-demanded on every subsequent, unchanged restart.
func checkDigestSidecar(path string, data []byte, acceptHandEdit bool) (*DriftRecord, error) {
	scPath := sidecarPath(path)
	actual := sha256Hex(data)

	sidecarData, err := os.ReadFile(scPath)
	switch {
	case err == nil:
		recorded := parseDigestSidecar(sidecarData)
		if recorded == actual {
			return nil, nil
		}
		if !acceptHandEdit {
			return nil, &DigestMismatchError{
				Path:           path,
				SidecarPath:    scPath,
				RecordedDigest: recorded,
				ActualDigest:   actual,
			}
		}
		drift := &DriftRecord{
			Path:           path,
			SidecarPath:    scPath,
			RecordedDigest: recorded,
			ActualDigest:   actual,
		}
		if werr := os.WriteFile(scPath, []byte(actual+"\n"), 0600); werr != nil {
			return nil, fmt.Errorf("hub: accept-hand-edit: rewrite digest sidecar %s: %w", scPath, werr)
		}
		return drift, nil

	case os.IsNotExist(err):
		if werr := os.WriteFile(scPath, []byte(actual+"\n"), 0600); werr != nil {
			return nil, fmt.Errorf("hub: write digest sidecar %s: %w", scPath, werr)
		}
		return nil, nil

	default:
		return nil, fmt.Errorf("hub: read digest sidecar %s: %w", scPath, err)
	}
}

// DefaultSocketPath returns the standard Unix socket path for the hub.
func DefaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "omca-hub.sock")
	}
	return filepath.Join(home, DefaultSocketRelPath)
}

// DefaultConfigPath returns the standard configuration file path.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "omca-harness.json")
	}
	return filepath.Join(home, DefaultConfigRelPath)
}

// LoadConfig loads the hub configuration from the specified path, or returns
// a sensible default configuration if the file does not exist.
//
// harness.json is a rendered artifact (issue #124): a loaded config is
// validated before it is trusted to start anything --
//   - no ToolConfig.Env value may look like a literal secret
//     (validateSecretLiterals; contract #3);
//   - every shared_tools entry must carry shared_allow: true
//     (validateSharedToolsAllowList; contract #4);
//   - the file's content must match its digest sidecar, i.e. it has not
//     been hand-edited since the last known-good load (checkDigestSidecar;
//     contract #1).
//
// acceptHandEdit bypasses only the last of those, and its bypass is
// reported back as a non-nil *DriftRecord for the caller to log; it never
// bypasses content validation. profile = workspace validation (contract #2)
// lives in ResolveServer, not here, because it is a property of one
// resolution request's explicit -profile, not of the config file itself.
func LoadConfig(path string, acceptHandEdit bool) (*Config, *DriftRecord, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	cfg := &Config{
		SocketPath:  DefaultSocketPath(),
		Port:        8765,
		IdleTimeout: "5m",
		SharedTools: make(map[string]ToolConfig),
		Profiles:    make(map[string]ProfileConfig),
		Tools:       make(map[string]ToolConfig),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil, nil
		}
		return nil, nil, fmt.Errorf("hub: read config %s: %w", path, err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, nil, fmt.Errorf("hub: parse config %s: %w", path, err)
	}

	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath()
	}
	if cfg.IdleTimeout == "" {
		cfg.IdleTimeout = "5m"
	}
	if cfg.SharedTools == nil {
		cfg.SharedTools = make(map[string]ToolConfig)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]ProfileConfig)
	}
	if cfg.Tools == nil {
		cfg.Tools = make(map[string]ToolConfig)
	}

	if err := cfg.validateSecretLiterals(); err != nil {
		return nil, nil, err
	}
	if err := cfg.validateSharedToolsAllowList(); err != nil {
		return nil, nil, err
	}

	drift, err := checkDigestSidecar(path, data, acceptHandEdit)
	if err != nil {
		return nil, nil, err
	}

	return cfg, drift, nil
}
