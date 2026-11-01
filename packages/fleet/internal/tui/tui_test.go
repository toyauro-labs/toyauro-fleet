package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/toyauro-labs/toyauro-fleet/packages/fleet/internal/fleet"
)

// fakeBackend records what the view asked for. Nothing connects anywhere.
type fakeBackend struct {
	sent, replied, acked, jumped []string
	sendErr                      error
}

func (f *fakeBackend) Load(fleet.Node) ([]fleet.Pane, []fleet.Message, error) { return nil, nil, nil }

func (f *fakeBackend) Send(t fleet.Target, to, from, body string) (fleet.Receipt, error) {
	if f.sendErr != nil {
		return fleet.Receipt{}, f.sendErr
	}
	f.sent = append(f.sent, t.Host+" "+to+" "+from+" "+body)
	return fleet.Receipt{Stored: true, ID: "abcdef0123456789", ToPane: to}, nil
}

func (f *fakeBackend) Reply(t fleet.Target, pane, id, from, body string) (fleet.Receipt, error) {
	f.replied = append(f.replied, pane+" "+id+" "+from+" "+body)
	return fleet.Receipt{Stored: true, ID: "abcdef0123456789", ToPane: "%9"}, nil
}

func (f *fakeBackend) Ack(t fleet.Target, pane, id string) error {
	f.acked = append(f.acked, pane+" "+id)
	return nil
}

func (f *fakeBackend) Jump(p fleet.DevicePane) tea.Cmd {
	f.jumped = append(f.jumped, p.Device+" "+p.Pane.Pane)
	return nil
}

func panes() []fleet.Pane {
	return []fleet.Pane{
		{Session: "s", Window: "0", PaneIndex: "0", Pane: "%1", Command: "pi", Name: "phone use", State: "idle"},
		{Session: "s", Window: "0", PaneIndex: "1", Pane: "%2", Command: "zsh"},
		{Session: "s", Window: "1", PaneIndex: "0", Pane: "%3", Command: "claude", Name: "手机工位", State: "working"},
	}
}

func viewModel(t *testing.T) (*model, *fakeBackend) {
	t.Helper()
	t.Setenv("TMUX", "/tmp/tmux-0/mine,1,0")
	t.Setenv("TMUX_PANE", "%7")
	f := &fakeBackend{}
	m := newModel([]fleet.Node{
		{Name: "mac", Server: "vibe"},
		{Name: "phone", Host: "ace-ts", Server: "default", ReturnHost: "macmini"},
	}, f)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(loadedMsg{node: 0, panes: panes(), messages: []fleet.Message{
		{ID: "11111111aaaaaaaa", ToPane: "%1", From: "./vibe/%3", Body: "first\nsecond line", Created: "2026-11-01T10:00:00.000000+00:00"},
	}})
	m.Update(loadedMsg{node: 1, panes: []fleet.Pane{{Pane: "%5", Name: "remote", State: "waiting", Session: "d", Window: "0", PaneIndex: "0"}},
		messages: []fleet.Message{{ID: "22222222bbbbbbbb", ToPane: "%5", From: "macmini/vibe/%7", Body: "hi"}}})
	return m, f
}

func press(t *testing.T, m *model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		_, cmd := m.Update(msg)
		runCmd(m, cmd)
	}
}

// runCmd delivers a command's result, as the runtime would. Timers and focus
// blinks are not part of what these tests check, so batches are left alone.
func runCmd(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch out := cmd().(type) {
	case sentMsg, ackedMsg:
		_, next := m.Update(out)
		if next != nil {
			m.Update(next())
		}
	}
}

func selectRow(t *testing.T, m *model, pane string) {
	t.Helper()
	for i, r := range m.rows {
		if r.kind == rowPane && r.pane.Pane.Pane == pane {
			m.cursor = i
			return
		}
	}
	t.Fatalf("no row for %s in %+v", pane, m.rows)
}

func TestLoadsListWorkstationsAndHideBareShells(t *testing.T) {
	m, _ := viewModel(t)
	out := m.View()
	for _, want := range []string{"phone use", "手机工位", "remote", "mac", "phone", "● working", "3 workstations"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "%2") {
		t.Errorf("unnamed shell shown by default:\n%s", out)
	}
	press(t, m, "a")
	if !strings.Contains(m.View(), "%2") {
		t.Errorf("a should show every pane:\n%s", m.View())
	}
}

