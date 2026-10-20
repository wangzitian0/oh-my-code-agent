package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultRenderCheckInterval is the default cadence for the periodic render
// check worker (issue #125).
const DefaultRenderCheckInterval = 30 * time.Minute

// DefaultSessionDebounce is the default debounce window for the session
// watcher before running ws-mem-distill (issue #125).
const DefaultSessionDebounce = 60 * time.Second

// DefaultSessionMaxWait is the maximum wait deadline before forcing a
// distillation run under continuous session writes.
const DefaultSessionMaxWait = 5 * time.Minute

// DefaultSessionPollInterval is the filesystem scan cadence for the session
// watcher worker.
const DefaultSessionPollInterval = 10 * time.Second

// RenderDriftCard represents one stale, missing, or hand-edited checkout
// reported by the periodic ws-render / ws-publish check (issue #125).
type RenderDriftCard struct {
	ID           string    `json:"id"`
	Profile      string    `json:"profile"`
	WorkspaceDir string    `json:"workspace_dir"`
	CheckoutPath string    `json:"checkout_path"`
	Status       string    `json:"status"`
	Detail       string    `json:"detail"`
	Remediation  string    `json:"remediation"`
	Command      string    `json:"command"`
	ObservedAt   time.Time `json:"observed_at"`
}

// RenderCheckOutput is the JSON shape accepted from `ws-render --check --all --json`.
type RenderCheckOutput struct {
	Checkouts []RenderCheckEntry `json:"checkouts"`
}

// RenderCheckEntry is a single checkout result from `ws-render --check --all --json`.
type RenderCheckEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// RenderCheckWorker periodically invokes `ws-render --check --all --json` (or
// falls back to `ws-publish --check-files --checkout <path>`) per profile and
// maintains the current set of RenderDriftCards. Calling Restore runs
// `ws-render --checkout <path>` (or `ws-publish --checkout <path>`) and
// re-checks to clear the card. Nothing in this worker renders instructions or
// writes directly to a checkout.
type RenderCheckWorker struct {
	mu        sync.RWMutex
	cfg       *Config
	interval  time.Duration
	renderBin string
	cards     map[string]RenderDriftCard
	stopCh    chan struct{}
	running   bool
}

// NewRenderCheckWorker constructs a RenderCheckWorker for cfg.
func NewRenderCheckWorker(cfg *Config) *RenderCheckWorker {
	interval := DefaultRenderCheckInterval
	if cfg != nil && cfg.RenderCheckInterval != "" {
		if d, err := time.ParseDuration(cfg.RenderCheckInterval); err == nil && d > 0 {
			interval = d
		}
	}
	return &RenderCheckWorker{
		cfg:       cfg,
		interval:  interval,
		renderBin: resolveDevEnvBinary("ws-render", "ws-publish"),
		cards:     make(map[string]RenderDriftCard),
		stopCh:    make(chan struct{}),
	}
}

// SetInterval overrides the periodic check cadence (used by tests).
func (w *RenderCheckWorker) SetInterval(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.interval = d
}

// SetRenderBinary overrides the binary invoked for check and restore (used by tests).
func (w *RenderCheckWorker) SetRenderBinary(bin string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.renderBin = bin
}

// Start runs the periodic render check loop in the background.
func (w *RenderCheckWorker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.stopCh = make(chan struct{})
	interval := w.interval
	w.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stopCh:
				return
			case <-ticker.C:
				w.CheckOnce(ctx)
			}
		}
	}()
}

// Stop halts the background render check ticker.
func (w *RenderCheckWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	w.running = false
	close(w.stopCh)
}

// Cards returns the sorted slice of active RenderDriftCards across all profiles.
func (w *RenderCheckWorker) Cards() []RenderDriftCard {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]RenderDriftCard, 0, len(w.cards))
	for _, c := range w.cards {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Profile != out[j].Profile {
			return out[i].Profile < out[j].Profile
		}
		return out[i].CheckoutPath < out[j].CheckoutPath
	})
	return out
}

