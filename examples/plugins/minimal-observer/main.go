package main

import (
	"fmt"
	"os"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/transport"
)

// main is the whole job of a real, out-of-process adapter binary
// (docs/plugin/authoring-guide.md's "wire transport.Serve" step): build the
// adapter, declare its manifest, and hand both to transport.Serve against
// this process's own stdin/stdout. Nothing else about being a separate OS
// process is this binary's concern -- internal/plugin/transport owns the
// wire protocol.
func main() {
	adapter := NewAdapter()

	// This manifest's Hosts entry deliberately uses a host ID
	// ("demo-observer-host") outside internal/domain's closed canonical host
	// vocabulary (KnownHostIDs) on purpose: this example is not, and does
	// not claim to be, a real host adapter for any of the hosts that
	// registry already recognizes (docs/plugin/authoring-guide.md's "known
	// gaps" section explains why, and what a genuine third party does
	// differently). Because of that, this manifest intentionally never
	// passes through plugin.PluginManifest.Validate() or
	// plugin.Registry.Register() -- only through the transport handshake,
	// which does not validate it, and internal/plugin/conformance.Run,
	// which does not either.
	manifest := plugin.PluginManifest{
		AdapterID:       adapter.ID(),
		AdapterVersion:  "0.1.0",
		ContractVersion: plugin.ContractVersion,
		Hosts: []plugin.HostSelector{
			{HostID: "demo-observer-host", Surfaces: []string{"cli"}, VersionRange: ">=0.0.0"},
		},
	}

	if err := transport.Serve(os.Stdin, os.Stdout, manifest, adapter); err != nil {
		fmt.Fprintln(os.Stderr, "minimal-observer:", err)
		os.Exit(1)
	}
}
