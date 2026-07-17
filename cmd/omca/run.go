package main

import (
	stdcontext "context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
	"github.com/wangzitian0/oh-my-code-agent/internal/shim"
)

// runArgs is `omca run`'s parsed command line.
type runArgs struct {
	Mode        string // "isolated" (default) or "native"
	Host        string // as typed: "codex", "claude", or "claude-code"
	Passthrough []string
}

// parseRunArgs accepts `--mode isolated|native` (either `--mode X` or
// `--mode=X`) in any position relative to the host argument, matching both
// this issue's own synopsis ("omca run [--mode isolated|native] [<host>]")
// and docs/architecture/runtime.md §11's examples ("omca run codex --mode
// isolated"). A literal "--" stops flag parsing and forwards everything
// after it verbatim; without one, any argument after the host is still
// forwarded (a single positional host is unambiguous once seen, so nothing
// is lost by not requiring "--" for the common case).
func parseRunArgs(args []string) (runArgs, error) {
	out := runArgs{Mode: "isolated"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			out.Passthrough = append(out.Passthrough, args[i+1:]...)
			return out, nil
		case a == "--mode":
			if i+1 >= len(args) {
				return runArgs{}, fmt.Errorf("--mode requires a value (isolated or native)")
			}
			out.Mode = args[i+1]
			i++
		case strings.HasPrefix(a, "--mode="):
			out.Mode = strings.TrimPrefix(a, "--mode=")
		case out.Host == "" && !strings.HasPrefix(a, "-"):
			out.Host = a
		default:
			out.Passthrough = append(out.Passthrough, a)
		}
	}
	return out, nil
}

// normalizeHostArg maps a user-typed `omca run` host argument to a
// canonical host ID: "codex" as-is, "claude" (the binary name) as a
// friendlier alias for "claude-code" (the canonical ID every other package
// in this module uses), and "claude-code" as-is. Anything else is a clear
// error rather than a silent no-op.
func normalizeHostArg(arg string) (string, error) {
	switch arg {
	case "":
		return "", fmt.Errorf("a host is required: omca run [--mode isolated|native] <codex|claude>")
	case "codex", "claude-code":
		return arg, nil
	case "claude":
		return "claude-code", nil
	default:
		return "", fmt.Errorf("unrecognized host %q (want codex or claude)", arg)
	}
}

// runRun implements `omca run [--mode isolated|native] <host>`
// (docs/architecture/runtime.md §11, issue #14's "the direct CLI-level
// equivalent for one-shot use without direnv"). On the success path for
// either mode, this function's own process image is replaced by the target
// host binary (internal/shim.ExecReplace/syscall.Exec) and never returns to
// its caller; stdout/stderr are still accepted as parameters so the
// pre-exec error/diagnostic paths stay testable without a subprocess.
func runRun(stdout, stderr io.Writer, args []string) int {
	parsed, err := parseRunArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 2
	}
	host, err := normalizeHostArg(parsed.Host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 2
	}
	binName, err := hostcontext.BinaryName(host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 2
	}

	realEnv := hostcontext.RealEnvironment()

	switch parsed.Mode {
	case "native":
		return runNative(stderr, host, binName, realEnv, parsed.Passthrough)
	case "isolated":
		return runIsolated(stderr, host, realEnv, parsed.Passthrough)
	default:
		fmt.Fprintf(stderr, "omca: run: --mode %q is not recognized (want isolated or native)\n", parsed.Mode)
		return 2
	}
}

