package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// This file is issue #36's own explicit, non-negotiable bar (round-3 final
// review addendum): scripted tests that walk action card -> matrix ->
// resolver trace -> evidence END TO END against the committed fixture
// artifact (fixture_test.go's real report.Build pipeline over the
// fixtures/codex/0.144.5/mcp-merge corpus, the same real SOURCE_DRIFT/
// Conflict content drift_test.go's own doc comment describes), proving
// docs/architecture/reporting.md §14's debug invariants actually hold for
// this Debug drill-down UI rather than merely asserting it renders
// something.

func keyEnter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func keyEsc() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }

// driveToDebugMatrix builds a Model over the fixture artifact, active on
// Drift, and presses enter once -- debugLevelMatrix for the fixture's one
// real SOURCE_DRIFT action card (DR-8105bca2).
func driveToDebugMatrix(t *testing.T) Model {
	t.Helper()
	a := loadFixtureArtifact(t)
	m := NewModel(a).SetActive(ViewDrift)
	next, _ := m.Update(keyEnter())
	return next.(Model)
}

// driveToDebugEntity presses enter a second time from driveToDebugMatrix's
// own state -- debugLevelEntity for that card's one Matrix row
// (mcp_server/stdio|shared-tools on host codex, an unresolved
// effective.Conflict per fixture_test.go's doc comment).
func driveToDebugEntity(t *testing.T) Model {
	t.Helper()
	m := driveToDebugMatrix(t)
	next, _ := m.Update(keyEnter())
	return next.(Model)
}

func TestDebugMatrixView_MatchesGolden(t *testing.T) {
	m := driveToDebugMatrix(t)
	compareGolden(t, "debug_matrix.golden", m.View())
}

func TestDebugEntityView_MatchesGolden(t *testing.T) {
	m := driveToDebugEntity(t)
	compareGolden(t, "debug_entity.golden", m.View())
}

