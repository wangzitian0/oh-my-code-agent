package tui

import (
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// ViewKind names one of this package's four read-only views (issue #34's
// own named list). Order here is also tab order and Model's own
// left/right navigation order.
type ViewKind int

const (
	ViewOverview ViewKind = iota
	ViewDrift
	ViewAssets
	ViewGenerations
)

// viewOrder is the fixed navigation cycle basic left/right/tab navigation
// walks — issue #34's AC requires nothing more elaborate than this.
var viewOrder = []ViewKind{ViewOverview, ViewDrift, ViewAssets, ViewGenerations}

// viewTitles labels each ViewKind for the tab bar (tabbar.go) and for
// error messages; kept next to viewOrder since both enumerate the same
// closed set.
var viewTitles = map[ViewKind]string{
	ViewOverview:    "Overview",
	ViewDrift:       "Drift",
	ViewAssets:      "Assets",
	ViewGenerations: "Generations",
}

// uiMode names Model's own interaction modes: modeBrowse is PR-30's original
// read-only navigation; modeConfirm is the one-screen "reviewed Change Set"
// a staged activation shows before a single approval keypress applies it
// (see confirm.go's renderConfirmScreen and Model's own approveReview);
// modeDebug is issue #36's Debug drill-down, entered from the Drift view's
// currently selected action card (enterDebug) and rendered by
// renderDebugMatrix/renderDebugEntity (debug.go) according to Model's own
// debugLevel stack depth.
type uiMode int

const (
	modeBrowse uiMode = iota
	modeConfirm
	modeDebug
)

// Model is this package's bubbletea.Model: an immutable report.Artifact
// (rebuilt in place after a real action succeeds -- see refreshArtifact --
// never recomputed speculatively) plus which of the four views is
// currently active, and, from issue #35 on, the small amount of additional
// state its action layer needs: an optional ActionContext (nil/zero
// disables every action key, preserving PR-30's original read-only
// behavior exactly -- see ActionContext.enabled), a host cursor shared by
// the Assets/Generations views, and the in-progress review/message/
// restart-status state one activate-or-rollback cycle produces.
type Model struct {
	Artifact report.Artifact
	active   ViewKind

	actions ActionContext
	clock   func() time.Time

	// hostCursor selects which of Artifact.Debug's sorted host keys the
	// Assets view's activate action and the Generations view's rollback
	// action apply to (currentHost) -- both views already render every
	// host, so this is the one piece of navigation state PR-30's read-only
	// foundation had no need for.
	hostCursor int

	mode uiMode
	// review and pendingGen are non-nil/non-zero exactly while mode ==
	// modeConfirm: the Change Set currently under review, and the pending
	// generation approving it will activate.
	review     *changeReview
	pendingGen domain.Generation

	// message is a one-line, human-readable result of the last action
	// (staged/activated/rolled back/failed), always shown just under the
	// tab bar once non-empty -- appended to, never a running log, matching
	// this package's existing rendering style of showing exactly the
	// current state, not history.
	message string
	// restartStatuses is issue #35's "restart_required per host" AC:
	// populated after a successful activation (restartStatusesForHosts).
	restartStatuses []runtime.RestartStatus

	// driftCursor selects which of Artifact.ActionCards the Drift view's
	// own up/down navigation currently highlights (currentDriftCard) --
	// issue #36's entry point into the Debug drill-down (enterDebug): only
	// meaningful while active == ViewDrift, the same way hostCursor is only
	// meaningful on the Assets/Generations views.
	driftCursor int

	// debugLevel/debugCardID/debugMatrixCursor/debugHost/debugConcept/
	// debugLogicalID/debugShowPlanes are issue #36's Debug drill-down state,
	// live only while mode == modeDebug (debug.go's renderDebugMatrix/
	// renderDebugEntity, Model's own enterDebug/updateDebug/
	// drillIntoSelectedCell). debugCardID is looked up fresh from
	// m.Artifact.ActionCards on every render (findCardByID) rather than a
	// stored report.DriftCard, so a refreshed Artifact (refreshArtifact,
	// after an unrelated Assets/Generations action) never leaves this state
	// pointing at a stale copy.
	debugLevel        debugLevel
	debugCardID       string
	debugMatrixCursor int
	debugHost         string
	debugConcept      string
	debugLogicalID    string
	debugShowPlanes   bool
}

// NewModel constructs a Model over a already-built report.Artifact,
// starting on the Overview view — the same starting point `omca report`
// gives the CLI. Actions are disabled until WithActionContext attaches a
// real ActionContext.
func NewModel(a report.Artifact) Model {
	return Model{Artifact: a, active: ViewOverview}
}

// WithActionContext attaches ctx to m, enabling issue #35's action keys
// ('a' to activate an AVAILABLE asset, 'y'/enter to approve a reviewed
// Change Set, 'n'/esc to cancel one, 'r' to roll back) -- see
// ActionContext's own doc comment for what it carries and why. A caller
// that never calls this (every PR-30 test, and any embedder that only
// wants the read-only views) gets exactly PR-30's original behavior: those
// keys are simply inert (ActionContext.enabled reports false).
func (m Model) WithActionContext(ctx ActionContext) Model {
	m.actions = ctx
	return m
}

// WithClock overrides Model's own clock (time.Now by default, see now())
// -- tests use this for a deterministic, injected "now," the same
// discipline every internal/runtime function this package's actions.go
// calls already requires of ITS callers rather than reading the real clock
// implicitly.
func (m Model) WithClock(clock func() time.Time) Model {
	m.clock = clock
	return m
}

// now returns m's current time: the injected clock if WithClock set one,
// else time.Now.
func (m Model) now() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

// sortedDebugHosts returns a.Debug's host keys, sorted -- the same order
// RenderAssets/RenderGenerations already iterate hosts in, reused here so
// hostCursor always walks hosts in the identical order a human sees them
// rendered.
func sortedDebugHosts(a report.Artifact) []string {
	hosts := make([]string, 0, len(a.Debug))
	for h := range a.Debug {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

// currentHost returns the host m.hostCursor currently selects among
// m.Artifact.Debug's sorted host keys -- the "which host is this action
// about" question both activateSelected and rollbackSelected need
// answered. ok is false when there are no hosts to select at all (an empty
// Artifact.Debug).
func (m Model) currentHost() (string, bool) {
	hosts := sortedDebugHosts(m.Artifact)
	if len(hosts) == 0 {
		return "", false
	}
	i := m.hostCursor % len(hosts)
	if i < 0 {
		i += len(hosts)
	}
	return hosts[i], true
}

// currentDriftCard returns the ActionCard m.driftCursor currently selects
// among m.Artifact.ActionCards -- the "which card would enter open the
// Debug view for" question enterDebug and driftSelectionHintLine both need
// answered. ok is false when there is no drift at all (an empty
// ActionCards, e.g. "no drift" or a zero-valued Artifact).
func (m Model) currentDriftCard() (report.DriftCard, bool) {
	cards := m.Artifact.ActionCards
	if len(cards) == 0 {
		return report.DriftCard{}, false
	}
	i := m.driftCursor % len(cards)
	if i < 0 {
		i += len(cards)
	}
	return cards[i], true
}

// Active returns the currently selected view.
func (m Model) Active() ViewKind {
	return m.active
}

// SetActive selects v as the current view, ignoring any value outside
// viewOrder (defensive: Model's own Update never produces one, but a test
// or future caller constructing a Model directly should not be able to put
// it in a state View() cannot render).
func (m Model) SetActive(v ViewKind) Model {
	for _, candidate := range viewOrder {
		if candidate == v {
			m.active = v
			return m
		}
	}
	return m
}

// Init satisfies tea.Model. This package has no async work to kick off —
// the Artifact is already fully built by the time NewModel is called
// (cmd/omca's TUI entry point builds it up front, the same
// buildArtifactForCLI pipeline the CLI report commands use), so there is
// nothing to Init.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles basic navigation between the four views (issue #34: arrow/
// tab keys to cycle, digit keys 1-4 to jump directly, q/ctrl+c to quit) plus,
// from issue #35 on, the action keys ActionContext.enabled unlocks: 'up'/'k'
// and 'down'/'j' move the host cursor (currentHost) the Assets/Generations
// views share, 'a' activates the current host's first AVAILABLE skill/
// mcpServer asset (activateSelected), and 'r' rolls back the current host
// (rollbackSelected). While mode == modeConfirm, every key is routed to
// updateConfirm instead: an in-progress reviewed Change Set must be
// explicitly approved or cancelled before any other navigation resumes.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.mode == modeConfirm {
		return m.updateConfirm(keyMsg)
	}
	if m.mode == modeDebug {
		return m.updateDebug(keyMsg)
	}

	switch keyMsg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "right", "l", "tab":
		m.active = viewOrder[(indexOf(m.active)+1)%len(viewOrder)]
	case "left", "h", "shift+tab":
		m.active = viewOrder[(indexOf(m.active)-1+len(viewOrder))%len(viewOrder)]
	case "1":
		m.active = ViewOverview
	case "2":
		m.active = ViewDrift
	case "3":
		m.active = ViewAssets
	case "4":
		m.active = ViewGenerations
	case "up", "k":
		if m.active == ViewDrift {
			m.driftCursor--
		} else {
			m.hostCursor--
		}
	case "down", "j":
		if m.active == ViewDrift {
			m.driftCursor++
		} else {
			m.hostCursor++
		}
	case "a":
		m = m.activateSelected()
	case "r":
		m = m.rollbackSelected()
	case "enter":
		if m.active == ViewDrift {
			m = m.enterDebug()
		}
	}
	return m, nil
}

