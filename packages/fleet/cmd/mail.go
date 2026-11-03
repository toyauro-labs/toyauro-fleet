package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/toyauro-labs/toyauro-fleet/packages/fleet/internal/fleet"
)

func printJSON(v any, indent string) error {
	data, err := fleet.Marshal(v, indent)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func paneFlag(cmd *cobra.Command, pane *string) {
	cmd.Flags().StringVar(pane, "pane", os.Getenv("TMUX_PANE"), "pane ID (default: $TMUX_PANE)")
}

func addressFlags(cmd *cobra.Command, from, body *string) {
	cmd.Flags().StringVar(from, "from", "", "your return address HOST/SERVER/%PANE (default: this pane, as the receiving device reaches it)")
	cmd.Flags().StringVar(body, "body", "", "message text, or - to read stdin")
	_ = cmd.MarkFlagRequired("body")
}

func idFlag(cmd *cobra.Command, id *string) {
	cmd.Flags().StringVar(id, "id", "", "message ID to reuse when retrying a send whose result is unknown")
}

func init() {
	var to, from, body, sendID string
	send := &cobra.Command{
		Use:   "send",
		Short: "Store a message for a pane",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := fleet.ReadBody(body, os.Stdin)
			if err != nil {
				return err
			}
			if from == "" {
				if from, err = defaultFrom(hostFlag); err != nil {
					return err
				}
			}
			receipt, err := fleet.Send(target(), to, from, text, sendID)
			if err != nil {
				return err
			}
			return printJSON(receipt, "")
		},
	}
	send.Flags().StringVar(&to, "to", "", "recipient pane ID")
	_ = send.MarkFlagRequired("to")
	addressFlags(send, &from, &body)
	idFlag(send, &sendID)

	var inboxPane string
	inbox := &cobra.Command{
		Use:   "inbox",
		Short: "Read a pane inbox without deleting messages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			messages, err := fleet.Inbox(target(), inboxPane)
			if err != nil {
				return err
			}
			return printJSON(messages, "  ")
		},
	}
	paneFlag(inbox, &inboxPane)

	var ackPane string
	ack := &cobra.Command{
		Use:   "ack ID",
		Short: "Delete one processed message from the inbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := fleet.Ack(target(), ackPane, args[0]); err != nil {
				return err
			}
			return printJSON(map[string]string{"acknowledged": args[0]}, "")
		},
	}
	paneFlag(ack, &ackPane)

	var replyPane, replyFrom, replyBody, replyID string
	reply := &cobra.Command{
		Use:   "reply ID",
		Short: "Reply to a message using its return address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := fleet.ReadBody(replyBody, os.Stdin)
			if err != nil {
				return err
			}
			if replyFrom == "" {
				if replyFrom, err = defaultReplyFrom(replyPane, args[0]); err != nil {
					return err
				}
			}
			receipt, err := fleet.Reply(target(), replyPane, args[0], replyFrom, text, replyID)
			if err != nil {
				return err
			}
			return printJSON(receipt, "")
		},
	}
	paneFlag(reply, &replyPane)
	addressFlags(reply, &replyFrom, &replyBody)
	idFlag(reply, &replyID)

	rootCmd.AddCommand(send, inbox, ack, reply)
}

// defaultFrom is this pane's return address as the device behind host reaches
// it: "." on the same device, else the return_host configured for that node.
// Hand-written addresses were often unusable ("local", or a device's own name),
// so the default comes from configuration rather than from the sender.
func defaultFrom(host string) (string, error) {
	node := fleet.Node{}
	if host != "" {
		nodes, err := fleet.LoadNodes()
		if err != nil {
			return "", err
		}
		node = fleet.Node{Host: host}
		for _, n := range nodes {
			if n.Host == host && n.ReturnHost != "" {
				node = n
			}
		}
		if node.ReturnHost == "" {
			return "", fmt.Errorf("--from is required: no node for %s sets return_host in %s", host, fleet.ConfigPath())
		}
	}
	addr := fleet.SelfAddress(node)
	if addr == "" {
		return "", fmt.Errorf("--from is required outside tmux")
	}
	return addr, nil
}

// defaultReplyFrom answers toward the device the message came from.
func defaultReplyFrom(pane, id string) (string, error) {
	if hostFlag != "" {
		return "", fmt.Errorf("--from is required with --host")
	}
	messages, err := fleet.Inbox(target(), pane)
	if err != nil {
		return "", err
	}
	for _, m := range messages {
		if m.ID == id {
			host, _, _, err := fleet.ParseAddress(m.From)
			if err != nil {
				return "", err
			}
			if host == "." {
				host = ""
			}
			return defaultFrom(host)
		}
	}
	return "", fmt.Errorf("message %s is not in the inbox of %s", id, pane)
}
