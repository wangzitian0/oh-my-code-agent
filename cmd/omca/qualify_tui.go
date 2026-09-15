package main

import (
	"bufio"
	"bytes"
	stdcontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/qualify"
	"github.com/wangzitian0/oh-my-code-agent/internal/shim"
)

const (
	qualificationSentinel     = "omca-native-sentinel"
	qualificationManagedSkill = "omca-managed-sentinel"
	interactiveAckEnv         = "OMCA_QUALIFY_INTERACTIVE"
)

type tuiQualificationArgs struct {
	Host        string
	JSON        bool
	Keep        bool
	Interactive bool
}

type tuiQualificationCheck struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
	Detail   string `json:"detail"`
}

type tuiHostQualification struct {
	Host          string                  `json:"host"`
	Version       string                  `json:"version"`
	KnowledgePack string                  `json:"knowledgePack,omitempty"`
	Checks        []tuiQualificationCheck `json:"checks"`
	Complete      bool                    `json:"complete"`
}

type tuiQualificationArtifact struct {
	Kind                 string                 `json:"kind"`
	GeneratedAt          string                 `json:"generatedAt"`
	Hosts                []tuiHostQualification `json:"hosts"`
	RealNativeStateClean bool                   `json:"realNativeStateClean"`
	RealNativeStateDiffs []string               `json:"realNativeStateDiffs,omitempty"`
	InteractiveAttempted bool                   `json:"interactiveAttempted"`
	ScratchRetained      string                 `json:"scratchRetained,omitempty"`
	Complete             bool                   `json:"complete"`
}

type tuiQualificationRunner interface {
	Run(ctx stdcontext.Context, env []string, dir, host string, args []string, stdin string) (stdout, stderr string, exitCode int, err error)
	RunCodexSkills(ctx stdcontext.Context, env []string, dir string) (stdout, stderr string, exitCode int, err error)
}

type selfQualificationRunner struct {
	executable string
}

func (r selfQualificationRunner) Run(ctx stdcontext.Context, env []string, dir, host string, args []string, stdin string) (string, string, int, error) {
	argv := []string{"run", host, "--"}
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, r.executable, argv...)
	cmd.Dir = dir
	cmd.Env = append([]string{}, env...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	return stdout.String(), stderr.String(), exitCode, err
}