// runNative is `--mode native`: docs/architecture/runtime.md §11's
// "explicit diagnostic baseline" and issue #14's literal AC, "prints an
// explicit unmanaged warning" before running the host's plain native binary
// with the calling process's ambient environment completely unmodified —
// no generation, no CODEX_HOME/CLAUDE_CONFIG_DIR override, nothing.
//
// It still resolves the real binary through internal/shim.ResolveReal with
// whatever shim directory is currently on PATH filtered out, rather than a
// bare exec.LookPath: without that, running `omca run --mode native codex`
// from inside an already-managed shell (OMCA_SHIM_DIR first on PATH, the
// normal steady state) would resolve back to the OMCA shim itself —
// silently defeating the entire point of asking for the native, unmanaged
// binary.
func runNative(stderr io.Writer, host, binName string, env hostcontext.Environment, passthrough []string) int {
	fmt.Fprintf(stderr, "omca: run: WARNING — running %s in UNMANAGED native mode: no generation is applied, and native user-global Instructions, Skills, MCP servers, Hooks, and Plugins may load (docs/architecture/runtime.md §11 diagnostic baseline).\n", host)

	shimDir := env.Get("OMCA_SHIM_DIR")
	realPath, err := shim.ResolveReal(binName, env.Get("PATH"), shimDir)
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 1
	}

	argv := append([]string{realPath}, passthrough...)
	if err := shim.ExecReplace(realPath, argv, os.Environ()); err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 1
	}
	return 0 // unreachable on success
}

