# Fleet: tmux mailboxes

English · [简体中文](README.zh-CN.md)

A small, experimental CLI for peer agents in tmux panes. It uses tmux named buffers as an inbox on the **recipient's tmux server**; SSH transports the same tmux operations between devices. No broker, database, daemon or harness-specific plugin is required. `tmux-agent-adon` remains independent: `fleet list` reads its pane name and reported state.

Run `fleet` with no arguments in a terminal for the interactive view; agents use the subcommands, which print JSON.

Requirements: tmux and, for remote peers, noninteractive SSH in both directions. A device only needs `fleet` where someone runs it: remote operations run plain tmux commands over SSH. Messages written by the earlier Python prototype and by this binary are interchangeable.

## Install

```sh
# macOS (Homebrew)
brew install toyauro-labs/tap/fleet

# Phone (Termux)
curl -fLO https://github.com/toyauro-labs/toyauro-fleet/releases/latest/download/fleet-android-arm64.termux.deb
pkg install ./fleet-android-arm64.termux.deb

# Debian / Ubuntu; Fedora uses fleet-linux-amd64.rpm from the same place
curl -fLO https://github.com/toyauro-labs/toyauro-fleet/releases/latest/download/fleet-linux-amd64.deb
sudo apt install ./fleet-linux-amd64.deb

# From source (Go 1.26 or later)
go install github.com/toyauro-labs/toyauro-fleet/packages/fleet@latest
```

The Termux and apt packages do not update themselves; run the same commands again to upgrade.

## Adding devices

```sh
fleet node scan                                # tailnet devices and how to add them; never logs in
fleet node add mac jeff@100.88.205.23          # a phone is IP:8022
fleet node ls
fleet node rm mac
```

`node add` contacts the device over SSH and adds every live tmux server on it, or the one named by `--server`. A host key seen for the first time is trusted; a changed one is refused. If the device refuses this one's key, it prints the command to run over there, which lets this device log in and run anything as that user. Use `--no-check` for a device that is offline. The first remote device also adds this one, so its own panes stay in view.

## Interactive view

A live table of every configured device, refreshed every 5 seconds, in the style of `gpus`.

| Key | Action |
| --- | --- |
| `↑`/`k`, `↓`/`j`, `g`, `G` | Move |
| `enter` | Jump to the pane: switch client on the same tmux server, otherwise attach, over SSH if remote. Detach to return. |
| `i` | Read the pane's inbox. `r` replies, `a` twice deletes a message, `esc` goes back. |
| `s` | Write a message. `ctrl+s` sends, `tab` moves between return address and body. |
| `d` | Show the pane's address and details |
| `a` | Show all panes, not just workstations |
| `/` | Filter |
| `r` | Refresh now |
| `q` | Quit |

The `INBOX` column counts messages stored and not yet acknowledged; it is not an unread count. An unreachable device appears as one `unreachable` row and its panes are dropped, so lamps that can no longer be checked never look current. A reachable device with no workstations appears as one `N unnamed` row; press `a` to expand it.

The view only covers devices in the config. With no config it shows this device alone, and `--host` or `--server` skip the config to show one server; the title bar says so in both cases.

`s` prefills your return address from the tmux pane the view runs in. Add `"return_host"` to a remote node to name the alias that device uses to reach this one. Outside tmux there is no pane to receive a reply, so no address is prefilled. You can only reply from the device holding the inbox, because return addresses name hosts as that device sees them.

Attaching from a smaller terminal can shrink the window for everyone on it, following that server's `window-size` option.

A return address is `SSH_ALIAS/TMUX_SERVER/%PANE`. The alias must work **from the recipient's device**, not necessarily from the sender. Use `.` as the host when replying to a pane on the same machine. Inside tmux, `send` and `reply` fill the address in when `--from` is omitted: `.` on the same device, and across devices the `return_host` of the node you are sending to. Without one, a cross-device send asks for `--from`. In a five-round experiment with Pi agents, about 40% of hand-written addresses could not be used by the recipient (`local`, or a device's own name); set `return_host` once on each device instead. A pane ID is unique only within its tmux server and lifetime; pane names and roles are display labels, not unique addresses.

`fleet node add` writes devices to `~/.config/toyauro-fleet/config.json`, which you can also edit by hand, with `"port"` for a nonstandard SSH port; one `fleet list` then queries them all. By default it shows panes with an Adon name or a connected Adon adapter; use `fleet list --all` for every pane or `fleet list --json` for scripts. Wide terminals show a table; narrow terminals group panes by device. Status colors are used only on interactive terminals and respect `NO_COLOR`. Unreachable nodes are reported as errors. Names and state are read from tmux live, and names are not unique addresses. For example:

