package fleet

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var destRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*(@[A-Za-z0-9_][A-Za-z0-9_.-]*)?$`)

// ParseDest splits an SSH destination, [user@]host[:port], for a node.
func ParseDest(dest string) (host string, port int, err error) {
	host = dest
	if h, p, found := strings.Cut(dest, ":"); found {
		host = h
		port, err = strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("invalid port in %q", dest)
		}
	}
	if !destRe.MatchString(host) {
		return "", 0, fmt.Errorf("invalid SSH destination %q: expected [user@]host[:port]", dest)
	}
	return host, port, nil
}

// SaveNodes replaces the config atomically: a crash cannot leave half a file
// where the whole fleet's addresses were. No nodes at all removes the file,
// because an empty list is not a valid config.
func SaveNodes(nodes []Node) error {
	path := ConfigPath()
	if len(nodes) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	type entry struct {
		Name       string `json:"name"`
		Host       string `json:"host,omitempty"`
		Port       int    `json:"port,omitempty"`
		Server     string `json:"server"`
		ReturnHost string `json:"return_host,omitempty"`
	}
	entries := make([]entry, len(nodes))
	for i, n := range nodes {
		entries[i] = entry{n.Name, n.Host, n.Port, n.Server, n.ReturnHost}
	}
	data, err := json.MarshalIndent(map[string]any{"nodes": entries}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// probeScript runs on the device being added. Only sockets with a server that
// answers count: tmux leaves dead sockets behind. It starts nothing.
const probeScript = `echo "os=$(uname -s)"
if ! command -v tmux >/dev/null 2>&1; then
  for b in /opt/homebrew/bin/tmux /usr/local/bin/tmux; do [ -x "$b" ] && echo "stray=$b"; done
  exit 0
fi
echo "tmux=$(command -v tmux)"
uid=$(id -u)
for d in "${TMUX_TMPDIR:-/tmp}" "${PREFIX:-/nonexistent}/var/run" "${PREFIX:-/nonexistent}/tmp" /tmp; do
  for s in "$d/tmux-$uid"/*; do
    [ -S "$s" ] || continue
    n=${s##*/}
    tmux -L "$n" list-sessions >/dev/null 2>&1 && echo "server=$n"
  done
