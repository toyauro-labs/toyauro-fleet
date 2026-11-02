package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/toyauro-labs/toyauro-fleet/packages/fleet/internal/fleet"
)

type realBackend struct{}

func (realBackend) Load(n fleet.Node) ([]fleet.Pane, []fleet.Message, error) {
	return fleet.Snapshot(n.Target())
}

func (realBackend) Send(t fleet.Target, to, from, body string) (fleet.Receipt, error) {
	return fleet.Send(t, to, from, body)
}

func (realBackend) Reply(t fleet.Target, pane, id, from, body string) (fleet.Receipt, error) {
	return fleet.Reply(t, pane, id, from, body)
}

func (realBackend) Ack(t fleet.Target, pane, id string) error { return fleet.Ack(t, pane, id) }

func (realBackend) Jump(p fleet.DevicePane) tea.Cmd {
	c, interactive := fleet.Jump(p)
	if interactive {
		return tea.ExecProcess(c, func(err error) tea.Msg { return jumpedMsg{err} })
	}
	return func() tea.Msg { return jumpedMsg{c.Run()} }
}