```json
{"nodes":[
  {"name":"macmini","server":"vibe"},
  {"name":"phone","host":"ace-ts","server":"default","return_host":"macmini"}
]}
```

A node without `host` is local; otherwise `host` is an SSH alias on this machine, or `user@address`. Configure the reverse direction separately on the phone. Nodes are explicitly registered: `fleet node scan` only lists what is on the tailnet.

```sh
# On Mac: discover configured local and remote fleet panes.
fleet list

# To query just the phone's tmux server:
fleet --host ace-ts --server default list

# Store a request for phone %2 from Mac pane %127. The return address,
# macmini/vibe/%127, comes from the phone node's return_host.
fleet --host ace-ts --server default send --to %2 \
  --body 'Please check the Android build.'

# In the phone pane: read, then reply to the message ID shown by inbox.
fleet inbox --pane %2
fleet reply --pane %2 --body 'Checked: build launches.' MESSAGE_ID
fleet ack --pane %2 MESSAGE_ID

# On Mac: see the reply. Use the correct local tmux server and pane.
fleet --server vibe inbox --pane %127
```

`reply` reads the message from the selected local inbox and delivers a new message to its return address; it does **not** delete the original. `ack` explicitly removes a processed message. A reply carries the original thread and reply ID. To send multiline input, use `--body -` and provide UTF-8 text on stdin.

**Delivery semantics:** `stored: true` means tmux accepted the buffer. It does not mean an Agent saw the message, accepted a task, or finished it. Agents must check their inbox themselves; there is no automatic wake-up or injection into a live prompt. An agent that types into another pane to prompt it must read the pane first and leave any draft it did not type alone: never clear, edit or submit it. Inside tmux, `--server` defaults to the server the command runs in; with `--host` it defaults to `default`. `ack` is a local removal, not an automatically transmitted read receipt. Results and acknowledgements can be sent explicitly as replies.

**Limits:** messages are stored in tmux memory and disappear when the recipient server exits. A stopped server, unreachable SSH peer, or missing pane is an error; a pane may disappear immediately after the existence check. A send or reply that fails after reaching the peer may still have stored the message; the error then names its ID, and retrying with `--id ID` overwrites that copy instead of adding a second. This holds only until the recipient acks: a retry after that stores the message again, and a recipient that saw both attempts may act twice. SSH authentication and access to the tmux server control who can read or modify its buffers. This prototype does not provide encryption beyond SSH, automatic delivery, expiry, persistence, or exactly-once processing.

From this directory, `make build` builds, `make dist` cross-compiles, and `make test` runs the tests against isolated tmux servers. `tests/test_fleet.py` pins the command-line contract agents depend on; set `FLEET_BIN` to run it against a built binary on a device without Go.

## Observed validation

On 2026-10-29, a Mac `vibe` pane and a phone `toyauro-fleet` pane each initiated and replied across Tailscale / SSH using the earlier Python prototype. The phone Pi actually read and acknowledged the messages after a manual prompt; automatic notification was not tested. Isolated tmux tests also covered different windows and sessions, thread correlation, and rejection of missing panes.

On 2026-11-01, this Go version was tested on Linux with tmux 3.4: it exchanged messages with the Python prototype in both directions on one server, its `list` output matched byte for byte, and the interactive view was driven in a real terminal against isolated servers, with SSH replaced by a stand-in that runs the remote command through a shell. 

On 2026-11-02, v0.1.1 was installed on a Mac with `brew` and on a rooted Android 16 phone from the Termux package. The contract tests passed on both, twice each, with exit code 0 on the phone, where the Python prototype's test process had segfaulted on exit. Over real SSH through Tailscale, on isolated tmux servers: Mac and phone each sent to the other, read, replied and acknowledged; a body with Chinese text, newlines and shell metacharacters arrived intact and ran nothing; the view ran on both devices; and `enter` attached to a pane on each device and returned on detach. `brew` was run over SSH, so this does not cover macOS prompting about the unnotarized binary when opened from Finder, and agents still do not pick up messages unprompted.

On that phone SSH took about 4.5 seconds per connection (574 ms round trip), so a device on a link like this appears, and refreshes, about 5 seconds late. In a rooted Termux session `pkg` refuses to run as root; install as the Termux user, or from the Termux app.