func TestDebugEntityView_WithPlanesToggle_MatchesGolden(t *testing.T) {
	m := driveToDebugEntity(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	compareGolden(t, "debug_entity_planes.golden", next.(Model).View())
}

// TestDebugDrillDown_ActionCardToMatrixToTraceToEvidence_EndToEnd is this
// issue's own literal, non-negotiable bar: walk action card -> Host Matrix
// -> resolver trace -> Evidence -> Native Artifacts against the committed
// fixture artifact, asserting the REAL content report.Build/report.Explain/
// report.ComparePlanes computed for the mcp-merge corpus's genuine
// "shared-tools" collision at every step -- not just that some golden file
// happens to match byte-for-byte, but that docs/architecture/reporting.md
// §14's debug invariants ("every action card expands to all affected
// cells," "every cell expands to desired and observed values," "every
// effective value expands to its resolver trace," "every resolver trace
// expands to physical sources and Knowledge evidence") are each backed by a
// specific, checkable assertion below.
func TestDebugDrillDown_ActionCardToMatrixToTraceToEvidence_EndToEnd(t *testing.T) {
	a := loadFixtureArtifact(t)
	if len(a.ActionCards) == 0 {
		t.Fatal("fixture artifact has no ActionCards -- this test needs a real drift card to drill into")
	}
	wantCardID := a.ActionCards[0].ID

	// Step 0: Drift view names the card up/down would drill into.
	m := NewModel(a).SetActive(ViewDrift)
	if hint := driftSelectionHintLine(m); !contains(hint, wantCardID) || !contains(hint, "enter") {
		t.Fatalf("driftSelectionHintLine() = %q, want it to name %q and mention pressing enter", hint, wantCardID)
	}

	// Step 1: action card -> Host Matrix ("every action card expands to all
	// affected cells").
	next, cmd := m.Update(keyEnter())
	if cmd != nil {
		t.Fatalf("Update(enter) on Drift returned a non-nil Cmd, want nil")
	}
	m = next.(Model)
	if m.mode != modeDebug || m.debugLevel != debugLevelMatrix {
		t.Fatalf("after entering debug: mode=%v debugLevel=%v, want modeDebug/debugLevelMatrix", m.mode, m.debugLevel)
	}
	if m.debugCardID != wantCardID {
		t.Errorf("debugCardID = %q, want %q", m.debugCardID, wantCardID)
	}
	card, ok := findCardByID(a, wantCardID)
	if !ok {
		t.Fatalf("findCardByID(%q) = false", wantCardID)
	}
	if len(card.Matrix) == 0 {
		t.Fatal("card.Matrix is empty -- this test needs at least one real cell")
	}
	wantRow := card.Matrix[0]
	matrixView := m.View()
	if !contains(matrixView, "Host Matrix") {
		t.Errorf("matrix view missing 'Host Matrix' section:\n%s", matrixView)
	}
	if !contains(matrixView, wantRow.EntityID) {
		t.Errorf("matrix view missing the real cell's EntityID %q:\n%s", wantRow.EntityID, matrixView)
	}
	// "every cell expands to desired and observed values": RenderMatrixHuman
	// already prints Expected/Observed for every row -- check the real
	// values from this exact row are present, not placeholders.
	if !contains(matrixView, "expected=") || !contains(matrixView, "observed=") {
		t.Errorf("matrix view does not show expected/observed values for its cells:\n%s", matrixView)
	}

	// Step 2: cell -> Effective State + resolver trace ("every effective
	// value expands to its resolver trace").
	next, cmd = m.Update(keyEnter())
	if cmd != nil {
		t.Fatalf("Update(enter) in debugLevelMatrix returned a non-nil Cmd, want nil")
	}
	m = next.(Model)
	if m.debugLevel != debugLevelEntity {
		t.Fatalf("debugLevel after drilling into the cell = %v, want debugLevelEntity", m.debugLevel)
	}
	wantConcept, wantLogicalID, ok := splitEntityID(wantRow.EntityID)
	if !ok {
		t.Fatalf("splitEntityID(%q) failed", wantRow.EntityID)
	}
	if m.debugHost != wantRow.Host || m.debugConcept != wantConcept || m.debugLogicalID != wantLogicalID {
		t.Errorf("drilled-into entity = host=%q concept=%q logicalID=%q, want host=%q concept=%q logicalID=%q",
			m.debugHost, m.debugConcept, m.debugLogicalID, wantRow.Host, wantConcept, wantLogicalID)
	}

	result := report.Explain(a, wantRow.Host, wantConcept, wantLogicalID, true)
	if !result.Found {
		t.Fatalf("report.Explain(%s, %s, %s) not Found -- fixture must resolve this entity", wantRow.Host, wantConcept, wantLogicalID)
	}
	if result.Trace == nil {
		t.Fatal("report.Explain(..., trace=true) returned a nil Trace -- this test needs a real resolver trace")
	}

	entityView := m.View()
	if !contains(entityView, "Effective State & Precedence Trace") {
		t.Errorf("entity view missing 'Effective State & Precedence Trace' section:\n%s", entityView)
	}
	if !contains(entityView, "Resolver trace:") {
		t.Errorf("entity view missing the resolver trace:\n%s", entityView)
	}

	// Step 3: resolver trace -> physical sources ("every resolver trace
	// expands to physical sources and Knowledge evidence").
	if len(result.Trace.PhysicalSources) == 0 {
		t.Fatal("resolver trace has no PhysicalSources -- this test needs at least one real physical source")
	}
	if !contains(entityView, "Physical sources:") {
		t.Errorf("entity view missing 'Physical sources:':\n%s", entityView)
	}
	for _, ps := range result.Trace.PhysicalSources {
		if !contains(entityView, ps.Ref) {
			t.Errorf("entity view missing physical source ref %q:\n%s", ps.Ref, entityView)
		}
	}

	// Step 3b: resolver trace -> Knowledge evidence (same invariant, the
	// Knowledge Pack citation half).
	if len(result.Trace.KnowledgeEvidence) == 0 {
		t.Fatal("resolver trace has no KnowledgeEvidence -- this test needs at least one real Knowledge citation")
	}
	if !contains(entityView, "Knowledge evidence:") {
		t.Errorf("entity view missing 'Knowledge evidence:':\n%s", entityView)
	}
	for _, ke := range result.Trace.KnowledgeEvidence {
		if !contains(entityView, ke.ID) {
			t.Errorf("entity view missing Knowledge evidence ref %q:\n%s", ke.ID, entityView)
		}
	}

	// Step 4: this Debug tree's own distinct "Evidence" pane -- real
	// per-claim domain.Evidence records for exactly this entity, a
	// different dataset than the Knowledge citations above.
	hd := a.Debug[wantRow.Host]
	records := evidenceForEntity(hd, wantConcept, wantLogicalID)
	if len(records) == 0 {
		t.Fatal("evidenceForEntity found no Evidence record -- this test needs at least one real per-entity Evidence record")
	}
	if !contains(entityView, "Evidence") {
		t.Errorf("entity view missing the 'Evidence' section:\n%s", entityView)
	}
	for _, e := range records {
		if !contains(entityView, e.Metadata.ID) {
			t.Errorf("entity view missing Evidence record ID %q:\n%s", e.Metadata.ID, entityView)
		}
	}

	// Step 5: "Native Artifacts" -- the same physical sources joined against
	// the raw Observation records they were extracted from.
	if !contains(entityView, "Native Artifacts") {
		t.Errorf("entity view missing the 'Native Artifacts' section:\n%s", entityView)
	}
	var sawJoinedObservation bool
	for _, ps := range result.Trace.PhysicalSources {
		for _, o := range observationsForPath(hd.Observations, ps.Path) {
			sawJoinedObservation = true
			if !contains(entityView, o.Metadata.ID) {
				t.Errorf("entity view missing joined Observation ID %q:\n%s", o.Metadata.ID, entityView)
			}
		}
	}
	if !sawJoinedObservation {
		t.Fatal("no PhysicalSource joined to a real Observation -- this test needs at least one real join to be meaningful")
	}

	// Step 6: the "Native vs Current vs Pending" pane toggles on/off and
	// shows a real plane comparison for the drilled-into host.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = next.(Model)
	if !m.debugShowPlanes {
		t.Fatal("debugShowPlanes did not toggle on after pressing 'p'")
	}
	withPlanes := m.View()
	if !contains(withPlanes, "Native vs Current vs Pending") || !contains(withPlanes, "NATIVE vs CURRENT") || !contains(withPlanes, "CURRENT vs PENDING") {
		t.Errorf("view with planes toggled on is missing the Native vs Current vs Pending pane:\n%s", withPlanes)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = next.(Model)
	if m.debugShowPlanes {
		t.Fatal("debugShowPlanes did not toggle back off after pressing 'p' again")
	}
	// The footer hint always names "Native vs Current vs Pending" (it is the
	// key's own label, shown whether or not the pane is currently open) --
	// check for the pane's actual rendered content instead, which must
	// disappear once toggled off.
	if contains(m.View(), "NATIVE vs CURRENT") || contains(m.View(), "CURRENT vs PENDING") {
		t.Errorf("view still shows the planes pane's content after toggling it back off:\n%s", m.View())
	}

	// Step 7: esc/b steps back one level at a time, never straight past the
	// Drift view in one keypress.
	next, cmd = m.Update(keyEsc())
	if cmd != nil {
		t.Fatalf("Update(esc) at debugLevelEntity returned a non-nil Cmd, want nil")
	}
	m = next.(Model)
	if m.mode != modeDebug || m.debugLevel != debugLevelMatrix {
		t.Fatalf("after one esc from debugLevelEntity: mode=%v debugLevel=%v, want modeDebug/debugLevelMatrix", m.mode, m.debugLevel)
	}
	if !contains(m.View(), "Host Matrix") {
		t.Errorf("view after stepping back is not the matrix view:\n%s", m.View())
	}

	next, _ = m.Update(keyEsc())
	m = next.(Model)
	if m.mode != modeBrowse || m.active != ViewDrift {
		t.Fatalf("after second esc: mode=%v active=%v, want modeBrowse/ViewDrift", m.mode, m.active)
	}
}

// TestDebugMode_NoActionCards_ReportsMessage proves enterDebug's own honest
// "nothing to do" path: an Artifact with no drift leaves Model in
// modeBrowse with an explanatory message, never a confirm/debug screen for
// nothing (mirroring TestModel_ActivateSelected_NoAvailableAsset_
// ReportsMessage's identical discipline for the Assets view's own action).
func TestDebugMode_NoActionCards_ReportsMessage(t *testing.T) {
	m := NewModel(emptyArtifactForTest()).SetActive(ViewDrift)
	next, _ := m.Update(keyEnter())
	m = next.(Model)
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse (no drift to drill into)", m.mode)
	}
	if !contains(m.message, "no action card") {
		t.Errorf("message = %q, want it to explain there is no action card to drill into", m.message)
	}
}

// TestDebugMode_EnterOutsideDriftView_IsInert proves enter is only
// meaningful on the Drift view -- pressing it elsewhere must not open the
// Debug drill-down over whatever action card driftCursor happens to
// address, since the operator never chose one.
func TestDebugMode_EnterOutsideDriftView_IsInert(t *testing.T) {
	m := NewModel(loadFixtureArtifact(t)).SetActive(ViewOverview)
	next, _ := m.Update(keyEnter())
	m = next.(Model)
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse (enter pressed outside the Drift view)", m.mode)
	}
}