done
tmux -L default list-sessions >/dev/null 2>&1 && echo server=default
true`

// Probe is what a device says about its tmux over the same path Fleet will use.
type Probe struct {
	OS      string
	HasTmux bool
	// StrayTmux is a tmux that exists but is not on the non-interactive PATH,
	// the usual state of a Homebrew install reached over SSH.
	StrayTmux string
	Servers   []string // live servers, default first
}

// ProbeTarget asks a device about tmux. It accepts a new host key, since adding
// the device is the moment a person vouches for it.
func ProbeTarget(host string, port int) (Probe, error) {
	t := Target{Host: host, Port: port, trustNewKey: true}
	out, err := t.run([]string{"sh", "-c", probeScript}, "")
	if err != nil {
		return Probe{}, err
	}
	return parseProbe(out), nil
}

func parseProbe(out string) Probe {
	var p Probe
	for _, line := range strings.Split(out, "\n") {
		key, value, _ := strings.Cut(line, "=")
		switch key {
		case "os":
			p.OS = value
		case "tmux":
			p.HasTmux = true
		case "stray":
			if p.StrayTmux == "" {
				p.StrayTmux = value
			}
		case "server":
			if ValidServer(value) && !slices.Contains(p.Servers, value) {
				p.Servers = append(p.Servers, value)
			}
		}
	}
	slices.SortStableFunc(p.Servers, func(a, b string) int {
		switch {
		case a == b:
			return 0
		case a == "default":
			return -1
		case b == "default":
			return 1
		}
		return strings.Compare(a, b)
	})
	return p
}

// IsAuthError reports whether SSH reached the device and was refused a login.
func IsAuthError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Permission denied")
}

// AuthorizeCommand is what to run on a device so this one may log in, using the
// first public key ssh would offer for host. Empty if this device has no key.
func AuthorizeCommand(host string, port int) string {
	out, err := exec.Command("ssh", append(append([]string{"-G"}, portArgs(port)...), "--", host)...).Output()
	if err != nil {
		return ""
	}
	home, _ := os.UserHomeDir()
	for _, line := range strings.Split(string(out), "\n") {
		path, found := strings.CutPrefix(line, "identityfile ")
		if !found {
			continue
		}
		if rest, ok := strings.CutPrefix(path, "~/"); ok {
			path = filepath.Join(home, rest)
		}
		key, err := os.ReadFile(path + ".pub")
		if err != nil {
			continue
		}
		return "mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo " + quote(strings.TrimSpace(string(key))) +
			" >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
	}
	return ""
}

// Peer is a device on the tailnet, as this one can see it.
type Peer struct {
	Name       string `json:"name"`
	OS         string `json:"os"`
	IP         string `json:"ip"`
	Online     bool   `json:"online"`
	SSHPort    int    `json:"ssh_port,omitempty"`
	Configured string `json:"configured,omitempty"` // the node already naming it
}

// tailscaleCommand finds the CLI. The Mac app does not put it on PATH.
func tailscaleCommand() (string, error) {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path, nil
	}
	const macApp = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	if _, err := os.Stat(macApp); err == nil {
		return macApp, nil
	}
	return "", errors.New("tailscale CLI not found")
}

// ScanTailnet lists the tailnet's devices, which ports answer for SSH, and which
// are already nodes. It never logs in, so it says nothing about access.
func ScanTailnet(nodes []Node) ([]Peer, error) {
	cli, err := tailscaleCommand()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	c := exec.Command(cli, "status", "--json")
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %s", strings.TrimSpace(stderr.String()))
	}
	peers, err := parsePeers(out)
	if err != nil {
		return nil, err
	}
	configured := map[string]string{}
	for _, n := range nodes {
		if n.Host != "" {
			configured[sshHostname(n.Host)] = n.Name
		}
	}
	var wg sync.WaitGroup
	for i := range peers {
		peers[i].Configured = configured[peers[i].IP]
		if name := configured[peers[i].Name]; name != "" {
			peers[i].Configured = name
		}
		if peers[i].Online {
			wg.Add(1)
			go func(p *Peer) {
				defer wg.Done()
				p.SSHPort = sshPort(p.IP)
			}(&peers[i])
		}
	}
	wg.Wait()
	return peers, nil
}

func parsePeers(data []byte) ([]Peer, error) {
	var status struct {
		Peer map[string]struct {
			HostName, DNSName, OS string
			TailscaleIPs          []string
			Online                bool
		}
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("cannot read tailscale status: %w", err)
	}
	peers := make([]Peer, 0, len(status.Peer))
	for _, p := range status.Peer {
		if len(p.TailscaleIPs) == 0 {
			continue
		}
		// The DNS name is unique on a tailnet; the host name need not be.
		name, _, _ := strings.Cut(p.DNSName, ".")
		if name == "" {
			name = p.HostName
		}
		peers = append(peers, Peer{Name: Visible(name), OS: Visible(p.OS), IP: Visible(p.TailscaleIPs[0]), Online: p.Online})
	}
	slices.SortFunc(peers, func(a, b Peer) int {
		if a.Online != b.Online {
			if a.Online {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return peers, nil
}

func sshHostname(host string) string {
	out, err := exec.Command("ssh", "-G", "--", host).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if name, found := strings.CutPrefix(line, "hostname "); found {
			return name
		}
	}
	return ""
}

// sshPort finds which of the usual ports answers: 22, or 8022 for Termux.
func sshPort(ip string) int {
	for _, port := range []int{22, 8022} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), 2*time.Second)
		if err == nil {
			c.Close()
			return port
		}
	}
	return 0
}
