// Command omca is the entry point for the oh-my-code-agent control plane.
package main

import (
	"fmt"
	"os"

	"github.com/wangzitian0/oh-my-code-agent/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: omca <command>")
		return 2
	}

	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version.String())
		return 0
	default:
		fmt.Fprintf(stderr, "omca: unknown command %q\n", args[0])
		return 2
	}
}
