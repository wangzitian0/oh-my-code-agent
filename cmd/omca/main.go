// Command omca is the entry point for the oh-my-code-agent control plane.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/wangzitian0/oh-my-code-agent/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usage = "usage: omca <command>"

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}

	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version.String())
		return 0
	default:
		fmt.Fprintf(stderr, "omca: unknown command %q\n%s\n", args[0], usage)
		return 2
	}
}
