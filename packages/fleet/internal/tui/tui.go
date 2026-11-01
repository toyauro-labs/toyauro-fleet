// Package tui is the interactive view for a person: a live table of every
// device's workstations, with inboxes, sending, and enter to jump to a pane.
// It uses the same mailbox as the agents and adds no state of its own.
package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/toyauro-labs/toyauro-fleet/packages/fleet/internal/fleet"
)

const refreshEvery = 5 * time.Second

// Backend is everything the view does to the outside world.
type Backend interface {
	Load(fleet.Node) ([]fleet.Pane, []fleet.Message, error)
	Send(t fleet.Target, to, from, body string) (fleet.Receipt, error)
	Reply(t fleet.Target, pane, id, from, body string) (fleet.Receipt, error)
	Ack(t fleet.Target, pane, id string) error
	Jump(fleet.DevicePane) tea.Cmd
}

type (
	loadedMsg struct {
		node     int
		panes    []fleet.Pane
		messages []fleet.Message
		err      error
	}
	tickMsg time.Time
	sentMsg struct {
		node    int
		receipt fleet.Receipt
		err     error
	}
	ackedMsg struct {
		node int
		err  error
	}
	jumpedMsg struct{ err error }
)

type keyMap struct {
	Up, Down, PageUp, PageDown, Top, Bottom key.Binding
	Jump, Detail, Inbox, Send, All, Filter  key.Binding
	Refresh, Quit, Reply, Ack, Back         key.Binding
}

func (k keyMap) tableHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Jump, k.Inbox, k.Send, k.Detail, k.All, k.Filter, k.Refresh, k.Quit}
}

func (k keyMap) inboxHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Reply, k.Ack, k.Jump, k.Back}
}

var keys = keyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
	PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
	Top:      key.NewBinding(key.WithKeys("home", "g")),
	Bottom:   key.NewBinding(key.WithKeys("end", "G")),
	Jump:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "jump")),
	Detail:   key.NewBinding(key.WithKeys("d", " "), key.WithHelp("d", "detail")),
	Inbox:    key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "inbox")),
	Send:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "send")),
	All:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all panes")),
	Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Reply:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
	Ack:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "ack")),
	Back:     key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc", "back")),
}

type screen int

const (
	screenTable screen = iota
	screenInbox
	screenCompose
)

type rowKind int

const (
	rowPane rowKind = iota
	rowLoading
	rowDown
	rowBare
)

// row is one line of the table: a pane, or a device with no panes to show.
type row struct {
	kind   rowKind
	node   int
	pane   fleet.DevicePane
	err    string
	hidden int // rowBare: panes on the device that are not workstations
}

func (r row) key() string {
	if r.kind == rowPane {
		return fmt.Sprintf("%d/%s", r.node, r.pane.Pane.Pane)
	}
	return fmt.Sprintf("%d/", r.node)
}

// nodeState is the last answer from a device. A failed query clears it: showing
// old lamps for an unreachable device would present them as current.
type nodeState struct {
	loaded, loading bool
	panes           []fleet.Pane
	mail            map[string][]fleet.Message
	err             string
}

type compose struct {
	target     row
	replyTo    *fleet.Message
	from       textinput.Model
	body       textarea.Model
	onFrom     bool
	sending    bool
	armDiscard bool
	back       screen
}

type model struct {
	backend Backend
	nodes   []fleet.Node
	scope   string // why the view covers less than the fleet, if it does
	states  []nodeState
	updated time.Time

	rows           []row
	cursor, offset int
	showAll        bool
	filter         textinput.Model
	filtering      bool
	detail         bool

	screen     screen
	inbox      row
	mailCursor int
	bodyOffset int
	armAck     string // the message a second press of ack would delete
	compose    compose

	width, height int
	help          help.Model
	flash, notice string
}

// Run shows the interactive view over the given nodes. scope, if set, says why
// that is less than the whole fleet: a short table must not read as a full one.
func Run(nodes []fleet.Node, scope string) error {
	m := newModel(nodes, realBackend{})
	m.scope = scope
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func newModel(nodes []fleet.Node, backend Backend) *model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "name, device, state, process, pane…"
	m := &model{backend: backend, nodes: nodes, states: make([]nodeState, len(nodes)), filter: ti, help: help.New()}
	m.rebuild()
	return m
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.loadAll(), tick()) }

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// load queries one device, unless a query to it is still running: a slow device
// must not pile up SSH connections, or hold back the others.
func (m *model) load(node int) tea.Cmd {
	if m.states[node].loading {
		return nil
	}
	m.states[node].loading = true
	n := m.nodes[node]
	return func() tea.Msg {
		panes, messages, err := m.backend.Load(n)
		return loadedMsg{node: node, panes: panes, messages: messages, err: err}
	}
}