// enterDebug implements issue #36's entry point into the Debug drill-down:
// pressing enter on the Drift view opens debugLevelMatrix (Model.
// renderDebugView -> renderDebugMatrix, debug.go) for the action card
// driftCursor currently selects (currentDriftCard) -- "reachable from an
// actual action card," never a dead-end standalone screen. A no-op (with an
// explanatory message, matching activateSelected/rollbackSelected's own
// "nothing to do" contract) when there is no drift to drill into at all.
func (m Model) enterDebug() Model {
	card, ok := m.currentDriftCard()
	if !ok {
		m.message = "no action card to drill into (no drift)"
		return m
	}
	m.mode = modeDebug
	m.debugLevel = debugLevelMatrix
	m.debugCardID = card.ID
	m.debugMatrixCursor = 0
	m.debugShowPlanes = false
	return m
}

// updateDebug handles keys while mode == modeDebug: issue #36's two-level
// Debug drill-down stack on top of the Drift view. up/down (or k/j) move
// the Host Matrix row cursor at debugLevelMatrix; enter drills into the
// selected row's Effective State/Precedence Trace/Evidence/Native Artifacts
// (drillIntoSelectedCell, moving to debugLevelEntity); p toggles the Native
// vs Current vs Pending pane at either level; esc/b steps back one level
// (debugLevelEntity -> debugLevelMatrix -> modeBrowse on the Drift view,
// mirroring updateConfirm's own single-level "n/esc cancels back" pattern,
// just with a second level); quit keys still quit mid-drill-down (matching
// updateConfirm's own identical "quit always works" contract).
func (m Model) updateDebug(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyMsg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "p":
		m.debugShowPlanes = !m.debugShowPlanes
		return m, nil
	case "esc", "b":
		if m.debugLevel == debugLevelEntity {
			m.debugLevel = debugLevelMatrix
			return m, nil
		}
		m.mode = modeBrowse
		return m, nil
	case "up", "k":
		if m.debugLevel == debugLevelMatrix {
			m.debugMatrixCursor--
		}
		return m, nil
	case "down", "j":
		if m.debugLevel == debugLevelMatrix {
			m.debugMatrixCursor++
		}
		return m, nil
	case "enter":
		if m.debugLevel == debugLevelMatrix {
			return m.drillIntoSelectedCell(), nil
		}
		return m, nil
	}
	return m, nil
}

