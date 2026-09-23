package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/hub"
)

func runHub(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: omca hub <start|stop|status|bridge|serve|top|worker> [flags]")
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
	case "worker":
		return runHubWorker(stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "omca: unknown hub subcommand %q\nusage: omca hub <start|stop|status|bridge|serve|top|worker>\n", args[0])
		return 2
	}
}

func runHubStart(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("omca hub start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	daemon := fs.Bool("d", false, "run hub as background daemon")
	configPath := fs.String("config", "", "path to harness hub config")
	socketPath := fs.String("socket", "", "override unix socket path")
	acceptHandEdit := fs.Bool("accept-hand-edit", false, "start even if harness.json does not match its digest sidecar (records the mismatch as drift)")
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
		if *acceptHandEdit {
			cmdArgs = append(cmdArgs, "--accept-hand-edit")
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
	acceptHandEdit := fs.Bool("accept-hand-edit", false, "start even if harness.json does not match its digest sidecar (records the mismatch as drift)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, drift, err := hub.LoadConfig(*configPath, *acceptHandEdit)
	if err != nil {
		fmt.Fprintf(stderr, "omca: %v\n", err)
		return 1
	}
	if drift != nil {
		fmt.Fprintf(stderr, "omca: %s\n", drift)
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

	// The pid file is written and removed by hub.Start/hub.Close, which hold
	// an exclusive flock on it for this process's lifetime (internal/hub/
	// instancelock.go). Writing it here too was not merely redundant: the
	// deferred os.Remove would unlink the lock file while the lock was still
	// held, letting a second hub create a fresh file, lock that new inode,
	// and proceed to replace the socket — reintroducing the exact orphan the
	// lock exists to prevent.

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

	pidFile := hub.InstanceLockPath(sock)
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

	// Neither the socket nor the lock file is removed here.
	//
	// That pid file is now the instance lock (internal/hub/instancelock.go).
	// Unlinking it while the daemon still holds it — which is always, since
	// SIGTERM above is asynchronous and shutdown is not instant — lets the
	// next hub create and lock a fresh inode at the same path while the
	// dying one still holds the old one. Two holders, one path: exactly the
	// orphan wedge the lock was added to close, recreated by the tool meant
	// to clean up.
	//
	// The socket is the daemon's to remove for the same reason, and because
	// a stop that targeted an already-dead pid would otherwise unlink a
	// *different*, live daemon's socket. hub.Close removes both. If the
	// daemon died without cleaning up, the leftovers are inert: the next
	// Start's ProbeSocket finds the socket unreachable and replaces it, and
	// the lock file carries no lock.

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

	respBytes, err := sendHubAdminRequest(sock, "status", "")
	if err != nil {
		if *jsonOutput {
			fmt.Fprintln(stdout, `{"status": "stopped"}`)
		} else {
			fmt.Fprintf(stdout, "omca hub is NOT running (socket %s unreachable)\n", sock)
		}
		return 0
	}

	if *jsonOutput {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, respBytes, "", "  "); err == nil {
			fmt.Fprintln(stdout, pretty.String())
		} else {
			fmt.Fprintln(stdout, string(respBytes))
		}
		return 0
	}

	var snap hub.DashboardSnapshot
	if err := json.Unmarshal(respBytes, &snap); err != nil {
		fmt.Fprintf(stdout, "🟢 omca hub is RUNNING (socket: %s)\n", sock)
		return 0
	}

	fmt.Fprintf(stdout, "🟢 omca hub is RUNNING (socket: %s, uptime: %s, %d hosts, %d active workers)\n",
		sock, snap.Uptime.Round(time.Second), len(snap.Connected), len(snap.ActiveWorkers))

	if len(snap.Tools) > 0 {
		names := make([]string, 0, len(snap.Tools))
		for name := range snap.Tools {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(stdout, "%-30s %-12s %s\n", "TOOL", "STATUS", "SCOPE")
		for _, name := range names {
			ts := snap.Tools[name]
			scope := "profile"
			if ts.CrossProfile {
				// shared_tools entry (issue #124 contract #4): every
				// profile can resolve this one, so it is flagged rather
				// than shown identically to a profile-scoped tool.
				scope = "cross-profile (shared_tools)"
			}
			fmt.Fprintf(stdout, "%-30s %-12s %s\n", name, ts.Status, scope)
		}
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
	fs := flag.NewFlagSet("omca hub top", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPath := fs.String("socket", "", "override unix socket path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sock := *socketPath
	if sock == "" {
		sock = hub.DefaultSocketPath()
	}

	respBytes, err := sendHubAdminRequest(sock, "status", "")
	if err != nil {
		fmt.Fprintf(stderr, "omca hub top: hub is not running (%v)\nStart it with: omca hub start -d\n", err)
		return 1
	}

	var snap hub.DashboardSnapshot
	if err := json.Unmarshal(respBytes, &snap); err != nil {
		fmt.Fprintf(stderr, "omca hub top: decode status: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "┌─ omca Resident Harness Hub ──────────────────────────────────────┐")
	fmt.Fprintf(stdout, "│ Socket  : %-54s │\n", sock)
	fmt.Fprintf(stdout, "│ Uptime  : %-54s │\n", snap.Uptime.Round(time.Second))
	fmt.Fprintf(stdout, "│ Clients : %-54s │\n", fmt.Sprintf("%d connected hosts", len(snap.Connected)))
	fmt.Fprintln(stdout, "├──────────────────────────────────────────────────────────────────┤")
	fmt.Fprintf(stdout, "│ Execution Workers (GLM-5.3)   : [%s] %d/%d active (queued: %d)   │\n",
		renderProgressBar(int(snap.Workers.ActiveTasks), snap.Workers.MaxTasks, 5),
		snap.Workers.ActiveTasks, snap.Workers.MaxTasks, snap.Workers.QueuedTasks)
	fmt.Fprintf(stdout, "│ Swarm Workers (GLM-5.3-Flash) : [%s] %d/%d active (queued: %d) │\n",
		renderProgressBar(int(snap.Workers.ActiveBatch), snap.Workers.MaxBatch, 10),
		snap.Workers.ActiveBatch, snap.Workers.MaxBatch, snap.Workers.QueuedBatch)
	fmt.Fprintf(stdout, "│ Rate Limiter (Prevented 429)  : %-32d │\n", snap.Workers.Prevented429)
	fmt.Fprintf(stdout, "│ Reaped Subprocesses (5m idle) : %-32d │\n", snap.ReapedCount)
	fmt.Fprintln(stdout, "├──────────────────────────────────────────────────────────────────┤")
	fmt.Fprintf(stdout, "│ Active Workers: %-48d │\n", len(snap.ActiveWorkers))
	if len(snap.ActiveWorkers) > 0 {
		for _, w := range snap.ActiveWorkers {
			age := time.Since(w.StartTime).Round(time.Second)
			fmt.Fprintf(stdout, "│  • %-20s %-6s %-12s %-6s %-20s │\n",
				w.ID, w.Type, w.Model, age, w.Phase)
		}
	} else {
		fmt.Fprintln(stdout, "│  (no active workers running)                                     │")
	}
	fmt.Fprintln(stdout, "└──────────────────────────────────────────────────────────────────┘")
	return 0
}

func runHubWorker(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: omca hub worker <list|kill> [flags]")
		return 2
	}

	switch args[0] {
	case "list", "ls":
		return runHubWorkerList(stdout, stderr, args[1:])
	case "kill", "rm":
		return runHubWorkerKill(stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "omca: unknown worker subcommand %q\nusage: omca hub worker <list|kill>\n", args[0])
		return 2
	}
}

func runHubWorkerList(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("omca hub worker list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	socketPath := fs.String("socket", "", "override unix socket path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sock := *socketPath
	if sock == "" {
		sock = hub.DefaultSocketPath()
	}

	respBytes, err := sendHubAdminRequest(sock, "worker_list", "")
	if err != nil {
		fmt.Fprintf(stderr, "omca hub: hub daemon is not running (%v)\n", err)
		return 1
	}

	if *jsonOutput {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, respBytes, "", "  "); err == nil {
			fmt.Fprintln(stdout, pretty.String())
		} else {
			fmt.Fprintln(stdout, string(respBytes))
		}
		return 0
	}

	var workers []hub.ActiveWorker
	if err := json.Unmarshal(respBytes, &workers); err != nil {
		fmt.Fprintf(stderr, "omca hub: decode worker list: %v\n", err)
		return 1
	}

	if len(workers) == 0 {
		fmt.Fprintln(stdout, "No active workers currently executing.")
		return 0
	}

	fmt.Fprintf(stdout, "%-24s %-6s %-16s %-12s %-8s %-10s %-10s %s\n",
		"WORKER ID", "TYPE", "MODEL", "HOST", "AGE", "HEARTBEAT", "PHASE", "PREVIEW")
	for _, w := range workers {
		age := time.Since(w.StartTime).Round(time.Second)
		hb := time.Since(w.LastHeartbeat).Round(time.Second).String() + " ago"
		fmt.Fprintf(stdout, "%-24s %-6s %-16s %-12s %-8s %-10s %-10s %s\n",
			w.ID, w.Type, w.Model, w.HostName, age, hb, w.Phase, w.PromptPreview)
	}
	return 0
}

func runHubWorkerKill(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("omca hub worker kill", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPath := fs.String("socket", "", "override unix socket path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sock := *socketPath
	if sock == "" {
		sock = hub.DefaultSocketPath()
	}

	if fs.NArg() == 0 {
		fmt.Fprintln(stderr, "error: worker ID required (usage: omca hub worker kill <worker-id>)")
		return 2
	}
	targetID := fs.Arg(0)

	respBytes, err := sendHubAdminRequest(sock, "worker_kill", targetID)
	if err != nil {
		fmt.Fprintf(stderr, "omca hub: hub daemon is not running (%v)\n", err)
		return 1
	}

	var res struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Killed  string `json:"killed"`
	}
	_ = json.Unmarshal(respBytes, &res)

	if res.Status == "ok" {
		fmt.Fprintf(stdout, "Worker %q terminated successfully.\n", targetID)
		return 0
	}
	fmt.Fprintf(stderr, "omca hub: failed to kill worker: %s\n", res.Message)
	return 1
}

func sendHubAdminRequest(socketPath string, action string, targetID string) ([]byte, error) {
	if socketPath == "" {
		socketPath = hub.DefaultSocketPath()
	}
	conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	req := hub.AttachMessage{
		Type:     "admin_request",
		Action:   action,
		TargetID: targetID,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	return reader.ReadBytes('\n')
}

func renderProgressBar(current, maxVal, width int) string {
	if maxVal <= 0 {
		maxVal = 1
	}
	filled := (current * width) / maxVal
	if filled > width {
		filled = width
	}
	var b strings.Builder
	for i := 0; i < filled; i++ {
		b.WriteString("■")
	}
	for i := filled; i < width; i++ {
		b.WriteString("□")
	}
	return b.String()
}
