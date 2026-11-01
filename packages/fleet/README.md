# Fleet: tmux mailboxes

English · [简体中文](README.zh-CN.md)

A small, experimental CLI for peer agents in tmux panes. It uses tmux named buffers as an inbox on the **recipient's tmux server**; SSH transports the same tmux operations between devices. No broker, database, daemon or harness-specific plugin is required. `tmux-agent-adon` remains independent: `fleet list` reads its pane name and reported state.

Requirements: Python 3, tmux, and, for remote peers, noninteractive SSH in both directions. Put `bin/fleet` on each device's PATH. The same version is needed on a device where an agent runs `inbox`, `reply`, or `ack`.

A return address is `SSH_ALIAS/TMUX_SERVER/%PANE`. The alias must work **from the recipient's device**, not necessarily from the sender. Use `.` as the host when replying to a pane on the same machine. A pane ID is unique only within its tmux server and lifetime; pane names and roles are display labels, not unique addresses.

Configure `~/.config/toyauro-fleet/config.json` to query multiple nodes with one `fleet list`. By default it shows panes with an Adon name or a connected Adon adapter; use `fleet list --all` for every pane or `fleet list --json` for scripts. Wide terminals show a table; narrow terminals group panes by device. Status colors are used only on interactive terminals and respect `NO_COLOR`. Unreachable nodes are reported as errors. Names and state are read from tmux live, and names are not unique addresses. For example:

```json
{"nodes":[
  {"name":"macmini","server":"vibe"},
  {"name":"phone","host":"ace-ts","server":"default"}
]}
```

A node without `host` is local; otherwise `host` is an SSH alias on this machine. Configure the reverse direction separately on the phone. Nodes are explicitly registered: Tailscale does not register tmux servers automatically.

```sh
# On Mac: discover configured local and remote fleet panes.
fleet list

# To query just the phone's tmux server:
fleet --host ace-ts --server default list

# Store a request for phone %2; phone can reach this Mac through SSH alias macmini.
fleet --host ace-ts --server default send --to %2 \
  --from 'macmini/vibe/%127' --body 'Please check the Android build.'

# In the phone pane: read, then reply to the message ID shown by inbox.
fleet inbox --pane %2
fleet reply --pane %2 --from 'ace-ts/default/%2' \
  --body 'Checked: build launches.' MESSAGE_ID
fleet ack --pane %2 MESSAGE_ID

# On Mac: see the reply. Use the correct local tmux server and pane.
fleet --server vibe inbox --pane %127
```

`reply` reads the message from the selected local inbox and delivers a new message to its return address; it does **not** delete the original. `ack` explicitly removes a processed message. A reply carries the original thread and reply ID. To send multiline input, use `--body -` and provide UTF-8 text on stdin.

**Delivery semantics:** `stored: true` means tmux accepted the buffer. It does not mean an Agent saw the message, accepted a task, or finished it. Agents must check their inbox themselves; there is no automatic wake-up or injection into a live prompt. `ack` is a local removal, not an automatically transmitted read receipt. Results and acknowledgements can be sent explicitly as replies.

**Limits:** messages are stored in tmux memory and disappear when the recipient server exits. A stopped server, unreachable SSH peer, or missing pane is an error; a pane may disappear immediately after the existence check. Do not assume retries are deduplicated: each send creates a new ID. SSH authentication and access to the tmux server control who can read or modify its buffers. This prototype does not provide encryption beyond SSH, automatic delivery, expiry, persistence, or exactly-once processing.

Run the isolated same-device integration tests with `python3 -m unittest discover -s packages/fleet/tests -v` from the repository root.

## Observed validation

On 2026-10-29, a Mac `vibe` pane and a phone `toyauro-fleet` pane each initiated and replied across Tailscale / SSH. The phone Pi actually read and acknowledged the messages after a manual prompt; automatic notification was not tested. Isolated tmux tests also covered different windows and sessions, thread correlation, and rejection of missing panes.
