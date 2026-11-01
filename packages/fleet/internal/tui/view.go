package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"

	"github.com/toyauro-labs/toyauro-fleet/packages/fleet/internal/fleet"
)

var (
	green  = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	yellow = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	red    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	dim    = lipgloss.AdaptiveColor{Light: "#8c959f", Dark: "#6e7681"}
	selBg  = lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#1f3a5f"}

	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(dim)
	greenStyle  = lipgloss.NewStyle().Foreground(green)
	redStyle    = lipgloss.NewStyle().Foreground(red)
	cellStyle   = lipgloss.NewStyle().PaddingRight(2)
	headerStyle = cellStyle.Bold(true).Foreground(dim)
	detailStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dim).Padding(0, 1)
)

var headers = []string{"WORKSTATION", "DEVICE", "STATE", "INBOX", "PROCESS", "WHERE", "ID"}

const (
	colName = iota
	colDevice
	colState
	colInbox
	colProcess
	colWhere
	colID
)

// dropOrder is which columns give way on a narrow terminal, before anything is
// truncated. Who, how they report and what is waiting is what you came for; the
// detail pane has the rest.
var dropOrder = []int{colWhere, colID, colProcess, colDevice}

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	switch m.screen {
	case screenCompose:
		return m.viewCompose()
	case screenInbox:
		return m.viewInbox()
	}
	top := titleStyle.Render("fleet") + "  " + dimStyle.Render(m.summary())
	parts := []string{m.line(top), m.renderTable()}
	if m.detail {
		if r, ok := m.selected(); ok {
			parts = append(parts, m.renderDetail(r))
		}
	}
	bottom := m.help.ShortHelpView(keys.tableHelp())
	if m.notice != "" {
		bottom = greenStyle.Render(m.notice)
	}
	if m.filtering || m.filter.Value() != "" {
		bottom = m.filter.View() + "  " + dimStyle.Render("enter keep · esc clear")
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(parts, m.footer(bottom))...)
}

func (m *model) line(s string) string { return ansi.Truncate(s, m.width, "…") }

func (m *model) footer(bottom string) string {
	if m.flash != "" {
		bottom = redStyle.Render(m.flash)
	}
	return m.line(bottom)
}

func (m *model) tableHeight() int {
	used := 1 + 2 + 1 // title, table header + rule, footer
	if m.detail {
		if r, ok := m.selected(); ok {
			used += lipgloss.Height(m.renderDetail(r))
		}
	}
	return max(m.height-used, 1)
}

func (m *model) renderTable() string {
	n := m.tableHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+n {
		m.offset = m.cursor - n + 1
	}
	m.offset = max(min(m.offset, len(m.rows)-n), 0)
	visible := m.rows[m.offset:min(m.offset+n, len(m.rows))]

	rows := make([][]string, len(visible))
	for i, r := range visible {
		rows[i] = m.cells(r, m.offset+i == m.cursor)
	}
	cols := make([]int, len(headers))
	for i := range cols {
		cols[i] = i
	}
	build := func() *table.Table {
		return table.New().
			Border(lipgloss.NormalBorder()).BorderStyle(dimStyle).
			BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
			BorderColumn(false).BorderHeader(true).
			Headers(pick(headers, cols)...).Rows(pickRows(rows, cols)...).
			StyleFunc(func(rowIndex, col int) lipgloss.Style {
				if rowIndex == table.HeaderRow {
					return headerStyle
				}
				s := cellStyle
				if m.offset+rowIndex == m.cursor {
					s = s.Background(selBg)
				}
				if cols[col] == colState {
					s = s.Foreground(stateColor(visible[rowIndex]))
				}
				return s
			})
	}
	t := build()
	out := t.String()
	for _, c := range dropOrder {
		if lipgloss.Width(out) <= m.width {
			break
		}
		cols = slices.DeleteFunc(cols, func(x int) bool { return x == c })
		t = build()
		out = t.String()
	}
	if lipgloss.Width(out) > m.width {
		// Narrow terminal: shrink columns and truncate cells, never wrap rows.
		out = t.Wrap(false).Width(m.width).String()
	}
	if len(m.rows) == 0 {
		out += "\n" + dimStyle.Render("  no workstations match · a shows all panes")
	}
	return out
}