// drillIntoSelectedCell moves Model from debugLevelMatrix to
// debugLevelEntity for the Host Matrix row m.debugMatrixCursor currently
// selects -- splitEntityID (debug.go) recovers the (concept, logicalID)
// pair the row's own EntityID encodes. A no-op when the card no longer
// resolves (should not happen in practice: enterDebug only ever sets
// debugCardID to a card m.Artifact.ActionCards actually contains) or has no
// Matrix rows to drill into.
func (m Model) drillIntoSelectedCell() Model {
	card, ok := findCardByID(m.Artifact, m.debugCardID)
	if !ok || len(card.Matrix) == 0 {
		return m
	}
	i := m.debugMatrixCursor % len(card.Matrix)
	if i < 0 {
		i += len(card.Matrix)
	}
	row := card.Matrix[i]
	concept, logicalID, ok := splitEntityID(row.EntityID)
	if !ok {
		return m
	}
	m.debugLevel = debugLevelEntity
	m.debugHost = row.Host
	m.debugConcept = concept
	m.debugLogicalID = logicalID
	return m
}

// updateConfirm handles keys while mode == modeConfirm: 'y'/enter approves
// the ENTIRE reviewed Change Set (approveReview -- issue #35's "one human
// approval can execute a complete reviewed Change Set" AC), 'n'/esc cancels
// back to modeBrowse leaving the pending generation staged (never discarded
// -- an operator can still activate it later, e.g. through `omca activate`),
// and quit keys still quit even mid-review.
func (m Model) updateConfirm(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyMsg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "y", "enter":
		return m.approveReview(), nil
	case "n", "esc":
		if m.review != nil {
			m.message = fmt.Sprintf("%s: activation cancelled; pending generation %s remains staged", m.review.Host, m.pendingGen.Metadata.ID)
		}
		m.mode = modeBrowse
		m.review = nil
		return m, nil
	}
	return m, nil
}