// TestDebugMode_QuitStillQuits proves ctrl+c/q still quit the program even
// mid-drill-down, matching updateConfirm's own identical contract.
func TestDebugMode_QuitStillQuits(t *testing.T) {
	m := driveToDebugMatrix(t)
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("Update(ctrl+c) in modeDebug returned a nil Cmd, want tea.Quit")
	}

	m2 := driveToDebugEntity(t)
	if _, cmd := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); cmd == nil {
		t.Error("Update(q) in modeDebug (debugLevelEntity) returned a nil Cmd, want tea.Quit")
	}
}

// TestDebugMode_BKeyIsEscAlias proves 'b' ("back") behaves identically to
// esc at both drill-down levels.
func TestDebugMode_BKeyIsEscAlias(t *testing.T) {
	m := driveToDebugEntity(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = next.(Model)
	if m.debugLevel != debugLevelMatrix {
		t.Errorf("debugLevel after 'b' = %v, want debugLevelMatrix", m.debugLevel)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = next.(Model)
	if m.mode != modeBrowse {
		t.Errorf("mode after second 'b' = %v, want modeBrowse", m.mode)
	}
}

// TestDebugMode_MatrixCursor_UpDownWrap proves debugLevelMatrix's up/down
// (k/j) cursor wraps across card.Matrix, matching hostCursor/driftCursor's
// own identical wrapping convention (TestModel_HostCursor_WrapsBothDirections).
// The fixture's one card has exactly one Matrix row, so up/down must wrap
// to that same row rather than drifting out of range.
func TestDebugMode_MatrixCursor_UpDownWrap(t *testing.T) {
	m := driveToDebugMatrix(t)
	card, ok := findCardByID(m.Artifact, m.debugCardID)
	if !ok || len(card.Matrix) != 1 {
		t.Fatalf("fixture's card.Matrix = %+v, want exactly 1 row for this test", card.Matrix)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := next.(Model)
	if !contains(m2.View(), "Selected [1/1]") {
		t.Errorf("view after down = %q, want it to still show Selected [1/1]", m2.View())
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m3 := next.(Model)
	if !contains(m3.View(), "Selected [1/1]") {
		t.Errorf("view after up = %q, want it to still show Selected [1/1]", m3.View())
	}
}

// TestModel_DriftCursor_WrapsBothDirections mirrors
// TestModel_HostCursor_WrapsBothDirections for the Drift view's own
// driftCursor (issue #36's addition): the fixture has exactly one
// ActionCard, so up/down (on the Drift view specifically) must wrap to that
// same card, and must NOT move hostCursor instead.
func TestModel_DriftCursor_WrapsBothDirections(t *testing.T) {
	a := loadFixtureArtifact(t)
	m := NewModel(a).SetActive(ViewDrift)
	card, ok := m.currentDriftCard()
	if !ok || card.ID != a.ActionCards[0].ID {
		t.Fatalf("currentDriftCard() = %+v, %v, want the fixture's one card", card, ok)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := next.(Model)
	card2, ok2 := m2.currentDriftCard()
	if !ok2 || card2.ID != card.ID {
		t.Errorf("currentDriftCard() after down = %+v, %v, want the same single card (must wrap)", card2, ok2)
	}
	if m2.hostCursor != 0 {
		t.Errorf("hostCursor changed to %d from a Drift-view down keypress, want unaffected", m2.hostCursor)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m3 := next.(Model)
	card3, ok3 := m3.currentDriftCard()
	if !ok3 || card3.ID != card.ID {
		t.Errorf("currentDriftCard() after up = %+v, %v, want the same single card (must wrap)", card3, ok3)
	}
}

// TestSplitEntityID covers splitEntityID's contract, including the case
// that motivates splitting on the FIRST "/" rather than the last: a
// logicalID that itself contains a slash (a real instruction identity
// shape, "scope|source.path").
func TestSplitEntityID(t *testing.T) {
	cases := []struct {
		entityID      string
		wantConcept   string
		wantLogicalID string
		wantOK        bool
	}{
		{"mcp_server/stdio|shared-tools", "mcp_server", "stdio|shared-tools", true},
		{"instruction/user|.claude/CLAUDE.md", "instruction", "user|.claude/CLAUDE.md", true},
		{"no-slash-here", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		concept, logicalID, ok := splitEntityID(c.entityID)
		if concept != c.wantConcept || logicalID != c.wantLogicalID || ok != c.wantOK {
			t.Errorf("splitEntityID(%q) = %q, %q, %v; want %q, %q, %v",
				c.entityID, concept, logicalID, ok, c.wantConcept, c.wantLogicalID, c.wantOK)
		}
	}
}

// TestEvidenceForEntity_FiltersBySubject proves the filter matches only
// records whose Subject names exactly (concept, logicalID), using small
// hand-built records rather than the fixture -- an explicit unit test for
// the join logic itself, isolated from the fixture's own real content.
func TestEvidenceForEntity_FiltersBySubject(t *testing.T) {
	hd := report.HostDebug{
		Evidence: []domain.Evidence{
			{Metadata: domain.Metadata{ID: "match"}, Spec: domain.EvidenceSpec{Subject: domain.EvidenceSubject{Concept: "mcp_server", LogicalID: "stdio|shared-tools"}}},
			{Metadata: domain.Metadata{ID: "wrong-concept"}, Spec: domain.EvidenceSpec{Subject: domain.EvidenceSubject{Concept: "skill", LogicalID: "stdio|shared-tools"}}},
			{Metadata: domain.Metadata{ID: "wrong-id"}, Spec: domain.EvidenceSpec{Subject: domain.EvidenceSubject{Concept: "mcp_server", LogicalID: "other"}}},
		},
	}
	got := evidenceForEntity(hd, "mcp_server", "stdio|shared-tools")
	if len(got) != 1 || got[0].Metadata.ID != "match" {
		t.Errorf("evidenceForEntity = %+v, want exactly the one matching record", got)
	}
}

// TestEvidenceSourceString_NeverGluesURLAndPathTogether is a regression test
// (Copilot review finding on this PR): an earlier version printed
// "%s%s" with URL then Path directly, so a record carrying both fields
// concatenated into one ambiguous, unseparated token. URL is preferred when
// both are set, with an unambiguous separator; Path is the fallback when no
// URL is recorded; neither set renders an empty string.
func TestEvidenceSourceString_NeverGluesURLAndPathTogether(t *testing.T) {
	cases := []struct {
		name string
		src  domain.EvidenceSource
		want string
	}{
		{"both set", domain.EvidenceSource{URL: "https://example.com/docs", Path: "hosts/codex/cli/config.toml"}, "https://example.com/docs (hosts/codex/cli/config.toml)"},
		{"url only", domain.EvidenceSource{URL: "https://example.com/docs"}, "https://example.com/docs"},
		{"path only", domain.EvidenceSource{Path: "hosts/codex/cli/config.toml"}, "hosts/codex/cli/config.toml"},
		{"neither set", domain.EvidenceSource{}, ""},
	}
	for _, c := range cases {
		if got := evidenceSourceString(c.src); got != c.want {
			t.Errorf("%s: evidenceSourceString(%+v) = %q, want %q", c.name, c.src, got, c.want)
		}
	}
}

// TestObservationsForPath_MatchesSourcePathAndHandlesEmpty proves the
// path-join helper matches only Observations whose own Source.Path equals
// path, and returns nil (not a panic or a false match) for an empty path --
// the honest degrade an unresolved PhysicalSource.Path (no path known,
// e.g. an IgnoredSources-only ref) must produce.
func TestObservationsForPath_MatchesSourcePathAndHandlesEmpty(t *testing.T) {
	obs := []domain.Observation{
		{Metadata: domain.Metadata{ID: "match"}, Spec: domain.ObservationSpec{Source: domain.ObservationSource{Path: "/a/config.toml"}}},
		{Metadata: domain.Metadata{ID: "other"}, Spec: domain.ObservationSpec{Source: domain.ObservationSource{Path: "/b/config.toml"}}},
	}
	got := observationsForPath(obs, "/a/config.toml")
	if len(got) != 1 || got[0].Metadata.ID != "match" {
		t.Errorf("observationsForPath(/a/config.toml) = %+v, want exactly the one matching record", got)
	}
	if got := observationsForPath(obs, ""); got != nil {
		t.Errorf("observationsForPath(\"\") = %+v, want nil", got)
	}
}