func (m *model) cells(r row, selected bool) []string {
	mark := "  "
	if selected {
		// The background alone disappears under NO_COLOR.
		mark = "› "
	}
	device := ansi.Truncate(fleet.Visible(r.pane.Device), 16, "…")
	switch r.kind {
	case rowLoading:
		return []string{mark + "…", device, "… loading", "", "", "", ""}
	case rowDown:
		return []string{mark + "—", device, "✕ unreachable", "", "", "", ""}
	case rowBare:
		return []string{mark + "—", device, fmt.Sprintf("○ %d unnamed", r.hidden), "", "", "", ""}
	}
	p := r.pane
	name, state := p.Name, p.State
	if name == "" {
		name = "—"
	}
	if state == "" {
		state = "unknown"
	}
	inbox := "·"
	if n := len(m.states[r.node].mail[p.Pane.Pane]); n > 0 {
		inbox = fmt.Sprint(n)
	}
	return []string{
		mark + ansi.Truncate(fleet.Visible(name), 24, "…"), device,
		fleet.Glyph(state) + " " + ansi.Truncate(fleet.Visible(state), 12, "…"), inbox,
		ansi.Truncate(fleet.Visible(p.Command), 14, "…"),
		ansi.Truncate(fleet.Visible(p.Location()), 22, "…"), fleet.Visible(p.Pane.Pane),
	}
}

func stateColor(r row) lipgloss.TerminalColor {
	switch r.kind {
	case rowDown:
		return red
	case rowLoading, rowBare:
		return dim
	}
	switch r.pane.State {
	case "working":
		return green
	case "waiting":
		return yellow
	case "error":
		return red
	case "idle":
		return lipgloss.NoColor{}
	}
	return dim
}

func pick(cells []string, cols []int) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = cells[c]
	}
	return out
}

func pickRows(rows [][]string, cols []int) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		out[i] = pick(r, cols)
	}
	return out
}

