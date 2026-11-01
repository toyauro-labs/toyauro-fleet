package fleet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

// Node is a configured tmux server on some device.
type Node struct {
	Name, Host, Server string
	Port               int
	// ReturnHost is the SSH alias that device uses to reach this one, needed
	// to build a return address for messages sent there.
	ReturnHost string
}

// Target is the tmux server the node names.
func (n Node) Target() Target { return Target{Host: n.Host, Server: n.Server, Port: n.Port} }

// Pane is what tmux reports for a pane, including Adon's name and state.
type Pane struct {
	Session   string `json:"session"`
	Window    string `json:"window"`
	PaneIndex string `json:"pane_index"`
	Pane      string `json:"pane"`
	Command   string `json:"command"`
	Name      string `json:"name"`
	State     string `json:"state"`
	InMode    string `json:"in_mode"`
	Connected string `json:"connected"`
}

// Listed reports whether a pane is a workstation rather than a bare shell.
func (p Pane) Listed() bool { return p.Name != "" || p.Connected == "1" }

// Location is the pane's current position. Unlike the pane ID, it moves.
func (p Pane) Location() string { return p.Session + ":" + p.Window + "." + p.PaneIndex }

// DevicePane is a pane and where it was found.
type DevicePane struct {
	Device string `json:"device"`
	Host   string `json:"host"`
	Server string `json:"server"`
	Port   int    `json:"-"`
	Pane
}

func (p DevicePane) hostAlias() string {
	if p.Host == "local" {
		return ""
	}
	return p.Host
}

// Address is the pane's return address as seen from its own device.
func (p DevicePane) Address() string {
	host := p.Host
	if host == "local" {
		host = "."
	}
	return host + "/" + p.Server + "/" + p.Pane.Pane
}

// NodeError is a device that could not be queried. It stays visible so an
// unreachable device never looks like one without workstations.
type NodeError struct {
	Device string `json:"device"`
	Host   string `json:"host"`
	Server string `json:"server"`
	Error  string `json:"error"`
}

// ConfigPath is $FLEET_CONFIG, or config.json under the XDG config directory.
func ConfigPath() string {
	if p := os.Getenv("FLEET_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "toyauro-fleet", "config.json")
}

// LoadNodes reads the configured nodes, or returns nil if there is no config.
func LoadNodes() ([]Node, error) {
	data, err := os.ReadFile(ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read fleet config: %w", err)
	}
	return parseNodes(data)
}

func parseNodes(data []byte) ([]Node, error) {
	var config struct {
		Nodes []struct {
			Name       *string `json:"name"`
			Host       *string `json:"host"`
			Port       *int    `json:"port"`
			Server     *string `json:"server"`
			ReturnHost *string `json:"return_host"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("cannot read fleet config: %w", err)
	}
	if len(config.Nodes) == 0 {
		return nil, errors.New("fleet config must contain a non-empty nodes list")
	}
	nodes := make([]Node, 0, len(config.Nodes))
	seen := map[string]bool{}
	for _, raw := range config.Nodes {
		if raw.Name == nil || strings.TrimSpace(*raw.Name) == "" || seen[*raw.Name] {
			return nil, errors.New("each fleet node needs a unique nonempty name")
		}
		n := Node{Name: *raw.Name, Server: "default"}
		seen[n.Name] = true
		if raw.Host != nil {
			if *raw.Host == "" || strings.HasPrefix(*raw.Host, "-") {
				return nil, errors.New("node host must be an SSH alias or null for local")
			}
			n.Host = *raw.Host
		}
		if raw.Port != nil {
			if *raw.Port < 1 || *raw.Port > 65535 || n.Host == "" {
				return nil, errors.New("node port must be 1-65535 and needs a host: " + n.Name)
			}
			n.Port = *raw.Port
		}
		if raw.Server != nil {
			n.Server = *raw.Server
		}
		if !ValidServer(n.Server) {
			return nil, errors.New("invalid tmux server name for node " + n.Name)
		}
		if raw.ReturnHost != nil {
			n.ReturnHost = *raw.ReturnHost
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

const paneFormat = "#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_id}\t#{pane_current_command}\t#{@pane_name}\t#{@agent_adon_state}\t#{pane_in_mode}\t#{@agent_adon_connected}"

// ListPanes reads all panes on a server with their names and reported state.
func ListPanes(t Target) ([]Pane, error) {
	out, err := t.run(t.tmux("list-panes", "-a", "-F", paneFormat), "")
	if err != nil {
		return nil, err
	}
	panes := []Pane{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := append(strings.Split(line, "\t"), make([]string, 9)...)
		panes = append(panes, Pane{Session: f[0], Window: f[1], PaneIndex: f[2], Pane: f[3],
			Command: f[4], Name: f[5], State: f[6], InMode: f[7], Connected: f[8]})
	}
	return panes, nil
}

// Discover queries every node at once, so one slow device delays nothing
// but its own result. Results keep configuration order.
func Discover(nodes []Node, all bool) ([]DevicePane, []NodeError) {
	type result struct {
		panes []Pane
		err   error
	}
	results := make([]result, len(nodes))
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i].panes, results[i].err = ListPanes(n.Target())
		}()
	}
	wg.Wait()
	panes, errs := []DevicePane{}, []NodeError{}
	for i, n := range nodes {
		host := n.Target().HostLabel()
		if err := results[i].err; err != nil {
			errs = append(errs, NodeError{Device: n.Name, Host: host, Server: n.Server, Error: err.Error()})
			continue
		}
		for _, p := range results[i].panes {
			if all || p.Listed() {
				panes = append(panes, DevicePane{Device: n.Name, Host: host, Server: n.Server, Port: n.Port, Pane: p})
			}
		}
	}
	return panes, errs
}

// SelfAddress is the address a peer on node should use to reach this pane.
// Outside tmux there is no pane to receive a reply, so it is empty: claiming an
// existing workstation's address would send the answer to the wrong agent.
// A remote node without return_host leaves the host for the user to fill in.
func SelfAddress(n Node) string {
	pane, server := os.Getenv("TMUX_PANE"), CurrentServer()
	if pane == "" || server == "" {
		return ""
	}
	host := n.ReturnHost
	if n.Host == "" {
		host = "."
	}
	return host + "/" + server + "/" + pane
}

// Visible makes peer-controlled text safe for a terminal: control and format
// characters, which could carry escape sequences, become spaces.
func Visible(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsGraphic(r) {
			return r
		}
		return ' '
	}, s)
}

// VisibleBody is Visible that keeps a message's line breaks.
func VisibleBody(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = Visible(line)
	}
	return strings.Join(lines, "\n")
}

// Glyph is the status lamp for a reported state.
func Glyph(state string) string {
	if state == "" || state == "unknown" {
		return "○"
	}
	return "●"
}
