package cmd

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/toyauro-labs/toyauro-fleet/packages/fleet/internal/fleet"
)

func init() {
	node := &cobra.Command{
		Use:   "node",
		Short: "Add, list and remove the devices fleet watches",
	}
	node.AddCommand(nodeAddCmd(), nodeListCmd(), nodeRemoveCmd(), nodeScanCmd())
	rootCmd.AddCommand(node)
}

func nodeAddCmd() *cobra.Command {
	var returnHost string
	var noCheck bool
	c := &cobra.Command{
		Use:   "add NAME [[USER@]HOST[:PORT]]",
		Short: "Add a device, after checking it can be reached",
		Long: `Add a device to this one's config. With no destination, NAME is this device.

The device is contacted over SSH first, and every live tmux server on it is
added; --server picks one instead. A new host key is accepted, a changed one is not.`,
		Example: `  fleet node add mac jeff@100.88.205.23
  fleet node add phone 100.111.45.97:8022 --return-host macmini
  fleet node add phone ace-ts --server default --no-check   # offline for now`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := hostFlag
			if len(args) == 2 {
				dest = args[1]
			}
			var server string
			if cmd.Flags().Changed("server") {
				server = serverFlag
			}
			return addNode(args[0], dest, server, returnHost, noCheck)
		},
	}
	c.Flags().StringVar(&returnHost, "return-host", "", "SSH alias that device uses to reach this one")
	c.Flags().BoolVar(&noCheck, "no-check", false, "add without contacting the device (uses --server, or default)")
	return c
}

func addNode(name, dest, server, returnHost string, noCheck bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("node name must not be empty")
	}
	var host string
	var port int
	if dest != "" {
		var err error
		if host, port, err = fleet.ParseDest(dest); err != nil {
			return err
		}
	}
	nodes, err := fleet.LoadNodes()
	if err != nil {
		return err
	}
	firstNode := nodes == nil

	servers := []string{server}
	if !noCheck {
		probe, err := fleet.ProbeTarget(host, port)
		if err != nil {
			return explainProbeFailure(name, host, port, err)
		}
		if !probe.HasTmux {
			if probe.StrayTmux != "" {
				return fmt.Errorf("%s: tmux is at %s but not on the PATH of a non-interactive SSH session; add its directory to PATH in ~/.zshenv there", name, probe.StrayTmux)
			}
			return fmt.Errorf("%s: tmux is not installed there", name)
		}
		switch {
		case server != "":
		case len(probe.Servers) == 0:
			fmt.Fprintf(os.Stderr, "%s: no tmux server is running there yet; using default\n", name)
			servers = []string{"default"}
		default:
			servers = probe.Servers
		}
	} else if server == "" {
		servers = []string{"default"}
	}

	added := make([]fleet.Node, 0, len(servers))
	for _, s := range servers {
		n := fleet.Node{Name: name, Host: host, Port: port, Server: s, ReturnHost: returnHost}
		if len(servers) > 1 {
			n.Name = name + "-" + s
		}
		added = append(added, n)
	}
	for _, n := range added {
		if i := slices.IndexFunc(nodes, func(o fleet.Node) bool { return o.Name == n.Name }); i >= 0 {
			return fmt.Errorf("a node named %s already exists; run: fleet node rm %s", n.Name, n.Name)
		}
		if i := slices.IndexFunc(nodes, func(o fleet.Node) bool {
			return o.Host == n.Host && o.Port == n.Port && o.Server == n.Server
		}); i >= 0 {
			return fmt.Errorf("%s / %s is already node %s", n.Target().HostLabel(), n.Server, nodes[i].Name)
		}
	}
	// A config replaces the implicit local view, so the first remote device must
	// not make this one's own panes disappear.
	if firstNode && host != "" {
		nodes = append(nodes, localNodes(added)...)
	}
	nodes = append(nodes, added...)
	if err := fleet.SaveNodes(nodes); err != nil {
		return err
	}
	for _, n := range nodes {
		if slices.ContainsFunc(added, func(a fleet.Node) bool { return a.Name == n.Name }) || firstNode {
			fmt.Printf("added %s  %s  server %s\n", fleet.Visible(n.Name), hostPort(n), fleet.Visible(n.Server))
		}
	}
	return nil
}

// localNodes names this device's live servers, avoiding the names being added.
func localNodes(taken []fleet.Node) []fleet.Node {
	probe, err := fleet.ProbeTarget("", 0)
	if err != nil || len(probe.Servers) == 0 {
		return nil
	}
	base, err := os.Hostname()
	if err != nil || base == "" {
		base = "local"
	}
	base, _, _ = strings.Cut(base, ".")
	var nodes []fleet.Node
	for _, s := range probe.Servers {
		name := base
		if len(probe.Servers) > 1 {
			name = base + "-" + s
		}
		if slices.ContainsFunc(taken, func(n fleet.Node) bool { return n.Name == name }) {
			name += "-local"
		}
		nodes = append(nodes, fleet.Node{Name: name, Server: s})
	}
	return nodes
}

