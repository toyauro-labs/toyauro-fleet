<div align="center">

# toyauro-fleet

**Autonomous agent workbenches for a lights-out software factory.**

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

## Why I am building this

I want an agent fleet that can actually work: I provide an outcome, and agents
use existing tools to explore, collaborate, build and verify. Work can continue
while I am away. When I return, I can inspect progress, results and decisions
that need my attention instead of relaying every message myself.

The phone is an independent agent environment. It runs its own agents and phone
tools; the Mac mini runs its own agents and software tools. The devices should
exchange information and ask each other for help as peers. A phone can request
help with software debugging, and a Mac can request a real-device check.

Agent capabilities will continue to improve. I want the system to remain simple:
**reuse tools agents already understand, let agents decide how to work together,
and turn repeated steps into CLI tools only when real work calls for them.**

## What we want to build

ToyaUro Fleet is a monorepo for a lights-out software factory: independent
agents work from visible workbenches and gradually close the loop from an
objective through collaboration, execution, verification and delivery. People
set goals and acceptance criteria and can inspect, decide or take over.

```text
Phone: ToyaUro                          Mac mini
┌─────────────────────────┐             ┌──────────────────────────┐
│ tmux                    │             │ tmux                     │
│ Pi / other agent panels │◄── SSH ────►│ Codex / other agents     │
│ Phone tools, shell, Git │  Tailscale  │ Software tools, shell     │
└─────────────────────────┘             └──────────────────────────┘
```

Every tmux panel is an equal, general-purpose workbench: any panel can take a
name such as `phone use` or `dev` and change responsibilities with context. The
experimental communication CLI uses tmux and SSH for discovery, requests and
replies; it is being validated in real work. `tmux-agent-adon` provides the status lights.

For example, a Mac workbench could build an app, ask a phone workbench to open
and inspect it, then use the phone's observations to continue. **This is a
target workflow, not an end-to-end fleet communication feature implemented in
this repository today.**

We start with tmux, shell, SSH and Git without assuming a database, message
queue or central controller. Add capabilities when an actual need emerges;
agents can continue to use the underlying tools directly.

## First milestones

1. Validate two different agent harnesses in local tmux panels discovering
   each other, sending a request and returning an explicit response.
2. Repeat the exchange in both directions between the phone and Mac over SSH.
3. Apply the communication to a real software task and verify the outcome;
   extract only the reusable commands that the task actually needs.

Sending input to tmux does not by itself establish that an agent accepted or
completed a request. Each milestone needs evidence of the receiver's response.

## Components

| Component | Purpose | Status |
| --- | --- | --- |
| [tmux-agent-adon](packages/tmux-agent-adon/) | Panel names, agent status lights and optional agent adapters. | Imported from the independent [tmux-agent-adon repository](https://github.com/toyauro/tmux-agent-adon). |
| [Fleet communication](packages/fleet/) | Discover panes and exchange messages through tmux buffers and SSH. | Experimental; agents must check their inbox. |
| Software factory | Build and verify software using collaborating agents. | Direction for future real workflows; not yet implemented here. |

The independent adon repository remains available. Its MIT license and commit
history are preserved in the `packages/tmux-agent-adon` subtree. A clean clone of
this repository contains the component directly; no submodule is required.
The two repositories do not synchronize automatically. To bring a later
standalone adon release into this repository, review it and run:

```sh
git subtree pull --prefix=packages/tmux-agent-adon https://github.com/toyauro/tmux-agent-adon.git main
```

## Try the status lights

From a shell inside the tmux server you want to use, after cloning this repo:

```sh
export PATH="$PWD/packages/tmux-agent-adon/bin:$PATH"
tmux run-shell "$PWD/packages/tmux-agent-adon/agent-adon.tmux"
```

Loading the plugin registers format fragments. To try the bundled label in the
current window:

```sh
tmux set-option -w pane-border-status top
tmux set-option -w pane-border-format '#{E:@agent_adon_label_format}'
```

This changes the current window's border format. To keep an existing theme,
follow the [display integration guide](packages/tmux-agent-adon/examples/README.md)
instead. The plugin can be used independently of the future fleet features.

## Current boundaries

- A status light reflects a report; it does not prove task completion or that
  the reporter is still alive.
- Agents must actively check their inbox; automatic wake-up, persistence and
  automated factory execution remain future work.
- Device scopes remain autonomous; no central controller, database or broker
  is required by this repository today.

## License

The repository uses the [MIT License](LICENSE); component licenses remain in
their respective directories.