// activateSelected implements issue #35's "Activating an AVAILABLE asset
// stages pending" AC for the 'a' key: only meaningful from the Assets view,
// with a real ActionContext attached and at least one AVAILABLE skill/
// mcpServer candidate for the current host cursor (firstActivatableCandidate).
// A successful stage moves Model into modeConfirm, showing the FULL
// reviewed Change Set DiffProposedChanges/ClassifyChange computed between
// this host's current and newly-staged pending generation -- not yet
// applied until the operator presses 'y' (updateConfirm/approveReview).
func (m Model) activateSelected() Model {
	if m.active != ViewAssets {
		return m
	}
	if !m.actions.enabled() {
		m.message = "actions are not available: no worktree/action context attached to this TUI session"
		return m
	}
	host, ok := m.currentHost()
	if !ok {
		m.message = "no host to activate an asset for"
		return m
	}
	concept, logicalID, ok := firstActivatableCandidate(m.Artifact, host)
	if !ok {
		m.message = fmt.Sprintf("%s: no AVAILABLE skill or mcpServer asset to activate", host)
		return m
	}

	result, err := stageAssetActivation(m.actions, host, concept, logicalID, m.now())
	if err != nil {
		m.message = fmt.Sprintf("%s: staging %s %q failed: %v", host, concept, logicalID, err)
		return m
	}

	review := buildChangeReview(host, result.CurrentGen, result.PendingGen)
	m.review = &review
	m.pendingGen = result.PendingGen
	m.mode = modeConfirm
	m.message = fmt.Sprintf("%s: staged pending generation %s enabling %s %q — review before activating", host, result.PendingGen.Metadata.ID, concept, logicalID)
	return m
}