// explainProbeFailure turns the failure a new device most often has into the one
// command that fixes it.
func explainProbeFailure(name, host string, port int, err error) error {
	if !fleet.IsAuthError(err) {
		return fmt.Errorf("%s: %w", name, err)
	}
	fmt.Fprintf(os.Stderr, "%s: reachable, but it does not accept this device's SSH key.\n", name)
	command := fleet.AuthorizeCommand(host, port)
	if command == "" {
		fmt.Fprintln(os.Stderr, "This device has no SSH key yet. Run ssh-keygen -t ed25519, then try again.")
		return errSilent
	}
	fmt.Fprintf(os.Stderr, "\nThis lets this device log in there and run anything as that user. On %s, run:\n\n  %s\n\nThen run this command again.\n",
		fleet.Visible(name), command)
	if !strings.Contains(host, "@") {
		fmt.Fprintln(os.Stderr, "Or, if the username there differs from your default, give it as USER@HOST.")
	}
	return errSilent
}

func hostPort(n fleet.Node) string {
	host := fleet.Visible(n.Target().HostLabel())
	if n.Port != 0 {
		host += ":" + strconv.Itoa(n.Port)
	}
	return host
}

func nodeListCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Show the configured devices",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodes, err := fleet.LoadNodes()
			if err != nil {
				return err
			}
			if asJSON {
				out := make([]map[string]any, len(nodes))
				for i, n := range nodes {
					out[i] = map[string]any{"name": n.Name, "host": n.Target().HostLabel(), "port": n.Port, "server": n.Server, "return_host": n.ReturnHost}
				}
				return printJSON(map[string]any{"config": fleet.ConfigPath(), "nodes": out}, "  ")
			}
			if nodes == nil {
				fmt.Printf("No config at %s: fleet shows this device only.\nAdd one with: fleet node add NAME [USER@]HOST[:PORT]\n", fleet.ConfigPath())
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tHOST\tSERVER\tRETURN HOST")
			for _, n := range nodes {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", fleet.Visible(n.Name), hostPort(n), fleet.Visible(n.Server), fleet.Visible(n.ReturnHost))
			}
			return w.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return c
}

func nodeRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove NAME",
		Aliases: []string{"rm"},
		Short:   "Stop watching a device; nothing on it changes",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodes, err := fleet.LoadNodes()
			if err != nil {
				return err
			}
			i := slices.IndexFunc(nodes, func(n fleet.Node) bool { return n.Name == args[0] })
			if i < 0 {
				return fmt.Errorf("no node named %s", args[0])
			}
			if err := fleet.SaveNodes(slices.Delete(nodes, i, i+1)); err != nil {
				return err
			}
			fmt.Printf("removed %s\n", fleet.Visible(args[0]))
			return nil
		},
	}
}

func nodeScanCmd() *cobra.Command {
	var asJSON, all bool
	c := &cobra.Command{
		Use:   "scan",
		Short: "List tailnet devices and the command to add each; never logs in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodes, err := fleet.LoadNodes()
			if err != nil {
				return err
			}
			peers, err := fleet.ScanTailnet(nodes)
			if err != nil {
				return err
			}
			offline := 0
			peers = slices.DeleteFunc(peers, func(p fleet.Peer) bool {
				if !p.Online {
					offline++
				}
				return !p.Online && !all
			})
			if asJSON {
				return printJSON(map[string]any{"peers": peers}, "  ")
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "DEVICE\tOS\tIP\tSSH\tSTATUS")
			for _, p := range peers {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.Name, p.OS, p.IP, scanPort(p), scanStatus(p))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if offline > 0 && !all {
				fmt.Printf("\n%d offline, not shown (--all)\n", offline)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "include offline devices")
	c.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return c
}

func scanPort(p fleet.Peer) string {
	if p.SSHPort == 0 {
		return "-"
	}
	return strconv.Itoa(p.SSHPort)
}

func scanStatus(p fleet.Peer) string {
	switch {
	case p.Configured != "":
		return "node " + fleet.Visible(p.Configured)
	case !p.Online:
		return "offline"
	case p.SSHPort == 0:
		return "no SSH port open"
	}
	dest := p.IP
	if p.SSHPort != 22 {
		dest += ":" + strconv.Itoa(p.SSHPort)
	}
	// Termux ignores the username; elsewhere it usually differs per device.
	if p.SSHPort == 22 {
		dest = "USER@" + dest
	}
	return "fleet node add " + p.Name + " " + dest
}