func (m *model) loadAll() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.nodes))
	for i := range m.nodes {
		cmds[i] = m.load(i)
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.sizeCompose()
	case loadedMsg:
		m.applyLoad(msg)
	case tickMsg:
		return m, tea.Batch(m.loadAll(), tick())
	case jumpedMsg:
		m.flash = ""
		if msg.err != nil {
			m.flash = "jump: " + fleet.Visible(msg.err.Error())
		}
	case sentMsg:
		return m, m.finishSend(msg)
	case ackedMsg:
		m.flash = ""
		if msg.err != nil {
			m.flash = "ack: " + fleet.Visible(msg.err.Error())
		} else {
			m.notice = "acknowledged and deleted"
		}
		return m, m.load(msg.node)
	case tea.KeyMsg:
		switch {
		case m.screen == screenCompose:
			return m, m.updateCompose(msg)
		case m.screen == screenInbox:
			return m, m.updateInbox(msg)
		case m.filtering:
			return m, m.updateFilter(msg)
		}
		return m, m.updateKeys(msg)
	default:
		if m.screen == screenCompose {
			return m, m.forwardCompose(msg)
		}
	}
	return m, nil
}

func (m *model) applyLoad(msg loadedMsg) {
	s := &m.states[msg.node]
	s.loaded, s.loading = true, false
	if msg.err != nil {
		s.panes, s.mail, s.err = nil, nil, strings.TrimSpace(msg.err.Error())
	} else {
		s.panes, s.err = msg.panes, ""
		s.mail = map[string][]fleet.Message{}
		for _, message := range msg.messages {
			s.mail[message.ToPane] = append(s.mail[message.ToPane], message)
		}
		m.updated = time.Now()
	}
	m.rebuild()
}

// rebuild recomputes the visible rows and keeps the cursor on the same pane, or
// failing that on the same device, as lines come and go.
func (m *model) rebuild() {
	var selected row
	hadSelection := m.cursor < len(m.rows)
	if hadSelection {
		selected = m.rows[m.cursor]
	}
	terms := strings.Fields(strings.ToLower(m.filter.Value()))
	matches := func(parts ...string) bool {
		text := strings.ToLower(strings.Join(parts, " "))
		return !slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(text, t) })
	}
	m.rows = m.rows[:0]
	for i, n := range m.nodes {
		s := m.states[i]
		device := fleet.DevicePane{Device: n.Name, Host: n.Target().HostLabel(), Server: n.Server, Port: n.Port}
		if !s.loaded || s.err != "" {
			state, kind := "unreachable", rowDown
			if !s.loaded {
				state, kind = "loading", rowLoading
			}
			if matches(n.Name, n.Server, state, s.err) {
				m.rows = append(m.rows, row{kind: kind, node: i, err: s.err, pane: device})
			}
			continue
		}
		// A device with panes but no workstations stays visible as one row, so
		// it cannot be mistaken for a device that is not being watched.
		if !m.showAll && !slices.ContainsFunc(s.panes, fleet.Pane.Listed) {
			if len(s.panes) > 0 && matches(n.Name, n.Server) {
				m.rows = append(m.rows, row{kind: rowBare, node: i, pane: device, hidden: len(s.panes)})
			}
			continue
		}
		for _, p := range s.panes {
			if (m.showAll || p.Listed()) && matches(n.Name, n.Server, p.Name, p.State, p.Command, p.Location(), p.Pane) {
				pane := device
				pane.Pane = p
				m.rows = append(m.rows, row{kind: rowPane, node: i, pane: pane})
			}
		}
	}
	m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
	if !hadSelection {
		return
	}
	if i := slices.IndexFunc(m.rows, func(r row) bool { return r.key() == selected.key() }); i >= 0 {
		m.cursor = i
	} else if i := slices.IndexFunc(m.rows, func(r row) bool { return r.node == selected.node }); i >= 0 {
		m.cursor = i
	}
}