// approveReview implements issue #35's "one human approval can execute a
// complete reviewed Change Set": a single 'y'/enter keypress marks EVERY
// change m.review lists as confirmed (changeReview.approveAll) and, in one
// call, proceeds through runtime.RequireConfirmation and runtime.
// ActivateAndVerify (activateHost) -- mirroring the CLI's own
// multiple-"--confirm"-flags-reviewed-together-then-one-"omca
// activate"-call semantics, just interactively. Also computes and stores
// issue #35's "restart_required per host" signal for the just-activated
// host, and refreshes m.Artifact (refreshArtifact) so every other view
// reflects the change immediately.
func (m Model) approveReview() Model {
	if m.review == nil {
		m.mode = modeBrowse
		return m
	}
	review := *m.review
	confirmed := review.approveAll()
	now := m.now()

	res, err := activateHost(m.actions, review, m.pendingGen, confirmed, now)
	m.mode = modeBrowse
	m.review = nil
	if err != nil {
		m.message = fmt.Sprintf("%s: activation failed: %v", review.Host, err)
		return m
	}

	if res.RolledBack {
		m.message = fmt.Sprintf("%s: post-activation verification FAILED (%s) — automatically rolled back to %s", review.Host, res.Verification.Detail, res.Rollback.RestoredGenerationID)
	} else {
		m.message = fmt.Sprintf("%s: activated %s (previous: %q)", review.Host, res.Activation.ActivatedGenerationID, res.Activation.PreviousGenerationID)
	}

	if statuses := restartStatusesForHosts(m.actions, []string{review.Host}); len(statuses) > 0 {
		m.restartStatuses = statuses
		for _, s := range statuses {
			if s.RestartRequired {
				m.message += fmt.Sprintf(" — RESTART REQUIRED: %s", s.Detail)
			}
		}
	}

	if fresh, rerr := refreshArtifact(m.actions, now); rerr == nil {
		m.Artifact = fresh
	}
	return m
}

// rollbackSelected implements the rollback flow (issue #35's own AC list
// names rollback as one of the flows needing a scripted interaction test)
// for the 'r' key, meaningful from the Generations view: restores the
// current host cursor's parent generation via the exact same runtime.
// Rollback cmd/omca/rollback.go's runRollback calls (rollbackHost,
// actions.go), then refreshes m.Artifact so the Generations view reflects
// the restored "current" immediately.
func (m Model) rollbackSelected() Model {
	if m.active != ViewGenerations {
		return m
	}
	if !m.actions.enabled() {
		m.message = "actions are not available: no worktree/action context attached to this TUI session"
		return m
	}
	host, ok := m.currentHost()
	if !ok {
		m.message = "no host to roll back"
		return m
	}

	now := m.now()
	result, err := rollbackHost(m.actions, host, now)
	if err != nil {
		m.message = fmt.Sprintf("%s: rollback failed: %v", host, err)
		return m
	}
	m.message = fmt.Sprintf("%s: rolled back to %s, superseding %s", host, result.RestoredGenerationID, result.SupersededGenerationID)

	if fresh, rerr := refreshArtifact(m.actions, now); rerr == nil {
		m.Artifact = fresh
	}
	return m
}

func indexOf(v ViewKind) int {
	for i, candidate := range viewOrder {
		if candidate == v {
			return i
		}
	}
	return 0
}