func (r selfQualificationRunner) RunCodexSkills(ctx stdcontext.Context, env []string, dir string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, r.executable, "run", "codex", "--", "app-server", "--listen", "stdio://")
	cmd.Dir = dir
	cmd.Env = append([]string{}, env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", "", -1, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", -1, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", stderr.String(), -1, err
	}

	decoder := json.NewDecoder(stdout)
	var captured bytes.Buffer
	writeRequest := func(request string) error {
		_, err := io.WriteString(stdin, request+"\n")
		return err
	}
	readUntilID := func(want float64) error {
		for {
			var message map[string]any
			if err := decoder.Decode(&message); err != nil {
				return err
			}
			encoded, err := json.Marshal(message)
			if err != nil {
				return err
			}
			captured.Write(encoded)
			captured.WriteByte('\n')
			if id, ok := message["id"].(float64); ok && id == want {
				return nil
			}
		}
	}

	protocolErr := writeRequest(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"omca-qualification","version":"1"}}}`)
	if protocolErr == nil {
		protocolErr = readUntilID(1)
	}
	if protocolErr == nil {
		// The public app-server protocol requires an initialized
		// notification after the initialize response and before ordinary
		// requests. Keep the qualification client conformant across every
		// host version this probe qualifies.
		protocolErr = writeRequest(`{"method":"initialized","params":{}}`)
	}
	if protocolErr == nil {
		request, err := json.Marshal(map[string]any{
			"id":     2,
			"method": "skills/list",
			"params": map[string]any{
				"cwds":        []string{dir},
				"forceReload": true,
			},
		})
		if err != nil {
			protocolErr = fmt.Errorf("encoding skills/list request: %w", err)
		} else {
			protocolErr = writeRequest(string(request))
		}
	}
	if protocolErr == nil {
		protocolErr = readUntilID(2)
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if protocolErr != nil {
		return captured.String(), stderr.String(), exitCode, fmt.Errorf("codex app-server protocol: %w", protocolErr)
	}
	return captured.String(), stderr.String(), exitCode, waitErr
}

func runQualify(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 || args[0] != "tui" {
		fmt.Fprintln(stderr, "omca: qualify: usage: omca qualify tui [--host codex|claude-code|all] [--json] [--interactive] [--keep]")
		return 2
	}
	parsed, err := parseTUIQualificationArgs(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "omca: qualify tui: %v\n", err)
		return 2
	}
	return runTUIQualification(stdout, stderr, parsed)
}

func parseTUIQualificationArgs(args []string) (tuiQualificationArgs, error) {
	parsed := tuiQualificationArgs{Host: "all"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			parsed.JSON = true
		case "--keep":
			parsed.Keep = true
		case "--interactive":
			parsed.Interactive = true
		case "--host":
			if i+1 >= len(args) {
				return tuiQualificationArgs{}, errors.New("--host requires a value")
			}
			i++
			parsed.Host = args[i]
		default:
			return tuiQualificationArgs{}, fmt.Errorf("unrecognized argument %q", args[i])
		}
	}
	if parsed.Host == "claude" {
		parsed.Host = "claude-code"
	}
	if parsed.Host != "all" && parsed.Host != "codex" && parsed.Host != "claude-code" {
		return tuiQualificationArgs{}, fmt.Errorf("unsupported host %q (want codex, claude-code, or all)", parsed.Host)
	}
	if parsed.Interactive && os.Getenv(interactiveAckEnv) != "1" {
		return tuiQualificationArgs{}, fmt.Errorf("--interactive starts a real host TUI and may use network/model quota; rerun with %s=1 after a human reviews the qualification instructions", interactiveAckEnv)
	}
	return parsed, nil
}

func qualificationHosts(host string) []string {
	if host == "all" {
		return []string{"codex", "claude-code"}
	}
	return []string{host}
}

func runTUIQualification(stdout, stderr io.Writer, args tuiQualificationArgs) int {
	if args.Interactive && !hasInteractiveTerminal() {
		fmt.Fprintln(stderr, "omca: qualify tui: --interactive requires a real terminal on stdin and stdout")
		return 1
	}

	realEnv := hostcontext.RealEnvironment()
	realHome := realEnv.Get("HOME")
	if realHome == "" || !filepath.IsAbs(realHome) {
		fmt.Fprintln(stderr, "omca: qualify tui: HOME must be an absolute path")
		return 1
	}

	scratch, err := os.MkdirTemp("", "omca-tui-qualification-")
	if err != nil {
		fmt.Fprintf(stderr, "omca: qualify tui: creating scratch lane: %v\n", err)
		return 1
	}
	keep := args.Keep
	if !keep {
		defer removeQualificationScratch(scratch)
	}
	if err := seedTUIQualificationScratch(scratch); err != nil {
		fmt.Fprintf(stderr, "omca: qualify tui: seeding scratch lane: %v\n", err)
		return 1
	}

	hosts := qualificationHosts(args.Host)
	paths := make([]string, 0)
	for _, host := range hosts {
		paths = append(paths, qualify.RealHomePaths(host, realHome)...)
	}
	paths = uniqueStrings(paths)
	before, err := qualify.SnapshotRealHome(paths)
	if err != nil {
		fmt.Fprintf(stderr, "omca: qualify tui: snapshotting real native state before probe: %v\n", err)
		return 1
	}

	probeEnv, detections, err := tuiQualificationEnvironment(scratch, hosts, realEnv)
	if err != nil {
		fmt.Fprintf(stderr, "omca: qualify tui: %v\n", err)
		return 1
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "omca: qualify tui: resolving omca executable: %v\n", err)
		return 1
	}
	runner := selfQualificationRunner{executable: executable}
	repo := filepath.Join(scratch, "repo")
	artifact := tuiQualificationArtifact{
		Kind:        "InteractiveTUIQualification",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, host := range hosts {
		result := probeTUIHost(runner, probeEnv, repo, detections[host])
		artifact.Hosts = append(artifact.Hosts, result)
	}

	if args.Interactive {
		artifact.InteractiveAttempted = true
		for i := range artifact.Hosts {
			host := artifact.Hosts[i].Host
			check := runHumanTUIQualification(probeEnv, repo, executable, host, stdout, stderr)
			applyHumanTUIQualification(&artifact.Hosts[i], check)
			artifact.Hosts[i].Checks = append(artifact.Hosts[i].Checks, check)
			artifact.Hosts[i].Complete = allQualificationChecksPass(artifact.Hosts[i].Checks)
		}
	} else {
		for i := range artifact.Hosts {
			artifact.Hosts[i].Checks = append(artifact.Hosts[i].Checks, pendingHumanTUIQualification(artifact.Hosts[i].Host))
			artifact.Hosts[i].Complete = false
		}
	}

	after, err := qualify.SnapshotRealHome(paths)
	if err != nil {
		fmt.Fprintf(stderr, "omca: qualify tui: snapshotting real native state after probe: %v\n", err)
		return 1
	}
	artifact.RealNativeStateDiffs = qualify.DiffRealHomeSnapshots(before, after)
	artifact.RealNativeStateClean = len(artifact.RealNativeStateDiffs) == 0
	if !artifact.RealNativeStateClean {
		for i := range artifact.Hosts {
			artifact.Hosts[i].Checks = append(artifact.Hosts[i].Checks, tuiQualificationCheck{
				ID: "real-native-state-zero-write", Status: "UNKNOWN", Evidence: "E0",
				Detail: "real native state changed during the qualification window; concurrent host activity prevents attributing the change to this probe",
			})
			artifact.Hosts[i].Complete = false
		}
	}
	artifact.Complete = artifact.RealNativeStateClean
	for _, host := range artifact.Hosts {
		artifact.Complete = artifact.Complete && host.Complete
	}
	if keep {
		artifact.ScratchRetained = scratch
	}

	if args.JSON {
		if err := json.NewEncoder(stdout).Encode(artifact); err != nil {
			fmt.Fprintf(stderr, "omca: qualify tui: writing JSON: %v\n", err)
			return 1
		}
	} else {
		renderTUIQualification(stdout, artifact)
	}
	if artifact.Complete {
		return 0
	}
	return 1
}

func seedTUIQualificationScratch(root string) error {
	dirs := []string{
		filepath.Join(root, "home", ".codex"),
		filepath.Join(root, "home", ".agents", "skills", qualificationSentinel),
		filepath.Join(root, "home", ".codex", "skills", qualificationSentinel),
		filepath.Join(root, "home", ".claude", "skills", qualificationSentinel),
		filepath.Join(root, "repo", ".agents", "skills", qualificationManagedSkill),
		filepath.Join(root, "repo", ".claude", "skills", qualificationManagedSkill),
		filepath.Join(root, "repo", ".git"),
		filepath.Join(root, "state"),
		filepath.Join(root, "config"),
		filepath.Join(root, "tmp"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	files := map[string]string{
		filepath.Join(root, "home", ".codex", "config.toml"):                                    "[mcp_servers." + qualificationSentinel + "]\ncommand = \"/usr/bin/false\"\n",
		filepath.Join(root, "home", ".claude.json"):                                             "{\"mcpServers\":{\"" + qualificationSentinel + "\":{\"command\":\"/usr/bin/false\"}}}\n",
		filepath.Join(root, "home", ".agents", "skills", qualificationSentinel, "SKILL.md"):     "---\nname: " + qualificationSentinel + "\ndescription: Synthetic native Skill that must not load in an isolated host.\n---\n",
		filepath.Join(root, "home", ".codex", "skills", qualificationSentinel, "SKILL.md"):      "---\nname: " + qualificationSentinel + "\ndescription: Synthetic native Skill that must not load in an isolated host.\n---\n",
		filepath.Join(root, "home", ".claude", "skills", qualificationSentinel, "SKILL.md"):     "---\nname: " + qualificationSentinel + "\ndescription: Synthetic native Skill that must not load in an isolated host.\n---\n",
		filepath.Join(root, "repo", ".agents", "skills", qualificationManagedSkill, "SKILL.md"): "---\nname: " + qualificationManagedSkill + "\ndescription: Synthetic repository Skill that must remain visible in the isolated host.\n---\n",
		filepath.Join(root, "repo", ".claude", "skills", qualificationManagedSkill, "SKILL.md"): "---\nname: " + qualificationManagedSkill + "\ndescription: Synthetic repository Skill that must remain visible in the isolated host.\n---\n",
		filepath.Join(root, "repo", "AGENTS.md"):                                                "# OMCA interactive qualification repository\n",
		filepath.Join(root, "repo", "CLAUDE.md"):                                                "# OMCA interactive qualification repository\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func tuiQualificationEnvironment(root string, hosts []string, realEnv hostcontext.Environment) ([]string, map[string]hostcontext.HostDetection, error) {
	detectEnv := realEnv
	if shimDir := realEnv.Get("OMCA_SHIM_DIR"); shimDir != "" {
		detectEnv = envWithFilteredPath(realEnv, shimDir)
	}
	detections := make(map[string]hostcontext.HostDetection, len(hosts))
	prependDirs := make([]string, 0, len(hosts))
	for _, host := range hosts {
		detection, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, host)
		if err != nil {
			return nil, nil, err
		}
		if !detection.Installed || detection.Error != "" {
			return nil, nil, fmt.Errorf("%s is not safely detectable: installed=%v error=%q", host, detection.Installed, detection.Error)
		}
		path := detection.BinaryPath
		if shim.IsASDFShim(path) {
			path, err = shim.ResolveASDFShimTarget(path)
			if err != nil {
				return nil, nil, fmt.Errorf("resolving %s past asdf: %w", host, err)
			}
		}
		prependDirs = append(prependDirs, filepath.Dir(path))
		detections[host] = detection
	}
	prependDirs = uniqueStrings(prependDirs)
	path := strings.Join(prependDirs, string(os.PathListSeparator))
	if path != "" && detectEnv.Get("PATH") != "" {
		path += string(os.PathListSeparator)
	}
	path += detectEnv.Get("PATH")
	overrides := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
		"TMPDIR":          filepath.Join(root, "tmp"),
		"PATH":            path,
	}
	return shim.InjectEnv(detectEnv.Vars, overrides), detections, nil
}

func probeTUIHost(runner tuiQualificationRunner, env []string, repo string, detection hostcontext.HostDetection) tuiHostQualification {
	// Plain output and portable locale are automatic-probe settings only.
	// The human TUI receives the isolated environment with its terminal and
	// locale capabilities preserved from the caller.
	env = shim.InjectEnv(env, map[string]string{
		"TERM": "dumb", "NO_COLOR": "1", "LC_ALL": "C", "LANG": "C", "LC_CTYPE": "C",
	})
	result := tuiHostQualification{Host: detection.Host, Version: detection.Version}
	repository, repositoryErr := knowledge.Default()
	if repositoryErr != nil {
		result.Checks = append(result.Checks, tuiQualificationCheck{ID: "knowledge-pack", Status: "FAIL", Evidence: "E0", Detail: "loading Knowledge repository: " + repositoryErr.Error()})
	} else {
		resolution := repository.Resolve(detection.Host, detection.Surface, detection.Version)
		if resolution.Qualified {
			result.KnowledgePack = resolution.PackID
			result.Checks = append(result.Checks, tuiQualificationCheck{ID: "knowledge-pack", Status: "PASS", Evidence: "E2", Detail: "installed host version is covered by " + resolution.PackID})
		} else {
			result.Checks = append(result.Checks, tuiQualificationCheck{ID: "knowledge-pack", Status: "UNKNOWN", Evidence: "E0", Detail: resolution.Reason})
		}
	}

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 30*time.Second)
	defer cancel()
	switch detection.Host {
	case "codex":
		stdout, stderr, exit, err := runner.Run(ctx, env, repo, "codex", []string{"mcp", "list", "--json"}, "")
		result.Checks = append(result.Checks, evaluateCodexMCPList(stdout, stderr, exit, err))
		stdout, stderr, exit, err = runner.RunCodexSkills(ctx, env, repo)
		result.Checks = append(result.Checks, evaluateCodexSkillsList(stdout, stderr, exit, err, repo))
	case "claude-code":
		stdout, stderr, exit, err := runner.Run(ctx, env, repo, "claude", []string{"mcp", "list"}, "")
		result.Checks = append(result.Checks, evaluateClaudeMCPList(stdout, stderr, exit, err))
		result.Checks = append(result.Checks, unavailableClaudeSkillInventory(detection.Version))
	}
	result.Complete = allQualificationChecksPass(result.Checks)
	return result
}

func evaluateCodexMCPList(stdout, stderr string, exit int, runErr error) tuiQualificationCheck {
	check := tuiQualificationCheck{ID: "native-mcp-exclusion", Evidence: "E3"}
	if runErr != nil || exit != 0 {
		check.Status = "FAIL"
		check.Detail = fmt.Sprintf("codex mcp list failed (exit=%d): %s", exit, boundedDiagnostic(stderr, runErr))
		return check
	}
	var servers []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(stdout), &servers); err != nil {
		check.Status = "FAIL"
		check.Detail = "codex mcp list returned invalid JSON: " + err.Error()
		return check
	}
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	sort.Strings(names)
	if containsString(names, qualificationSentinel) || !containsString(names, "omca") {
		check.Status = "FAIL"
		check.Detail = fmt.Sprintf("host-reported MCP inventory=%v; want omca present and native sentinel absent", names)
		return check
	}
	check.Status = "PASS"
	check.Detail = fmt.Sprintf("host-reported MCP inventory=%v; native sentinel absent", names)
	return check
}

func evaluateClaudeMCPList(stdout, stderr string, exit int, runErr error) tuiQualificationCheck {
	check := tuiQualificationCheck{ID: "native-mcp-exclusion", Evidence: "E3"}
	if runErr != nil || exit != 0 {
		check.Status = "FAIL"
		check.Detail = fmt.Sprintf("claude mcp list failed (exit=%d): %s", exit, boundedDiagnostic(stderr, runErr))
		return check
	}
	if strings.Contains(stdout, qualificationSentinel) || !strings.Contains(stdout, "omca:") || !strings.Contains(stdout, "Connected") {
		check.Status = "FAIL"
		check.Detail = "Claude host report did not prove an omca-only connected MCP inventory"
		return check
	}
	check.Status = "PASS"
	check.Detail = "Claude host report shows omca connected and the native sentinel absent"
	return check
}

func evaluateCodexSkillsList(stdout, stderr string, exit int, runErr error, expectedCWD string) tuiQualificationCheck {
	check := tuiQualificationCheck{ID: "skill-isolation", Evidence: "E3"}
	if runErr != nil || exit != 0 {
		check.Status = "FAIL"
		check.Detail = fmt.Sprintf("codex app-server skills/list failed (exit=%d): %s", exit, boundedDiagnostic(stderr, runErr))
		return check
	}
	decoder := json.NewDecoder(strings.NewReader(stdout))
	foundResponse := false
	foundCWD := false
	foundManaged := false
	skillCount := 0
	for {
		var msg map[string]any
		if err := decoder.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			check.Status = "FAIL"
			check.Detail = "decoding codex app-server response: " + err.Error()
			return check
		}
		id, _ := msg["id"].(float64)
		if id != 2 {
			continue
		}
		foundResponse = true
		if responseErr, ok := msg["error"]; ok && responseErr != nil {
			encoded, _ := json.Marshal(responseErr)
			check.Status = "FAIL"
			check.Detail = "Codex app-server skills/list returned an error: " + string(encoded)
			return check
		}
		result, ok := msg["result"].(map[string]any)
		if !ok {
			check.Status = "FAIL"
			check.Detail = "Codex app-server skills/list response has no result object"
			return check
		}
		data, ok := result["data"].([]any)
		if !ok {
			check.Status = "FAIL"
			check.Detail = "Codex app-server skills/list result has no data array"
			return check
		}
		for _, entryValue := range data {
			entry, ok := entryValue.(map[string]any)
			if !ok {
				check.Status = "FAIL"
				check.Detail = "Codex app-server skills/list data contains a malformed entry"
				return check
			}
			cwd, _ := entry["cwd"].(string)
			if filepath.Clean(cwd) != filepath.Clean(expectedCWD) {
				continue
			}
			foundCWD = true
			skills, ok := entry["skills"].([]any)
			if !ok {
				check.Status = "FAIL"
				check.Detail = "Codex app-server skills/list entry for the qualification cwd has no skills array"
				return check
			}
			for _, skillValue := range skills {
				skill, ok := skillValue.(map[string]any)
				if !ok {
					check.Status = "FAIL"
					check.Detail = "Codex app-server skills/list returned a malformed Skill"
					return check
				}
				name, _ := skill["name"].(string)
				path, _ := skill["path"].(string)
				if name == "" {
					check.Status = "FAIL"
					check.Detail = "Codex app-server skills/list returned a Skill without a name"
					return check
				}
				skillCount++
				if name == qualificationSentinel || strings.Contains(path, qualificationSentinel) {
					check.Status = "FAIL"
					check.Detail = "Codex host-reported Skill inventory contains the native sentinel"
					return check
				}
				if name == qualificationManagedSkill || strings.Contains(path, qualificationManagedSkill) {
					foundManaged = true
				}
			}
		}
	}
	if !foundResponse {
		check.Status = "FAIL"
		check.Detail = "Codex app-server returned no response to skills/list"
		return check
	}
	if !foundCWD {
		check.Status = "FAIL"
		check.Detail = fmt.Sprintf("Codex app-server skills/list returned no inventory for qualification cwd %q", expectedCWD)
		return check
	}
	if !foundManaged {
		check.Status = "FAIL"
		check.Detail = "Codex host-reported Skill inventory does not contain the managed repository sentinel"
		return check
	}
	check.Status = "PASS"
	check.Detail = fmt.Sprintf("Codex host-reported %d visible Skills; managed repository sentinel present and native sentinels absent", skillCount)
	return check
}

func unavailableClaudeSkillInventory(version string) tuiQualificationCheck {
	return tuiQualificationCheck{
		ID:       "skill-isolation",
		Status:   "UNKNOWN",
		Evidence: "E1",
		Detail:   fmt.Sprintf("Claude Code %s exposes no safe non-interactive Skill inventory; run --interactive under human supervision and verify /skills shows the managed repository sentinel but not the native sentinel", version),
	}
}

func pendingHumanTUIQualification(host string) tuiQualificationCheck {
	return tuiQualificationCheck{
		ID:       "human-interactive-tui",
		Status:   "UNKNOWN",
		Evidence: "E0",
		Detail:   fmt.Sprintf("%s initial/restart TUI and omca_status model canary require --interactive and explicit human attestation", host),
	}
}

func runHumanTUIQualification(env []string, repo, executable, host string, stdout, stderr io.Writer) tuiQualificationCheck {
	fmt.Fprintf(stdout, "\nHuman qualification for %s\n", host)
	fmt.Fprintf(stdout, "  1. Open /mcp and verify only OMCA-managed entries are present.\n")
	fmt.Fprintf(stdout, "  2. Open /skills and verify %q is present while %q is absent.\n", qualificationManagedSkill, qualificationSentinel)
	fmt.Fprintf(stdout, "  3. Ask the model to call omca_status and verify it returns this managed host/generation.\n")
	fmt.Fprintf(stdout, "  4. Exit normally. The harness repeats the launch once to prove restart behavior.\n\n")

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 30*time.Minute)
	defer cancel()
	reader := bufio.NewReader(os.Stdin)
	for _, phase := range []string{"initial", "restart"} {
		fmt.Fprintf(stdout, "\nStarting %s %s TUI launch...\n", host, phase)
		cmd := exec.CommandContext(ctx, executable, "run", host)
		cmd.Dir = repo
		cmd.Env = append([]string{}, env...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return tuiQualificationCheck{ID: "human-interactive-tui", Status: "FAIL", Evidence: "E4", Detail: phase + " interactive host launch exited with error: " + err.Error()}
		}
		fmt.Fprintf(stdout, "Did /mcp, /skills, and the omca_status model canary all pass in the %s launch? Type yes to attest: ", phase)
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "yes" {
			return tuiQualificationCheck{ID: "human-interactive-tui", Status: "UNKNOWN", Evidence: "E0", Detail: "human did not attest that the " + phase + " host TUI checks passed"}
		}
	}
	return tuiQualificationCheck{ID: "human-interactive-tui", Status: "PASS", Evidence: "E4", Detail: "human attested that initial and restarted TUI launches included the managed repository Skill, excluded native sentinels, and completed an omca_status model canary"}
}

func applyHumanTUIQualification(host *tuiHostQualification, human tuiQualificationCheck) {
	if host == nil || host.Host != "claude-code" || human.Status != "PASS" {
		return
	}
	for i := range host.Checks {
		if host.Checks[i].ID == "skill-isolation" && host.Checks[i].Status == "UNKNOWN" {
			host.Checks[i] = tuiQualificationCheck{
				ID:       "skill-isolation",
				Status:   "PASS",
				Evidence: "E4",
				Detail:   "human verified in both initial and restarted Claude Code TUIs that /skills includes the managed repository sentinel and excludes the native sentinel",
			}
		}
	}
}

func hasInteractiveTerminal() bool {
	stdin, err := os.Stdin.Stat()
	if err != nil || stdin.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	stdout, err := os.Stdout.Stat()
	return err == nil && stdout.Mode()&os.ModeCharDevice != 0
}

func allQualificationChecksPass(checks []tuiQualificationCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if check.Status != "PASS" {
			return false
		}
	}
	return true
}

func renderTUIQualification(w io.Writer, artifact tuiQualificationArtifact) {
	fmt.Fprintf(w, "OMCA interactive TUI qualification (%s)\n", artifact.GeneratedAt)
	for _, host := range artifact.Hosts {
		fmt.Fprintf(w, "\n%s %s complete=%v\n", host.Host, host.Version, host.Complete)
		for _, check := range host.Checks {
			fmt.Fprintf(w, "  [%-7s] %-28s %s: %s\n", check.Status, check.ID, check.Evidence, check.Detail)
		}
	}
	fmt.Fprintf(w, "\nreal native state clean=%v\n", artifact.RealNativeStateClean)
	for _, diff := range artifact.RealNativeStateDiffs {
		fmt.Fprintf(w, "  changed during window: %s\n", diff)
	}
	if artifact.ScratchRetained != "" {
		fmt.Fprintf(w, "scratch retained at %s\n", artifact.ScratchRetained)
	}
	fmt.Fprintf(w, "overall complete=%v\n", artifact.Complete)
}

func removeQualificationScratch(root string) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Host CLIs may create scratch-local symlinks whose targets are real,
		// external installation files (Codex creates applypatch/apply_patch/
		// codex-execve-wrapper links back to its installed native binary).
		// os.Chmod follows symlinks on macOS; chmodding one here would mutate
		// the real host installation while supposedly cleaning a temp lane.
		// Never chmod a symlink. RemoveAll removes the link itself without
		// following it after the containing directory has been made writable.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() {
			_ = os.Chmod(path, 0o700)
		} else {
			_ = os.Chmod(path, 0o600)
		}
		return nil
	})
	_ = os.RemoveAll(root)
}

func boundedDiagnostic(stderr string, runErr error) string {
	detail := strings.TrimSpace(stderr)
	if runErr != nil {
		if detail != "" {
			detail += "; "
		}
		detail += runErr.Error()
	}
	if len(detail) > 300 {
		// The launch report precedes host errors. Keep the failure at the
		// tail rather than filling the diagnostic with startup estimates.
		detail = "..." + detail[len(detail)-300:]
	}
	return detail
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
