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
	cmd.Flags().StringVar(from, "from", "", "your return address HOST/SERVER/%PANE")
	cmd.Flags().StringVar(body, "body", "", "message text, or - to read stdin")
	_ = cmd.MarkFlagRequired("from")
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
