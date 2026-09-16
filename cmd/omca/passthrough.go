package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/passthrough"
)

func passthroughPolicyPath(root string) (string, error) {
	directory := filepath.Join(root, ".omca")
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return "", os.ErrNotExist
	}
	if err != nil || !info.IsDir() {
		return "", errors.New("policy directory must be a real directory")
	}
	return filepath.Join(directory, "passthrough.yaml"), nil
}

func runPassthrough(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 || args[0] != "preview" {
		fmt.Fprintln(stderr, "usage: omca passthrough preview [--file policy.yaml]")
		return 2
	}
	flags := flag.NewFlagSet("passthrough preview", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // invalid arguments can contain accidental values
	file := flags.String("file", "", "explicit candidate policy")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: omca passthrough preview [--file policy.yaml]")
		return 2
	}
	if *file == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "omca: cannot locate worktree")
			return 1
		}
		wt, err := hostcontext.DetectWorktree(cwd)
		if err != nil {
			fmt.Fprintln(stderr, "omca: cannot locate worktree")
			return 1
		}
		path, err := passthroughPolicyPath(wt.Root)
		if err != nil {
			fmt.Fprintln(stderr, "omca: passthrough policy unavailable")
			return 1
		}
		*file = path
	}
	policy, err := passthrough.Read(*file)
	if err != nil {
		fmt.Fprintln(stderr, "omca: passthrough:", err)
		return 1
	}
	preview := passthrough.Inspect(policy, func(name string) bool { return os.Getenv(name) != "" })
	if err := json.NewEncoder(stdout).Encode(preview); err != nil {
		fmt.Fprintln(stderr, "omca: cannot write passthrough preview")
		return 1
	}
	return 0
}

func checkPassthrough(root string, env hostcontext.Environment) doctorFinding {
	finding := doctorFinding{Check: "passthrough", Status: statusWarn}
	path, err := passthroughPolicyPath(root)
	if err == nil {
		var policy passthrough.Policy
		policy, err = passthrough.Read(path)
		if err == nil {
			finding.Detail = passthrough.Inspect(policy, func(name string) bool { return env.Get(name) != "" }).Summary()
			return finding
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		finding.Detail = "no candidate policy; no passthrough qualification evidence"
		return finding
	}
	finding.Status = statusFail
	finding.Detail = "invalid candidate policy: " + err.Error()
	return finding
}
