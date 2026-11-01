// Package cmd wires the fleet subcommands. Agents get JSON from plain commands;
// humans get the TUI from a bare `fleet` in a terminal.
package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/toyauro-labs/toyauro-fleet/packages/fleet/internal/fleet"
	"github.com/toyauro-labs/toyauro-fleet/packages/fleet/internal/tui"
)

// errSilent signals failure after the command already reported it.
var errSilent = errors.New("silent")

var hostFlag, serverFlag string

// version is set at release time.
var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "fleet",
	Version: version,
	Short:   "Peer tmux inboxes; a stored message is not a read receipt",
	Long: `fleet lets agents in tmux panes find each other and exchange messages, with
tmux named buffers as inboxes and SSH between devices. There is no daemon.

Run without arguments in a terminal for the interactive view. Add devices to it
with "fleet node add"; "fleet node scan" lists what is on your tailnet.

Nodes: $FLEET_CONFIG or ~/.config/toyauro-fleet/config.json`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if !fleet.ValidServer(serverFlag) {
			return errors.New("invalid tmux server name")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
			return runUI(cmd)
		}
		return cmd.Help()
	},
}

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Interactive view: live workstations, inboxes, send, enter to jump",
	Args:  cobra.NoArgs,
	RunE:  func(cmd *cobra.Command, args []string) error { return runUI(cmd) },
}

func init() {
	rootCmd.PersistentFlags().StringVar(&hostFlag, "host", "", "SSH alias of target device; omit for local")
	rootCmd.PersistentFlags().StringVar(&serverFlag, "server", "default", "tmux server name")
	rootCmd.AddCommand(uiCmd)
	rootCmd.SetVersionTemplate("fleet {{.Version}}\n")
}

func target() fleet.Target { return fleet.Target{Host: hostFlag, Server: serverFlag} }

// selectedNodes is what a command that spans devices should look at: an explicit
// --host or --server names one node and skips the config.
func selectedNodes(cmd *cobra.Command) ([]fleet.Node, error) {
	flags := cmd.Flags()
	if !flags.Changed("host") && !flags.Changed("server") {
		nodes, err := fleet.LoadNodes()
		if err != nil || nodes != nil {
			return nodes, err
		}
	}
	name := hostFlag
	if name == "" {
		name = "local"
	}
	return []fleet.Node{{Name: name, Host: hostFlag, Server: serverFlag}}, nil
}

func runUI(cmd *cobra.Command) error {
	nodes, err := selectedNodes(cmd)
	if err != nil {
		return err
	}
	return tui.Run(nodes, uiScope(cmd))
}

// uiScope says when the view is narrower than the fleet, which an empty-looking
// table cannot say for itself.
func uiScope(cmd *cobra.Command) string {
	if cmd.Flags().Changed("host") || cmd.Flags().Changed("server") {
		return "one server only: --host/--server skip the config"
	}
	if configured, err := fleet.LoadNodes(); err == nil && configured == nil {
		return "this device only: no config at " + fleet.ConfigPath()
	}
	return ""
}

// Execute runs the command line and exits non-zero on failure.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, "fleet:", err)
		}
		os.Exit(1)
	}
}