// CheckOnce runs one render check pass across every configured profile and
// updates the active drift card map.
func (w *RenderCheckWorker) CheckOnce(ctx context.Context) []RenderDriftCard {
	w.mu.RLock()
	cfg := w.cfg
	renderBin := w.renderBin
	w.mu.RUnlock()

	if cfg == nil {
		return nil
	}

	nextCards := make(map[string]RenderDriftCard)
	now := time.Now().UTC()

	profileNames := make([]string, 0, len(cfg.Profiles))
	for pName := range cfg.Profiles {
		profileNames = append(profileNames, pName)
	}
	sort.Strings(profileNames)

	for _, pName := range profileNames {
		prof := cfg.Profiles[pName]
		for _, wsRoot := range prof.WorkspaceRoots {
			wsRoot = expandHome(wsRoot)
			entries := checkWorkspaceRender(ctx, renderBin, pName, wsRoot)
			for _, entry := range entries {
				if entry.Status == "" || entry.Status == "current" || entry.Status == "CURRENT" {
					continue
				}
				cardID := fmt.Sprintf("DR-render-%s-%s", pName, filepath.Base(entry.Path))
				nextCards[entry.Path] = RenderDriftCard{
					ID:           cardID,
					Profile:      pName,
					WorkspaceDir: wsRoot,
					CheckoutPath: entry.Path,
					Status:       entry.Status,
					Detail:       entry.Detail,
					Remediation:  fmt.Sprintf("ws-render --checkout %s", entry.Path),
					Command:      "ws-render --check --all --json",
					ObservedAt:   now,
				}
			}
		}
	}

	w.mu.Lock()
	w.cards = nextCards
	w.mu.Unlock()

	return w.Cards()
}

// Restore runs `ws-render --checkout <checkoutPath>` for a drifted checkout
// and re-runs CheckOnce so the card closes when the checkout becomes current.
func (w *RenderCheckWorker) Restore(ctx context.Context, checkoutPath string) error {
	if strings.TrimSpace(checkoutPath) == "" {
		return fmt.Errorf("hub: restore: checkout path is required")
	}
	w.mu.RLock()
	renderBin := w.renderBin
	w.mu.RUnlock()

	cmd := exec.CommandContext(ctx, renderBin, "--checkout", checkoutPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("hub: restore %s via %s: %w (%s)", checkoutPath, renderBin, err, strings.TrimSpace(string(out)))
	}

	w.CheckOnce(ctx)
	return nil
}

func checkWorkspaceRender(ctx context.Context, renderBin, profileName, wsRoot string) []RenderCheckEntry {
	// First try invoking `<renderBin> --check --all --json` scoped to the
	// profile's workspace root via WS_PATH / working directory.
	cmd := exec.CommandContext(ctx, renderBin, "--check", "--all", "--json")
	if info, err := os.Stat(wsRoot); err == nil && info.IsDir() {
		cmd.Dir = wsRoot
	}
	cmd.Env = append(os.Environ(),
		"WS_PATH="+wsRoot,
		"WS_WORKSPACE_ID="+profileName,
	)
	out, err := cmd.Output()
	if len(out) > 0 {
		var parsed RenderCheckOutput
		if jerr := json.Unmarshal(out, &parsed); jerr == nil && parsed.Checkouts != nil {
			return parsed.Checkouts
		}
		var list []RenderCheckEntry
		if jerr := json.Unmarshal(out, &list); jerr == nil && list != nil {
			return list
		}
	}
	// If the binary does not support `--check --all --json` (e.g., `ws-publish`
	// checking individual checkouts), enumerate git checkouts under wsRoot with
	// `.ws-publish/manifest.json` or `.ws-render/manifest.json` and invoke
	// `<renderBin> --check-files --checkout <path>`.
	_ = err
	return checkIndividualCheckouts(ctx, renderBin, profileName, wsRoot)
}

func checkIndividualCheckouts(ctx context.Context, renderBin, profileName, wsRoot string) []RenderCheckEntry {
	entries, err := os.ReadDir(wsRoot)
	if err != nil {
		return nil
	}
	var results []RenderCheckEntry
	for _, d := range entries {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		coPath := filepath.Join(wsRoot, d.Name())
		if !hasRenderManifest(coPath) {
			continue
		}
		cmd := exec.CommandContext(ctx, renderBin, "--check-files", "--checkout", coPath)
		cmd.Env = append(os.Environ(),
			"WS_PATH="+wsRoot,
			"WS_WORKSPACE_ID="+profileName,
		)
		out, runErr := cmd.CombinedOutput()
		text := strings.TrimSpace(string(out))
		if runErr == nil {
			results = append(results, RenderCheckEntry{
				Path:   coPath,
				Status: "current",
				Detail: text,
			})
		} else {
			results = append(results, RenderCheckEntry{
				Path:   coPath,
				Status: "drifted",
				Detail: text,
			})
		}
	}
	return results
}