func (m *model) selected() (row, bool) {
	if m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

// selectedPane is the selected workstation; on a device row it explains why
// there is nothing to act on.
func (m *model) selectedPane() (row, bool) {
	r, ok := m.selected()
	if !ok {
		return row{}, false
	}
	if r.kind == rowBare {
		m.flash = "press a to show the panes on " + fleet.Visible(r.pane.Device)
		return row{}, false
	}
	if r.kind != rowPane {
		m.flash = fleet.Visible(r.pane.Device) + " has no panes to act on"
		return row{}, false
	}
	return r, true
}

func (m *model) move(n int) {
	m.cursor = min(max(m.cursor+n, 0), max(len(m.rows)-1, 0))
}

func (m *model) updateKeys(msg tea.KeyMsg) tea.Cmd {
	page := max(m.tableHeight()-1, 1)
	m.notice, m.flash = "", ""
	switch {
	case key.Matches(msg, keys.Quit):
		return tea.Quit
	case key.Matches(msg, keys.Up):
		m.move(-1)
	case key.Matches(msg, keys.Down):
		m.move(1)
	case key.Matches(msg, keys.PageUp):
		m.move(-page)
	case key.Matches(msg, keys.PageDown):
		m.move(page)
	case key.Matches(msg, keys.Top):
		m.move(-len(m.rows))
	case key.Matches(msg, keys.Bottom):
		m.move(len(m.rows))
	case key.Matches(msg, keys.Detail):
		m.detail = !m.detail
	case key.Matches(msg, keys.All):
		m.showAll = !m.showAll
		m.rebuild()
	case key.Matches(msg, keys.Refresh):
		return m.loadAll()
	case key.Matches(msg, keys.Filter):
		m.filtering = true
		return m.filter.Focus()
	case msg.String() == "esc" && m.filter.Value() != "":
		m.filter.SetValue("")
		m.rebuild()
	case key.Matches(msg, keys.Jump):
		if r, ok := m.selectedPane(); ok {
			return m.backend.Jump(r.pane)
		}
	case key.Matches(msg, keys.Inbox):
		if r, ok := m.selectedPane(); ok {
			m.screen, m.inbox, m.mailCursor, m.bodyOffset, m.armAck = screenInbox, r, 0, 0, ""
		}
	case key.Matches(msg, keys.Send):
		if r, ok := m.selectedPane(); ok {
			return m.startCompose(r, nil, screenTable)
		}
	}
	return nil
}

func (m *model) updateFilter(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.filter.SetValue("")
		fallthrough
	case "enter":
		m.filtering = false
		m.filter.Blur()
		m.rebuild()
		return nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.cursor = 0
	m.rebuild()
	return cmd
}

// mailbox is the inbox being viewed, as of the latest refresh.
func (m *model) mailbox() []fleet.Message {
	return m.states[m.inbox.node].mail[m.inbox.pane.Pane.Pane]
}

func (m *model) selectedMail() (fleet.Message, bool) {
	mail := m.mailbox()
	if len(mail) == 0 {
		return fleet.Message{}, false
	}
	m.mailCursor = min(m.mailCursor, len(mail)-1)
	return mail[m.mailCursor], true
}

func (m *model) updateInbox(msg tea.KeyMsg) tea.Cmd {
	m.notice, m.flash = "", ""
	armed := m.armAck
	m.armAck = ""
	message, hasMail := m.selectedMail()
	switch {
	case msg.String() == "ctrl+c":
		return tea.Quit
	case key.Matches(msg, keys.Back):
		m.screen = screenTable
	case key.Matches(msg, keys.Up):
		m.mailCursor, m.bodyOffset = max(m.mailCursor-1, 0), 0
	case key.Matches(msg, keys.Down):
		m.mailCursor, m.bodyOffset = m.mailCursor+1, 0
		m.selectedMail()
	case key.Matches(msg, keys.PageUp):
		m.bodyOffset = max(m.bodyOffset-5, 0)
	case key.Matches(msg, keys.PageDown):
		m.bodyOffset += 5
	case key.Matches(msg, keys.Jump):
		return m.backend.Jump(m.inbox.pane)
	case key.Matches(msg, keys.Reply) && hasMail:
		// A return address names a host as the recipient's device sees it,
		// which may mean something else, or nothing, from here.
		if m.nodes[m.inbox.node].Host != "" {
			m.flash = "reply from the device holding this inbox: its return addresses resolve there"
			return nil
		}
		return m.startCompose(m.inbox, &message, screenInbox)
	case key.Matches(msg, keys.Ack) && hasMail:
		// Deleting a message another agent has not read is not recoverable.
		if armed != message.ID {
			m.armAck = message.ID
			m.flash = "press a again to delete this message from the inbox"
			return nil
		}
		t, pane, node := m.nodes[m.inbox.node].Target(), m.inbox.pane.Pane.Pane, m.inbox.node
		return func() tea.Msg { return ackedMsg{node: node, err: m.backend.Ack(t, pane, message.ID)} }
	}
	return nil
}

func (m *model) startCompose(target row, replyTo *fleet.Message, back screen) tea.Cmd {
	from := textinput.New()
	from.Prompt = ""
	from.Placeholder = "HOST/SERVER/%PANE"
	from.SetValue(fleet.SelfAddress(m.nodes[target.node]))
	body := textarea.New()
	body.Prompt = ""
	body.ShowLineNumbers = false
	body.CharLimit = 0
	body.MaxHeight = 0
	body.Placeholder = "message"
	m.compose = compose{target: target, replyTo: replyTo, from: from, body: body, back: back}
	m.screen, m.flash, m.notice = screenCompose, "", ""
	m.sizeCompose()
	// With no usable address to prefill, sending would only fail: start there.
	if _, _, _, err := fleet.ParseAddress(from.Value()); err != nil {
		m.compose.onFrom = true
		return m.compose.from.Focus()
	}
	return m.compose.body.Focus()
}

func (m *model) sizeCompose() {
	if m.screen == screenCompose {
		m.compose.from.Width = max(m.width-8, 8)
		m.compose.body.SetWidth(max(m.width, 8))
		m.compose.body.SetHeight(max(m.height-4, 1))
	}
}

func (m *model) updateCompose(msg tea.KeyMsg) tea.Cmd {
	c := &m.compose
	switch msg.String() {
	case "esc", "ctrl+c":
		// A long message is too costly to lose to one stray key.
		if strings.TrimSpace(c.body.Value()) != "" && !c.armDiscard {
			c.armDiscard = true
			m.flash = "press esc again to discard this message"
			return nil
		}
		m.screen, m.flash = c.back, ""
		return nil
	case "tab", "shift+tab":
		c.armDiscard, c.onFrom = false, !c.onFrom
		if c.onFrom {
			c.body.Blur()
			return c.from.Focus()
		}
		c.from.Blur()
		return c.body.Focus()
	case "ctrl+s":
		return m.submit()
	}
	if c.armDiscard {
		c.armDiscard, m.flash = false, ""
	}
	return m.forwardCompose(msg)
}

func (m *model) forwardCompose(msg tea.Msg) tea.Cmd {
	c := &m.compose
	var cmd tea.Cmd
	if c.onFrom {
		c.from, cmd = c.from.Update(msg)
	} else {
		c.body, cmd = c.body.Update(msg)
	}
	return cmd
}

// submit stores the message. There is no deduplication, so a send in flight
// ignores further presses; on failure the draft stays for another try.
func (m *model) submit() tea.Cmd {
	c := &m.compose
	if c.sending {
		return nil
	}
	from, body := strings.TrimSpace(c.from.Value()), c.body.Value()
	if _, _, _, err := fleet.ParseAddress(from); err != nil {
		m.flash = "from: " + err.Error()
		return nil
	}
	if err := fleet.CheckBody(body); err != nil {
		m.flash = err.Error()
		return nil
	}
	c.sending, m.flash = true, ""
	node, t, pane := c.target.node, m.nodes[c.target.node].Target(), c.target.pane.Pane.Pane
	replyTo := c.replyTo
	return func() tea.Msg {
		var receipt fleet.Receipt
		var err error
		if replyTo != nil {
			receipt, err = m.backend.Reply(t, pane, replyTo.ID, from, body)
		} else {
			receipt, err = m.backend.Send(t, pane, from, body)
		}
		return sentMsg{node: node, receipt: receipt, err: err}
	}
}

func (m *model) finishSend(msg sentMsg) tea.Cmd {
	c := &m.compose
	c.sending = false
	if msg.err != nil {
		m.flash = "not sent: " + fleet.Visible(msg.err.Error())
		return nil
	}
	m.screen = c.back
	m.notice = fmt.Sprintf("stored for %s · not a read receipt · id %s", msg.receipt.ToPane, msg.receipt.ID[:8])
	return m.load(msg.node)
}