func TestDeviceWithOnlyBareShellsStaysVisible(t *testing.T) {
	m, _ := viewModel(t)
	bare := []fleet.Pane{{Pane: "%5", Command: "zsh"}, {Pane: "%6", Command: "zsh"}}
	m.Update(loadedMsg{node: 1, panes: bare})
	out := m.View()
	if !strings.Contains(out, "phone") || !strings.Contains(out, "2 unnamed") {
		t.Fatalf("reachable device vanished from the table:\n%s", out)
	}
	if strings.Contains(out, "unreachable") {
		t.Errorf("a reachable device must not read as down:\n%s", out)
	}
	m.cursor = len(m.rows) - 1
	press(t, m, "s")
	if m.screen != screenTable || !strings.Contains(m.flash, "press a") {
		t.Errorf("acting on the device row should point at a, screen=%v flash=%q", m.screen, m.flash)
	}
	press(t, m, "a")
	if out := m.View(); strings.Contains(out, "unnamed") || !strings.Contains(out, "%6") {
		t.Errorf("a should replace the device row with its panes:\n%s", out)
	}
}

func TestViewSaysWhenItCoversLessThanTheFleet(t *testing.T) {
	m, _ := viewModel(t)
	m.scope = "this device only"
	if !strings.Contains(m.View(), "this device only") {
		t.Errorf("scope missing from the title:\n%s", m.View())
	}
}

func TestCursorFollowsPaneAcrossRefresh(t *testing.T) {
	m, _ := viewModel(t)
	selectRow(t, m, "%3")
	// A pane above it closes between refreshes.
	m.Update(loadedMsg{node: 0, panes: panes()[2:]})
	if r, _ := m.selected(); r.pane.Pane.Pane != "%3" {
		t.Fatalf("cursor moved to %+v", r)
	}
}

func TestUnreachableDeviceDropsStaleLamps(t *testing.T) {
	m, _ := viewModel(t)
	selectRow(t, m, "%5")
	m.Update(loadedMsg{node: 1, err: errors.New("ssh: connect timed out")})
	out := m.View()
	if strings.Contains(out, "remote") {
		t.Errorf("stale workstation still shown as live:\n%s", out)
	}
	if !strings.Contains(out, "unreachable") {
		t.Errorf("unreachable device not visible:\n%s", out)
	}
	if r, _ := m.selected(); r.kind != rowDown {
		t.Errorf("cursor should stay on the device, got %+v", r)
	}
	press(t, m, "s")
	if m.screen != screenTable || !strings.Contains(m.flash, "no panes") {
		t.Errorf("sending to a down device should explain itself, screen=%v flash=%q", m.screen, m.flash)
	}
	press(t, m, "d")
	if !strings.Contains(m.View(), "connect timed out") {
		t.Errorf("detail should show the error:\n%s", m.View())
	}
	m.Update(loadedMsg{node: 1, panes: []fleet.Pane{{Pane: "%5", Name: "remote"}}})
	if !strings.Contains(m.View(), "remote") {
		t.Errorf("device should return after recovering:\n%s", m.View())
	}
}

func TestPeerControlledTextCannotReachTerminal(t *testing.T) {
	m, _ := viewModel(t)
	evil := "\x1b]0;pwned\x07\x1b[2Jname"
	m.Update(loadedMsg{node: 0, panes: []fleet.Pane{{Pane: "%1", Name: evil, State: evil, Command: evil, Session: evil}},
		messages: []fleet.Message{{ID: "abc", ToPane: "%1", From: evil, Body: evil + "\nsecond"}}})
	selectRow(t, m, "%1")
	press(t, m, "d")
	check := func(screen, out string) {
		t.Helper()
		// lipgloss renders no styles in tests, so any ESC or BEL here came from a peer.
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Errorf("%s leaked control characters: %q", screen, out)
		}
	}
	check("table", m.View())
	press(t, m, "i")
	check("inbox", m.View())
	if !strings.Contains(m.View(), "second") {
		t.Errorf("message body should keep its lines:\n%s", m.View())
	}
}

func TestSendComposesWithSelfAddress(t *testing.T) {
	m, f := viewModel(t)
	selectRow(t, m, "%5")
	press(t, m, "s", "你好", "enter", "world", "ctrl+s")
	if len(f.sent) != 1 || f.sent[0] != "ace-ts %5 macmini/mine/%7 你好\nworld" {
		t.Fatalf("sent %q", f.sent)
	}
	if m.screen != screenTable || !strings.Contains(m.notice, "not a read receipt") {
		t.Errorf("screen=%v notice=%q", m.screen, m.notice)
	}
}

func TestFailedSendKeepsDraftAndDoesNotDoubleSend(t *testing.T) {
	m, f := viewModel(t)
	f.sendErr = errors.New("pane does not exist")
	selectRow(t, m, "%1")
	press(t, m, "s", "draft", "ctrl+s")
	if m.screen != screenCompose || m.compose.body.Value() != "draft" || !strings.Contains(m.flash, "not sent") {
		t.Fatalf("draft lost: screen=%v body=%q flash=%q", m.screen, m.compose.body.Value(), m.flash)
	}
	f.sendErr = nil
	m.compose.sending = true
	if cmd := m.submit(); cmd != nil {
		t.Error("a second submit while one is in flight must not send")
	}
}