func hasRenderManifest(checkoutPath string) bool {
	if _, err := os.Stat(filepath.Join(checkoutPath, ".ws-publish", "manifest.json")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(checkoutPath, ".ws-render", "manifest.json")); err == nil {
		return true
	}
	return false
}

// SessionWatcherWorker watches per-profile session directories (Claude, Codex,
// Pi, Antigravity), debounces modifications, and invokes `ws-mem-distill`
// isolated per profile so one profile's session never triggers distillation
// into another profile's memory (issue #125).
type SessionWatcherWorker struct {
	mu           sync.Mutex
	cfg          *Config
	debounce     time.Duration
	maxWait      time.Duration
	pollInterval time.Duration
	distillBin   string
	extraPaths   map[string][]string
	states       map[string]*profileWatchState
	stopCh       chan struct{}
	running      bool
}

type profileWatchState struct {
	lastKnownMtime      time.Time
	pendingTriggerSince time.Time
	firstPendingSince   time.Time
	distillRuns         uint64
}

// NewSessionWatcherWorker creates a SessionWatcherWorker for cfg.
func NewSessionWatcherWorker(cfg *Config) *SessionWatcherWorker {
	return &SessionWatcherWorker{
		cfg:          cfg,
		debounce:     DefaultSessionDebounce,
		maxWait:      DefaultSessionMaxWait,
		pollInterval: DefaultSessionPollInterval,
		distillBin:   resolveDevEnvBinary("ws-mem-distill"),
		extraPaths:   make(map[string][]string),
		states:       make(map[string]*profileWatchState),
		stopCh:       make(chan struct{}),
	}
}

// SetDebounce overrides the debounce and maxWait durations (used by tests).
func (s *SessionWatcherWorker) SetDebounce(debounce, maxWait time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.debounce = debounce
	if maxWait > 0 {
		s.maxWait = maxWait
	}
}

// SetPollInterval overrides the watcher polling cadence (used by tests).
func (s *SessionWatcherWorker) SetPollInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pollInterval = d
}

// SetDistillBinary overrides the `ws-mem-distill` binary path (used by tests).
func (s *SessionWatcherWorker) SetDistillBinary(bin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.distillBin = bin
}

// SetProfileWatchPaths sets explicit session watch paths for a profile (used by tests).
func (s *SessionWatcherWorker) SetProfileWatchPaths(profile string, paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extraPaths[profile] = append([]string(nil), paths...)
}

// DistillCount returns how many times `ws-mem-distill` has executed for profile.
func (s *SessionWatcherWorker) DistillCount(profile string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[profile]
	if !ok {
		return 0
	}
	return st.distillRuns
}

// Start launches the background session watcher loop.
func (s *SessionWatcherWorker) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	pollInterval := s.pollInterval
	s.initBaselinesLocked(time.Now())
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.PollOnce(ctx, time.Now())
			}
		}
	}()
}

// Stop halts the background session watcher loop.
func (s *SessionWatcherWorker) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
}

func (s *SessionWatcherWorker) initBaselinesLocked(now time.Time) {
	if s.cfg == nil {
		return
	}
	for pName, prof := range s.cfg.Profiles {
		if _, exists := s.states[pName]; !exists {
			paths := s.watchPathsForProfileLocked(pName, prof)
			s.states[pName] = &profileWatchState{
				lastKnownMtime: latestSessionMtime(paths),
			}
		}
	}
}

