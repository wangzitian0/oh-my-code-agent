// Command fakesigint is a test-only fixture binary (see testdata/fakehost's
// doc comment for why this directory is excluded from every real build/
// vet/lint pass) that blocks until it receives SIGINT, then exits with a
// fixed, distinctive code — the literal fixture issue #14's acceptance
// criteria describe: "a fake binary that traps/reports receipt of a signal
// before exiting... so the test can send SIGINT and observe the exit
// code/signal." Its exit code (42) is arbitrary but distinctive: a passing
// test proves the sender's SIGINT actually reached and was handled by
// *this* program image, not some other exit path (e.g. the default
// SIGINT-terminates action, which would exit via a different mechanism
// entirely, or a shim bug that left the exit code unrelated to this).
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ch := make(chan os.Signal, 1)
	// Install the handler before printing READY, so a test that only sends
	// SIGINT after observing READY on stdout can never race ahead of this
	// registration.
	signal.Notify(ch, syscall.SIGINT)
	fmt.Println("READY")
	<-ch
	os.Exit(42)
}
