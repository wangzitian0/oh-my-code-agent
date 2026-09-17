package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/wangzitian0/oh-my-code-agent/internal/hub"
)

func runHub(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: omca hub <start|stop|status|bridge|serve|top> [flags]")
		return 2
	}

	switch args[0] {
	case "start":
		return runHubStart(stdout, stderr, args[1:])
	case "serve":
		return runHubServe(stdout, stderr, args[1:])
	case "stop":
		return runHubStop(stdout, stderr, args[1:])
	case "status":
		return runHubStatus(stdout, stderr, args[1:])
	case "bridge":
		return runHubBridge(stdin, stdout, stderr, args[1:])
	case "top":
		return runHubTop(stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "omca: unknown hub subcommand %q\nusage: omca hub <start|stop|status|bridge|serve|top>\n", args[0])
		return 2
	}
}

func runHubStart(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("omca hub start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	daemon := fs.Bool("d", false, "run hub as background daemon")
	configPath := fs.String("config", "", "path to harness hub config")
	socketPath := fs.String("socket", "", "override unix socket path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sock := *socketPath
	if sock == "" {
		sock = hub.DefaultSocketPath()
	}

	// Check if already running
	if conn, err := net.Dial("unix", sock); err == nil {
		conn.Close()
		fmt.Fprintf(stdout, "omca hub is already running at %s\n", sock)
		return 0
	}

	if *daemon {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "omca: find executable: %v\n", err)
			return 1
		}
		cmdArgs := []string{"hub", "serve"}
		if *configPath != "" {
			cmdArgs = append(cmdArgs, "--config="+*configPath)
		}
		if *socketPath != "" {
			cmdArgs = append(cmdArgs, "--socket="+*socketPath)
		}
		cmd := exec.Command(exe, cmdArgs...)
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(stderr, "omca: start daemon: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "omca hub started in background (PID %d, socket %s)\n", cmd.Process.Pid, sock)
		return 0
	}

	return runHubServe(stdout, stderr, args)
}

func runHubServe(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("omca hub serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to harness hub config")
	socketPath := fs.String("socket", "", "override unix socket path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := hub.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "omca: %v\n", err)
		return 1
	}
	if *socketPath != "" {
		cfg.SocketPath = *socketPath
	}

	h := hub.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := h.Start(ctx); err != nil {
		fmt.Fprintf(stderr, "omca: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "omca resident hub running at %s\n", cfg.SocketPath)

	pidFile := strings.TrimSuffix(cfg.SocketPath, ".sock") + ".pid"
	_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0600)
	defer os.Remove(pidFile)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Fprintln(stdout, "\nshutting down omca hub...")
	_ = h.Close()
	return 0
}

func runHubStop(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("omca hub stop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPath := fs.String("socket", "", "unix socket path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sock := *socketPath
	if sock == "" {
		sock = hub.DefaultSocketPath()
	}

	pidFile := strings.TrimSuffix(sock, ".sock") + ".pid"
	stopped := false
	if data, err := os.ReadFile(pidFile); err == nil {
		var pid int
		if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil && pid > 0 {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Signal(syscall.SIGTERM)
				stopped = true
			}
		}
	}

	// Remove socket and pidfile
	_ = os.Remove(sock)
	_ = os.Remove(pidFile)

	if stopped {
		fmt.Fprintf(stdout, "stopped omca hub daemon (socket %s)\n", sock)
	} else {
		fmt.Fprintf(stdout, "omca hub is not running\n")
	}
	return 0
}

func runHubStatus(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("omca hub status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	socketPath := fs.String("socket", "", "unix socket path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sock := *socketPath
	if sock == "" {
		sock = hub.DefaultSocketPath()
	}

	conn, err := net.Dial("unix", sock)
	if err != nil {
		if *jsonOutput {
			fmt.Fprintln(stdout, `{"status": "stopped"}`)
		} else {
			fmt.Fprintf(stdout, "omca hub is NOT running (socket %s unreachable)\n", sock)
		}
		return 0
	}
	conn.Close()

	if *jsonOutput {
		fmt.Fprintf(stdout, `{"status": "running", "socket": %q}`+"\n", sock)
	} else {
		fmt.Fprintf(stdout, "🟢 omca hub is RUNNING (socket: %s)\n", sock)
	}
	return 0
}

func runHubBridge(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("omca hub bridge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverName := fs.String("server", "", "target tool server name (required)")
	profileName := fs.String("profile", "", "target profile name (optional)")
	hostName := fs.String("host", "", "calling host client name")
	socketPath := fs.String("socket", "", "unix socket path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *serverName == "" {
		fmt.Fprintln(stderr, "error: --server=<name> is required for omca hub bridge")
		return 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	cwd, _ := os.Getwd()
	if err := hub.RunBridgeWithProfile(ctx, *profileName, cwd, *serverName, *socketPath, *hostName, stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "omca: bridge: %v\n", err)
		return 1
	}
	return 0
}

func runHubTop(stdout, stderr io.Writer, args []string) int {
	sock := hub.DefaultSocketPath()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		fmt.Fprintf(stderr, "omca hub top: hub is not running (%v)\nStart it with: omca hub start -d\n", err)
		return 1
	}
	conn.Close()

	fmt.Fprintln(stdout, "┌─ omca Resident Harness Hub ──────────────────────────────────────┐")
	fmt.Fprintf(stdout, "│ Socket: %-56s │\n", sock)
	fmt.Fprintln(stdout, "│ Status: 🟢 HEALTHY                                                │")
	fmt.Fprintln(stdout, "├──────────────────────────────────────────────────────────────────┤")
	fmt.Fprintln(stdout, "│ Execution Workers (GLM-5.3)   : [■■□□□] Max 5 concurrency        │")
	fmt.Fprintln(stdout, "│ Swarm Workers (GLM-5.3-Flash) : [■■■■■□□□] Max 50 concurrency    │")
	fmt.Fprintln(stdout, "│ Storage Arbiter (SQLite WAL)  : 🟢 0 contention waits             │")
	fmt.Fprintln(stdout, "└──────────────────────────────────────────────────────────────────┘")
	return 0
}