func (m *model) renderDetail(r row) string {
	label := func(s string) string { return dimStyle.Render(s) }
	var b strings.Builder
	if r.kind != rowPane {
		fmt.Fprintf(&b, "%s  %s %s", titleStyle.Render(fleet.Visible(r.pane.Device)), label("server"), fleet.Visible(r.pane.Server))
		if r.err != "" {
			b.WriteString("\n" + redStyle.Render(fleet.Visible(r.err)))
		}
		if r.kind == rowBare {
			fmt.Fprintf(&b, "\n%s", label(fmt.Sprintf("reachable, %d panes without an Adon name or state · a shows them", r.hidden)))
		}
	} else {
		p := r.pane
		fmt.Fprintf(&b, "%s  %s %s\n", titleStyle.Render(fleet.Visible(orDash(p.Name))), label("address"), fleet.Visible(p.Address()))
		fmt.Fprintf(&b, "%s %s  %s %s  %s %s", label("device"), fleet.Visible(p.Device),
			label("where"), fleet.Visible(p.Location()), label("process"), fleet.Visible(p.Command))
		if p.InMode == "1" {
			b.WriteString("  " + label("in copy mode"))
		}
		fmt.Fprintf(&b, "\n%s %s  %s", label("reported"), fleet.Visible(orDash(p.State)), label("a report, not proof of progress or completion"))
	}
	// A short screen loses the end of the detail, never the selected row.
	inner := max(m.width-2, 20)
	lines := strings.Split(ansi.Wrap(b.String(), inner-2, ""), "\n")
	room := max(m.height-1-2-1-1-2, 1) // title, table header + rule, row, footer, border
	lines = lines[:min(len(lines), room)]
	return detailStyle.Width(inner).Render(strings.Join(lines, "\n"))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (m *model) summary() string {
	var panes, down int
	for _, r := range m.rows {
		switch r.kind {
		case rowPane:
			panes++
		case rowDown:
			down++
		}
	}
	noun := "workstation"
	if m.showAll {
		noun = "pane"
	}
	if panes != 1 {
		noun += "s"
	}
	parts := []string{fmt.Sprintf("%d %s", panes, noun)}
	if down > 0 {
		parts = append(parts, fmt.Sprintf("%d unreachable", down))
	}
	if m.scope != "" {
		parts = append(parts, m.scope)
	}
	if !m.updated.IsZero() {
		parts = append(parts, "updated "+m.updated.Format("15:04:05"))
	}
	return strings.Join(parts, " · ")
}

func (m *model) viewInbox() string {
	p := m.inbox.pane
	title := titleStyle.Render("inbox") + "  " + fleet.Visible(orDash(p.Name)) + "  " + dimStyle.Render(fleet.Visible(p.Address()))
	lines := []string{m.line(title)}
	if err := m.states[m.inbox.node].err; err != "" {
		lines = append(lines, redStyle.Render(m.line("unreachable: "+fleet.Visible(err))))
	}
	bottom := m.help.ShortHelpView(keys.inboxHelp())
	if m.notice != "" {
		bottom = greenStyle.Render(m.notice)
	}
	mail := m.mailbox()
	selected, ok := m.selectedMail()
	if !ok {
		lines = append(lines, dimStyle.Render("  no messages"))
		return strings.Join(append(lines, m.footer(bottom)), "\n")
	}

	// The list takes at most a third of the screen; the body gets the rest.
	listHeight := min(len(mail), max(m.height/3, 1))
	start := max(min(m.mailCursor-listHeight/2, len(mail)-listHeight), 0)
	for i := start; i < start+listHeight; i++ {
		msg, mark := mail[i], "  "
		if i == m.mailCursor {
			mark = "› "
		}
		text := m.line(fmt.Sprintf("%s%s  %s  %s", mark, shortTime(msg.Created), fleet.Visible(msg.From), firstLine(msg.Body)))
		if i == m.mailCursor {
			text = lipgloss.NewStyle().Background(selBg).Render(text)
		}
		lines = append(lines, text)
	}
	lines = append(lines, dimStyle.Render(strings.Repeat("─", max(m.width, 1))))
	meta := "from " + fleet.Visible(selected.From) + " · id " + shortID(selected.ID)
	if selected.ReplyTo != nil {
		meta += " · reply to " + shortID(*selected.ReplyTo)
	}
	lines = append(lines, dimStyle.Render(m.line(meta)))

	body := strings.Split(ansi.Wrap(fleet.VisibleBody(selected.Body), max(m.width, 1), ""), "\n")
	room := max(m.height-len(lines)-1, 1)
	m.bodyOffset = max(min(m.bodyOffset, len(body)-room), 0)
	lines = append(lines, body[m.bodyOffset:min(m.bodyOffset+room, len(body))]...)
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, m.footer(bottom)), "\n")
}

func shortID(id string) string { return ansi.Truncate(fleet.Visible(id), 8, "") }

func shortTime(created string) string {
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return "--:--:--"
	}
	return t.Local().Format("15:04:05")
}

func firstLine(body string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	return fleet.Visible(line)
}

func (m *model) viewCompose() string {
	c := &m.compose
	p := c.target.pane
	verb := "send to"
	if c.replyTo != nil {
		verb = "reply to " + fleet.Visible(c.replyTo.From) + ", from inbox of"
	}
	title := titleStyle.Render(verb) + "  " + fleet.Visible(orDash(p.Name)) + "  " + dimStyle.Render(fleet.Visible(p.Address()))
	from := dimStyle.Render("from  ") + c.from.View()
	hint := "ctrl+s send · tab from/body · esc cancel"
	if c.sending {
		hint = "sending…"
	}
	return strings.Join([]string{m.line(title), m.line(from), dimStyle.Render(strings.Repeat("─", max(m.width, 1))),
		c.body.View(), m.footer(dimStyle.Render(hint))}, "\n")
}
