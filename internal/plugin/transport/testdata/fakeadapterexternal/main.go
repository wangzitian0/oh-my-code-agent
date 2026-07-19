// Command fakeadapterexternal is a test-only fixture binary standing in for
// a genuine third-party, out-of-process host adapter plugin. It is never
// invoked by anything except internal/plugin/transport's own tests, and it
// is built explicitly by those tests (`go build ./testdata/fakeadapterexternal`)
// — this directory is named testdata precisely so `go build ./...`/
// `go vet ./...`/`golangci-lint run ./...` at the repository root skip it
// (the same convention cmd/omca/testdata/fakehost's own doc comment
// documents), keeping this fixture out of every real build and never
// exposing it as a real `omca` subcommand.
//
// Its whole job is to wrap internal/plugin/conformance.FakeAdapter — the
// exact reference in-process implementation
// internal/plugin/conformance.Run is already proven against — behind
// internal/plugin/transport.Serve, so a test can launch this binary as a
// real OS subprocess, drive it through transport.RemoteAdapter, and run the
// SAME conformance.Run suite against it: proof that an external adapter
// binary speaking the v1 contract over stdio passes the identical
// conformance suite as an in-process adapter (issue #29's first acceptance
// criterion), using no new test suite of its own.
package main

import (
	"fmt"
	"os"

	"github.com/wangzitian0/oh-my-code-agent/internal/plugin"
	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/conformance"
	"github.com/wangzitian0/oh-my-code-agent/internal/plugin/transport"
)

func main() {
	adapter := conformance.NewFakeAdapter()

	// This manifest deliberately matches FakeAdapter's own fixed HostInstance
	// (conformance/fake.go's NewFakeAdapter: HostID "codex", Surface "cli"),
	// since a manifest declaring a host the adapter itself never detects
	// would be internally inconsistent for no reason a real third-party
	// plugin author would ever have.
	manifest := plugin.PluginManifest{
		AdapterID:       adapter.ID(),
		AdapterVersion:  "0.0.0-test",
		ContractVersion: plugin.ContractVersion,
		Hosts: []plugin.HostSelector{
			{HostID: "codex", Surfaces: []string{"cli"}, VersionRange: "0.144.0"},
		},
	}

	if err := transport.Serve(os.Stdin, os.Stdout, manifest, adapter); err != nil {
		fmt.Fprintln(os.Stderr, "fakeadapterexternal:", err)
		os.Exit(1)
	}
}