// runIsolated is `--mode isolated` (the default): the managed path, sharing
// the exact same generation-selection/compile pipeline `omca env` uses
// (detect worktree + host, observe, runtime.EnsureGeneration,
// runtime.SetCurrentGeneration), but instead of printing exports it
// directly execs the target host with the generation environment injected
// — the same internal/shim.ExecReplace discipline the PATH shim itself
// uses, reused here rather than duplicated.
//
// The exec'd environment carries OMCA_STATE_DIR and OMCA_WORKTREE_ID (in
// addition to the native-home variable and OMCA_RUN_ID this function always
// set) precisely because the compiled generation now registers `omca mcp
// serve` as an MCP server (internal/runtime/compile.go's hostConfigFiles,
// PR-11/issue #15): if the host actually launches that registration as a
// subprocess, cmd/omca/mcp.go's runMCP reads exactly these two variables
// from its own (inherited) environment to answer omca_status, and
// internal/mcp.ComputeStatus hard-fails without OMCA_STATE_DIR. Before this
// fix, a session launched via `omca run <host>` — the documented "one-shot
// use without direnv" path, docs/architecture/runtime.md §11 — silently
// could never answer omca_status at all, since these variables are only
// otherwise present when a shell has separately run `omca env`
// (a Copilot-review-equivalent finding on this PR's own review pass).
// OMCA_CONTEXT_ID is deliberately left unset here: computing the same
// value `omca env` would (cmd/omca/env.go's computeContextID) requires
// detecting every DetectedHostIDs entry, not just the one host `omca run`
// targets, and StatusResult.ContextID is documented as optional/best-effort
// for exactly this reason.
//
// The exec'd environment also carries HOME (set to the compiled
// generation's virtual-home directory) and OMCA_REAL_HOME (set to the
// caller's real HOME) — docs/architecture/runtime.md §7.1's full documented
// env set. This closes a real isolation gap this project had until this
// fix: the native-home variable alone (CODEX_HOME/CLAUDE_CONFIG_DIR) never
// stopped a host from resolving its own "$HOME/.agents/skills" native Skill
// directory (internal/context/host.go's codexNativeHomes/claudeNativeHomes
// both append that entry independent of either override), so a real,
// unmanaged, unselected Skill source still loaded on every `omca run`
// launch — see TestRunIsolated_EndToEnd_VirtualizesHome (run_exec_test.go)
// for the regression proof.
func runIsolated(stderr io.Writer, host string, realEnv hostcontext.Environment, passthrough []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 1
	}
	wt, err := hostcontext.DetectWorktree(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 1
	}

	stateRoot, err := realStateRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 1
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)

	// installShims (not just shimDirPath's path arithmetic) must actually
	// run here, not only in `omca env`: the compiled generation's MCP
	// registration below points at shimDir/omca, and that entry must exist
	// and resolve to a real, executable omca binary for a host that
	// launches it to succeed — `omca run` is the documented direnv-free
	// path, so it cannot assume a prior `omca env` call ever populated
	// shimDir.
	if err := installShims(shimDir); err != nil {
		fmt.Fprintf(stderr, "omca: run: installing PATH shims: %v\n", err)
		return 1
	}

	detectEnv := envWithFilteredPath(realEnv, shimDir)
	hd, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 1
	}
	if !hd.Installed {
		fmt.Fprintf(stderr, "omca: run: %s is not installed\n", host)
		return 1
	}

	obs, err := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: wt.Root})
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: observing %s: %v\n", host, err)
		return 1
	}

	now := time.Now()
	req := runtime.BootstrapRequest{Detection: hd, Worktree: wt, Observations: obs, Now: now, OMCABinaryPath: omcaCommandPath(shimDir)}
	gen, outputDir, err := runtime.EnsureGeneration(req, filepath.Join(worktreeStateDir, "generations"))
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: compiling %s generation: %v\n", host, err)
		return 1
	}
	if err := runtime.SetCurrentGeneration(worktreeStateDir, host, outputDir, gen, hd, now); err != nil {
		fmt.Fprintf(stderr, "omca: run: recording current generation for %s: %v\n", host, err)
		return 1
	}
	fmt.Fprintf(stderr, "omca: run: %s\n", contextCostSummaryLine(host, gen))

	envVar, err := runtime.NativeHomeEnvVar(host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 1
	}
	homeDirName, err := runtime.NativeHomeDirName(host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 1
	}
	surface := hd.Surface
	if surface == "" {
		surface = "cli"
	}
	nativeHomeDir := filepath.Join(outputDir, "hosts", host, surface, homeDirName)
	// virtualHomeDir mirrors nativeHomeDir's own construction, pointing
	// instead at the empty, compiler-created directory (runtime.
	// VirtualHomeDirName, internal/runtime/compile.go) this generation's
	// EnsureGeneration call above already ensured exists. Setting HOME to it
	// (docs/architecture/runtime.md §7.1) is what actually stops a real host
	// binary from resolving its own native, unmanaged $HOME/.agents/skills
	// (internal/context/host.go's codexNativeHomes/claudeNativeHomes) --
	// nativeHomeDir/envVar alone (CODEX_HOME/CLAUDE_CONFIG_DIR) never
	// covered that path, since both host adapters compute it directly from
	// HOME regardless of either override. realEnv.Get("HOME") is guaranteed
	// non-empty and absolute here: hostcontext.DetectHost (called above, via
	// detectEnv which is realEnv with the shim dir filtered out of PATH)
	// already rejects a request whose Environment has no absolute HOME.
	virtualHomeDir := filepath.Join(outputDir, "hosts", host, surface, runtime.VirtualHomeDirName)
	// EnsureGeneration (above) returns a cache hit as soon as outputDir
	// already has a valid manifest.json — it never re-validates the rest of
	// the on-disk directory set against what the current compiler version
	// promises to have created. A generation directory compiled by an
	// older omca build, from before HOME virtualization existed, can still
	// be a perfectly valid cache hit by content address (nothing about the
	// desired-state inputs changed) while genuinely lacking virtualHomeDir
	// on disk. Blindly setting HOME to a path that does not exist would
	// silently reopen exactly the isolation gap this generation-compiler
	// change exists to close — some host binaries/libraries fall back to a
	// different, unmanaged home resolution when HOME points at something
	// invalid, rather than failing outright. Fail closed and loud instead.
	if info, statErr := os.Stat(virtualHomeDir); statErr != nil || !info.IsDir() {
		fmt.Fprintf(stderr, "omca: run: generation %s is missing its virtual-home directory (%s) -- it was likely compiled by an older omca build, before HOME isolation existed; delete %s and rerun to force a fresh compile\n", gen.Metadata.ID, virtualHomeDir, outputDir)
		return 1
	}

	overrides := map[string]string{
		envVar:             nativeHomeDir,
		"HOME":             virtualHomeDir,
		"OMCA_REAL_HOME":   realEnv.Get("HOME"),
		"OMCA_RUN_ID":      gen.Metadata.ID,
		"OMCA_STATE_DIR":   worktreeStateDir,
		"OMCA_WORKTREE_ID": wt.ID,
	}
	envp := shim.InjectEnv(os.Environ(), overrides)
	argv := append([]string{hd.BinaryPath}, passthrough...)
	if err := shim.ExecReplace(hd.BinaryPath, argv, envp); err != nil {
		fmt.Fprintf(stderr, "omca: run: %v\n", err)
		return 1
	}
	return 0 // unreachable on success
}
