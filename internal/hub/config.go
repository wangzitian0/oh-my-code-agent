// Package hub implements the OMCA resident harness hub: a local-first
// control plane daemon that multiplexes singleton MCP tool subprocesses,
// enforces global worker concurrency/rate-limits, arbitrates stateful storage,
// and serves multiple coding-agent hosts via stdio bridges, Unix domain sockets,
// and local SSE.
package hub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	// 1. Explicit profile requested
	if profileName != "" {
		if prof, ok := c.Profiles[profileName]; ok {
			if tc, ok := prof.Tools[serverName]; ok {
				return &ResolvedTool{
					InstanceKey: profileName + ":" + serverName,
					Config:      tc,
					Profile:     profileName,
					EnvFiles:    prof.EnvFiles,
				}, true
			}
		}
	}

	// 2. Auto-detect profile from CWD prefix match
	if cwd != "" {
		cleanCwd := filepath.Clean(cwd)
		var bestProfile string
		var bestLen int
		for pName, pCfg := range c.Profiles {
			for _, root := range pCfg.WorkspaceRoots {
				cleanRoot := filepath.Clean(root)
				if strings.HasPrefix(cleanCwd, cleanRoot) && len(cleanRoot) > bestLen {
					bestProfile = pName
					bestLen = len(cleanRoot)
				}
			}
		}
		if bestProfile != "" {
			pCfg := c.Profiles[bestProfile]
			if tc, ok := pCfg.Tools[serverName]; ok {
				return &ResolvedTool{
					InstanceKey: bestProfile + ":" + serverName,
					Config:      tc,
					Profile:     bestProfile,
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

	// 5. Fallback: check if ANY profile defines this server uniquely
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

	return nil, false
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
func LoadConfig(path string) (*Config, error) {
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
			return cfg, nil
		}
		return nil, fmt.Errorf("hub: read config %s: %w", path, err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("hub: parse config %s: %w", path, err)
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
	return cfg, nil
}