func (s *SessionWatcherWorker) watchPathsForProfileLocked(profile string, prof ProfileConfig) []string {
	if custom, ok := s.extraPaths[profile]; ok && len(custom) > 0 {
		return custom
	}
	var paths []string
	for _, wsRoot := range prof.WorkspaceRoots {
		wsRoot = expandHome(wsRoot)
		paths = append(paths,
			filepath.Join(wsRoot, ".ws", ".codex", "sessions"),
			filepath.Join(wsRoot, ".ws", ".pi", "agent", "sessions"),
			filepath.Join(wsRoot, ".ws", ".claude", "projects"),
			filepath.Join(wsRoot, ".ws", "claude-mem", "claude-mem.db"),
			filepath.Join(wsRoot, ".ws", ".gemini", "antigravity", "brain"),
		)
	}
	return paths
}

// PollOnce inspects session directories for each configured profile at time
// now and triggers `ws-mem-distill` for any profile whose debounce window or
// maxWait deadline has elapsed.
func (s *SessionWatcherWorker) PollOnce(ctx context.Context, now time.Time) []string {
	s.mu.Lock()
	if s.cfg == nil {
		s.mu.Unlock()
		return nil
	}
	s.initBaselinesLocked(now)

	type pendingRun struct {
		profile string
		wsRoot  string
		bin     string
	}
	var toRun []pendingRun

	profileNames := make([]string, 0, len(s.cfg.Profiles))
	for pName := range s.cfg.Profiles {
		profileNames = append(profileNames, pName)
	}
	sort.Strings(profileNames)

	for _, pName := range profileNames {
		prof := s.cfg.Profiles[pName]
		st := s.states[pName]
		paths := s.watchPathsForProfileLocked(pName, prof)
		currentMtime := latestSessionMtime(paths)

		if currentMtime.After(st.lastKnownMtime) {
			st.lastKnownMtime = currentMtime
			if st.firstPendingSince.IsZero() {
				st.firstPendingSince = now
			}
			st.pendingTriggerSince = now
		}

		shouldTrigger := false
		if !st.pendingTriggerSince.IsZero() && now.Sub(st.pendingTriggerSince) >= s.debounce {
			shouldTrigger = true
		} else if !st.firstPendingSince.IsZero() && now.Sub(st.firstPendingSince) >= s.maxWait {
			shouldTrigger = true
		}

		if shouldTrigger {
			st.pendingTriggerSince = time.Time{}
			st.firstPendingSince = time.Time{}
			st.distillRuns++
			wsRoot := ""
			if len(prof.WorkspaceRoots) > 0 {
				wsRoot = expandHome(prof.WorkspaceRoots[0])
			}
			toRun = append(toRun, pendingRun{
				profile: pName,
				wsRoot:  wsRoot,
				bin:     s.distillBin,
			})
		}
	}
	s.mu.Unlock()

	var triggered []string
	for _, r := range toRun {
		runDistillForProfile(ctx, r.bin, r.profile, r.wsRoot)
		triggered = append(triggered, r.profile)
	}
	return triggered
}

func runDistillForProfile(ctx context.Context, distillBin, profile, wsRoot string) {
	cmd := exec.CommandContext(ctx, distillBin, "--scan")
	if wsRoot != "" {
		if info, err := os.Stat(wsRoot); err == nil && info.IsDir() {
			cmd.Dir = wsRoot
		}
		bmHome := filepath.Join(wsRoot, ".ws", "basic-memory")
		cmd.Env = append(os.Environ(),
			"WS_PATH="+wsRoot,
			"WS_WORKSPACE_ID="+profile,
			"BASIC_MEMORY_HOME="+bmHome,
		)
	} else {
		cmd.Env = append(os.Environ(), "WS_WORKSPACE_ID="+profile)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[hub:session-watcher] ws-mem-distill for profile %q failed: %v (%s)", profile, err, strings.TrimSpace(string(out)))
	}
}

func latestSessionMtime(paths []string) time.Time {
	var latest time.Time
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if info.ModTime().After(latest) {
				latest = info.ModTime()
			}
			continue
		}
		_ = filepath.WalkDir(p, func(fp string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".db") {
				if fi, statErr := d.Info(); statErr == nil && fi.ModTime().After(latest) {
					latest = fi.ModTime()
				}
			}
			return nil
		})
	}
	return latest
}

func resolveDevEnvBinary(candidates ...string) string {
	home, _ := os.UserHomeDir()
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		if home != "" {
			localBin := filepath.Join(home, ".local", "bin", name)
			if _, err := os.Stat(localBin); err == nil {
				return localBin
			}
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "ws-render"
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