func TestEscAsksBeforeDiscardingDraft(t *testing.T) {
	m, _ := viewModel(t)
	selectRow(t, m, "%1")
	press(t, m, "s", "long draft", "esc")
	if m.screen != screenCompose || !strings.Contains(m.flash, "discard") {
		t.Fatalf("first esc should warn: screen=%v flash=%q", m.screen, m.flash)
	}
	press(t, m, "x", "esc")
	if m.screen != screenCompose {
		t.Fatal("typing should disarm the discard")
	}
	press(t, m, "esc")
	if m.screen != screenTable {
		t.Fatal("second esc should discard")
	}
	press(t, m, "s", "esc")
	if m.screen != screenTable {
		t.Fatal("an empty message needs no confirmation")
	}
}

func TestOutsideTmuxNoIdentityIsBorrowed(t *testing.T) {
	m, f := viewModel(t)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	selectRow(t, m, "%1")
	press(t, m, "s", "ctrl+s")
	if len(f.sent) != 0 || !m.compose.onFrom || !strings.Contains(m.flash, "HOST/SERVER/%PANE") {
		t.Fatalf("sent=%q onFrom=%v flash=%q", f.sent, m.compose.onFrom, m.flash)
	}
}

func TestAckNeedsSecondPress(t *testing.T) {
	m, f := viewModel(t)
	selectRow(t, m, "%1")
	press(t, m, "i", "a")
	if len(f.acked) != 0 || !strings.Contains(m.flash, "again") {
		t.Fatalf("first press acked=%q flash=%q", f.acked, m.flash)
	}
	press(t, m, "k", "a")
	if len(f.acked) != 0 {
		t.Fatal("moving should disarm the ack")
	}
	press(t, m, "a")
	if len(f.acked) != 1 || f.acked[0] != "%1 11111111aaaaaaaa" {
		t.Fatalf("acked %q", f.acked)
	}
}

func TestReplyOnlyFromTheDeviceHoldingTheInbox(t *testing.T) {
	m, f := viewModel(t)
	selectRow(t, m, "%5")
	press(t, m, "i", "r")
	if m.screen != screenInbox || !strings.Contains(m.flash, "resolve there") {
		t.Fatalf("remote reply should be refused: screen=%v flash=%q", m.screen, m.flash)
	}
	press(t, m, "esc")
	selectRow(t, m, "%1")
	press(t, m, "i", "r", "ok", "ctrl+s")
	if len(f.replied) != 1 || f.replied[0] != "%1 11111111aaaaaaaa ./mine/%7 ok" {
		t.Fatalf("replied %q", f.replied)
	}
	if m.screen != screenInbox {
		t.Errorf("reply should return to the inbox, got %v", m.screen)
	}
}

func TestEnterJumpsToPane(t *testing.T) {
	m, f := viewModel(t)
	selectRow(t, m, "%5")
	press(t, m, "enter")
	if len(f.jumped) != 1 || f.jumped[0] != "phone %5" {
		t.Fatalf("jumped %q", f.jumped)
	}
}

func TestEveryScreenFitsNarrowPhoneTerminals(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {60, 20}, {40, 24}, {28, 12}} {
		m, _ := viewModel(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		selectRow(t, m, "%3")
		check := func(screen string) {
			t.Helper()
			out := m.View()
			if w := lipgloss.Width(out); w > size[0] {
				t.Errorf("%dx%d %s: width %d:\n%s", size[0], size[1], screen, w, out)
			}
			if h := lipgloss.Height(out); h > size[1] {
				t.Errorf("%dx%d %s: height %d:\n%s", size[0], size[1], screen, h, out)
			}
			if screen == "table" && !strings.Contains(out, "手机") {
				t.Errorf("%dx%d: selected name gone:\n%s", size[0], size[1], out)
			}
		}
		check("table")
		press(t, m, "d")
		check("detail")
		press(t, m, "d")
		selectRow(t, m, "%1")
		press(t, m, "i")
		check("inbox")
		press(t, m, "esc", "s")
		check("compose")
	}
}

func TestFilterMatchesAcrossColumns(t *testing.T) {
	m, _ := viewModel(t)
	press(t, m, "/", "w", "o", "r", "k", "enter")
	if len(m.rows) != 1 || m.rows[0].pane.Pane.Pane != "%3" {
		t.Fatalf("rows %+v", m.rows)
	}
	press(t, m, "esc")
	if len(m.rows) != 3 {
		t.Fatalf("esc should clear the filter, rows %+v", m.rows)
	}
}
