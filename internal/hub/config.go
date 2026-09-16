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

// Config defines the complete harness hub configuration.
type Config struct {
	SocketPath string                `json:"socket_path"`
	Port       int                   `json:"port,omitempty"` // Optional HTTP/SSE port (e.g. 8765)
	Tools      map[string]ToolConfig `json:"tools"`
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
		SocketPath: DefaultSocketPath(),
		Port:       8765,
		Tools:      make(map[string]ToolConfig),
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
	return cfg, nil
}