// View renders the tab bar plus the active view's content — the pure
// function doc.go's "Library choice" section documents as this package's
// entire snapshot-testing strategy: no terminal, no event loop, just
// Model -> string. While mode == modeConfirm, the reviewed Change Set
// screen (confirm.go's renderConfirmScreen) replaces the active view
// entirely, forcing the operator to see the whole Change Set before any
// other content is shown again.
//
// Every addition this method makes beyond PR-30's original
// renderTabBar+renderActive shape is gated on state a caller must
// deliberately opt into (a non-empty m.message, mode == modeConfirm, or
// m.actions.enabled() while on the Assets view) — every existing golden
// test in this package constructs a Model via plain NewModel(a) with none
// of those set, so View()'s byte-for-byte output for them is completely
// unchanged from PR-30.
func (m Model) View() string {
	if m.mode == modeConfirm && m.review != nil {
		return renderTabBar(m.active) + "\n\n" + renderConfirmScreen(*m.review)
	}
	if m.mode == modeDebug {
		return renderTabBar(m.active) + "\n\n" + m.renderDebugView()
	}

	out := renderTabBar(m.active) + "\n\n"
	if m.message != "" {
		out += m.message + "\n\n"
	}
	out += m.renderActive()
	if m.actions.enabled() && m.active == ViewAssets {
		if host, ok := m.currentHost(); ok {
			out += "\n" + selectionHintLine(m.Artifact, host)
		}
	}
	if m.active == ViewDrift {
		if hint := driftSelectionHintLine(m); hint != "" {
			out += "\n" + hint
		}
	}
	return out
}

// renderDebugView dispatches to renderDebugMatrix/renderDebugEntity
// (debug.go) according to m.debugLevel -- the modeDebug half of View(),
// mirroring renderActive's own dispatch-by-enum shape for the four
// read-only views.
func (m Model) renderDebugView() string {
	if m.debugLevel == debugLevelEntity {
		return renderDebugEntity(m.Artifact, m.debugCardID, m.debugHost, m.debugConcept, m.debugLogicalID, m.debugShowPlanes)
	}
	card, ok := findCardByID(m.Artifact, m.debugCardID)
	if !ok {
		return "Debug: action card " + m.debugCardID + " no longer exists in the current report"
	}
	return renderDebugMatrix(m.Artifact, card, m.debugMatrixCursor, m.debugShowPlanes)
}

// driftSelectionHintLine tells the operator which action card up/down
// currently selects and how to open its Debug view -- shown unconditionally
// on the Drift view (unlike selectionHintLine's own ActionContext gate:
// drilling into Debug is read-only navigation, never a mutating action, so
// it needs no ActionContext attached to be meaningful). Empty when there is
// no drift at all, so the "No drift" empty-state message (RenderDrift) is
// not followed by a confusing "nothing to select" line.
func driftSelectionHintLine(m Model) string {
	card, ok := m.currentDriftCard()
	if !ok {
		return ""
	}
	return fmt.Sprintf("Selected %s -- press enter to open its Debug view (Host Matrix -> resolver trace -> evidence) -- up/down to change card", card.ID)
}

func (m Model) renderActive() string {
	switch m.active {
	case ViewOverview:
		return RenderOverview(m.Artifact)
	case ViewDrift:
		return RenderDrift(m.Artifact)
	case ViewAssets:
		return RenderAssets(m.Artifact)
	case ViewGenerations:
		return RenderGenerations(m.Artifact)
	default:
		return ""
	}
}

// selectionHintLine tells the operator which host the 'a' key currently
// acts on and what it would activate (firstActivatableCandidate), or that
// there is nothing to activate for this host -- shown only when
// ActionContext is attached (View's own doc comment), so it never appears
// in this package's plain, action-less golden tests.
func selectionHintLine(a report.Artifact, host string) string {
	concept, logicalID, ok := firstActivatableCandidate(a, host)
	if !ok {
		return fmt.Sprintf("[%s] no AVAILABLE skill/mcpServer to activate — up/down or j/k to change host", host)
	}
	return fmt.Sprintf("[%s] press 'a' to activate %s %q — up/down or j/k to change host", host, concept, logicalID)
}
