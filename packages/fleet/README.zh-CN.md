# Fleet：基于 tmux 的工位收件箱

[English](README.md) · 简体中文

这是实验性通信 CLI：每个 pane 都可以通过 tmux 命名 buffer 收取消息，通过 SSH 与其他设备通信。没有中心服务、数据库或常驻进程。Adon 保持独立，`fleet list` 读取它的工位名称和状态。

在终端直接运行 `fleet` 打开交互界面；Agent 使用子命令，输出 JSON。

需要 tmux 和非交互 SSH。只有要运行 `fleet` 的设备才需要它，远程操作通过 SSH 执行的是普通 tmux 命令。早期 Python 原型与本版本写入的消息可以互相读取。

## 安装

```sh
# macOS（Homebrew）
brew install toyauro-labs/tap/fleet

# 手机（Termux）
curl -fLO https://github.com/toyauro-labs/toyauro-fleet/releases/latest/download/fleet-android-arm64.termux.deb
pkg install ./fleet-android-arm64.termux.deb

# Debian / Ubuntu；Fedora 用同目录下的 fleet-linux-amd64.rpm
curl -fLO https://github.com/toyauro-labs/toyauro-fleet/releases/latest/download/fleet-linux-amd64.deb
sudo apt install ./fleet-linux-amd64.deb

# 从源码（需要 Go 1.26 或更高版本）
go install github.com/toyauro-labs/toyauro-fleet/packages/fleet@latest
```

Termux 和 apt 的安装包不会自动更新，升级时重新运行同样的命令。

## 添加设备

```sh
fleet node scan                                # 列出 tailnet 上的设备和添加命令，不登录
fleet node add mac jeff@100.88.205.23          # 手机用 IP:8022
fleet node ls
fleet node rm mac
```

`node add` 会先经 SSH 连接设备，把上面在运行的 tmux server 都加入配置，也可用 `--server` 指定一个；第一次看到的 host key 会被信任，已变化的 host key 会被拒绝。如果对方不接受本机的密钥，它会打印要在对方设备上运行的命令，这条命令会允许本机以该用户登录并执行任意操作。设备离线时用 `--no-check`。第一次添加远程设备时，本机也会写入配置，所以本机的工位不会消失。

## 交互界面

实时显示所有已配置设备的工位，每 5 秒刷新，操作方式与 `gpus` 一致。

| 按键 | 作用 |
| --- | --- |
| `↑`/`k`、`↓`/`j`、`g`、`G` | 移动 |
| `enter` | 跳到该 pane：同一 tmux server 时切换 client，否则 attach，远程则经 SSH。detach 后返回。 |
| `i` | 查看该 pane 的收件箱。`r` 回复，连按两次 `a` 删除消息，`esc` 返回。 |
| `s` | 写消息。`ctrl+s` 发送，`tab` 在回复地址和正文之间切换。 |
| `d` | 显示 pane 的地址和详情 |
| `a` | 显示所有 pane，而不只是工位 |
| `/` | 过滤 |
| `r` | 立即刷新 |
| `q` | 退出 |

`INBOX` 列统计已存入、尚未 `ack` 的消息，不是未读数。不可达设备显示为一行 `unreachable`，其工位不再显示，避免把无法核实的状态灯当作当前状态。可达但没有工位的设备显示为一行 `N unnamed`，按 `a` 展开。

只有配置文件中的设备会出现在界面里。没有配置时只显示本机，`--host` 或 `--server` 会跳过配置只看一个 server，这两种情况标题栏都会注明。

`s` 会用界面所在的 tmux pane 预填回复地址。远程节点可增加 `"return_host"`，写明对方连回本机所用的 SSH 别名。不在 tmux 内运行时没有可收回复的 pane，因此不会预填地址。只能在持有该收件箱的设备上回复，因为回复地址中的主机名是按那台设备的视角写的。

从较小的终端 attach 可能让该窗口对所有人都变小，具体取决于该 server 的 `window-size` 选项。

## 地址与配置

地址格式为 `SSH别名/tmux服务器/%pane`：SSH 别名必须是**对方机器**上能访问发送方的别名。同一台设备内发消息可以用 `.`。在 tmux 里省略 `--from` 时，`send` 和 `reply` 会自动填写：同一台设备用 `.`，跨设备用目标节点的 `return_host`；没配 `return_host` 的跨设备发送会要求写 `--from`。五轮 Pi Agent 实验中，手写地址约四成对方无法使用（如 `local`，或写成本机自己的名字），所以请在每台设备上配置一次 `return_host`，不要让 Agent 手写。pane 编号只在对应的 tmux server 存活期间有效，名称和职责可以改变，不能作为唯一地址。

`fleet node add` 把设备和 tmux server 写入 `~/.config/toyauro-fleet/config.json`，也可手动编辑；非标准 SSH 端口用 `"port"`。之后 `fleet list` 会同时查询它们。默认列出有 Adon 名称或已接入 Adon 的 pane；`fleet list --all` 查看所有 pane，`fleet list --json` 用于脚本。宽终端显示表格，窄终端按设备分组；状态灯只在交互终端着色，设置 `NO_COLOR` 可关闭颜色。不可达设备会报错，不会静默当成没有工位。列表实时读取 pane 的 `@pane_name` 和状态；显示名称不是唯一地址。配置样例：

```json
{"nodes":[
  {"name":"macmini","server":"vibe"},
  {"name":"phone","host":"ace-ts","server":"default","return_host":"macmini"}
]}
```

其中没有 `host` 的节点是本机，其余 `host` 是本机 SSH 配置中的别名或 `用户@地址`；在手机上使用相反方向的配置。节点需要显式登记，`fleet node scan` 只负责列出 tailnet 上的设备。

```sh
# 在 Mac 上统一查看本机和手机工位
fleet list

# 只查看手机一个 server
fleet --host ace-ts --server default list

# 从 Mac 的 %127 发给手机 %2；回复地址 macmini/vibe/%127 来自 phone 节点的 return_host
fleet --host ace-ts --server default send --to %2 \
  --body '请检查 Android 构建。'

# 手机 Agent 在 %2 里读取和回复
fleet inbox --pane %2
fleet reply --pane %2 --body '已检查，构建能够启动。' 消息ID

# Mac Agent 查看自己的收件箱
fleet --server vibe inbox --pane %127
```

`reply` 带原消息 ID 和线程 ID；回复存入后会顺带 ack 原消息，因为回复了就算处理完了，要留着原消息就加 `--keep`。没有回复、直接处理完的消息用 `ack` 删除。`inbox` 在标准输出给 JSON，有消息时在标准错误提示如何 ack。`--body -` 可从标准输入读取多行文字。

**现在的边界：** `stored: true` 只表示消息写入了对方 tmux，不表示 Agent 已读或完成。Agent 需要主动运行 `inbox`；当前不会自动唤醒 Agent 或注入输入框。需要在对方 pane 里输入文字去提醒它时，先读这个 pane；输入框里有不是自己输入的草稿就不要动，不清空、不修改、不提交。在 tmux 里，`--server` 默认是命令所在的 server；带 `--host` 时默认是 `default`。tmux server 退出后消息会消失。pane、SSH 或 server 不可用时会报错；发送或回复在连上对方后失败时，消息可能已经存入；这时错误信息会给出消息 ID，用 `--id 该ID` 重试会覆盖原消息，而不是再存一份。这只在对方 ack 之前成立：ack 之后再重试会重新存入，看到过两次的收件方也可能处理两次。SSH 凭据及 tmux server 的访问权限决定谁能读写消息。需要持久化、去重和自动通知时，再根据实际需求补充。

在本目录执行 `make build` 构建，`make dist` 交叉编译；`make test` 运行测试，测试使用隔离的 tmux server。`tests/test_fleet.py` 固定了 Agent 依赖的命令行约定；没有 Go 的设备可设置 `FLEET_BIN`，让它测试已构建的二进制。

## 本次实测

2026-10-29：Mac 的 `vibe` server 工位与手机 `toyauro-fleet` 工位使用早期 Python 原型，通过 Tailscale / SSH 双向发起、读取并回复；手机 Pi 实际读取并确认了消息。Agent 是经人工提示后查看收件箱的，这次没有验证自动通知。另用独立 tmux server 验证了跨 window、跨 session、原消息关联和错误 pane 拒绝。

2026-11-01：Go 版本在 Linux 和 tmux 3.4 上完成测试：与 Python 原型在同一 server 上双向收发，`list` 输出逐字节一致；交互界面在真实终端中针对隔离 server 操作，SSH 由一个经 shell 执行远程命令的替身代替。

2026-11-02：v0.1.1 用 `brew` 装到 Mac，用 Termux 包装到一台已 root 的 Android 16 手机。约定测试在两台设备上各跑两遍都通过，手机上退出码为 0，之前 Python 原型的测试进程曾在这里退出时段错误。经 Tailscale 走真实 SSH，在隔离的 tmux server 上验证了：Mac 和手机互相发送、读取、回复和确认；带中文、换行和 shell 元字符的消息原样到达，没有执行任何内容；界面能在两台设备上运行；`enter` 能进入两台设备上的 pane，detach 后返回。`brew` 是经 SSH 运行的，所以没有覆盖从 Finder 打开时 macOS 对未公证二进制的提示；Agent 仍然不会主动取走消息。

2026-11-02 至 11-03：在两台设备上用 Pi（deepseek-v4.1-flash）做了 30 组两两对照和 6 轮、72 个 Agent 的社会实验，验证了 `--id` 重试不再重复、默认 server 不再误投，并发现手写返回地址约四成不可用，促成了自动填写 `--from`。详见[实验报告](../../docs/fleet-agent-society-experiment.zh-CN.md)。

这台手机上每次 SSH 连接约需 4.5 秒（往返延迟 574ms），所以这种链路上的设备会晚约 5 秒出现和刷新。已 root 的 Termux 会话中 `pkg` 拒绝以 root 运行，需要以 Termux 用户身份或在 Termux App 里安装。

## 下一步：通信解耦与上下文边界

目标是让独立 pane Agent 直接协作，而不复制彼此的完整对话。每次请求只传当前任务的目标、必要约束、可访问的文件或 commit 引用，以及希望返回的结果；缺信息时 Agent 可以直接追问。Adon 提供名称和报告状态，不能证明消息被读或任务完成。引用需要能被对方访问，不能仅发送本机路径。

**待实现，不是现有功能**：

1. 按工位别名发现目标；别名重名时明确报错，最终定位仍包含设备、tmux server 和 pane。
2. 保留请求 ID，区分已存入、已接收、完成和失败；目标忙碌或用户正在输入时不破坏当前任务和草稿。
3. 在接收 Agent 的可用接口上触发处理，并把对应处理回合的最终结果关联请求 ID 返回；发送方可等待或稍后查询。需要区分取消、失败和重试，不能将其他回合的回复误认作结果。
4. 首先验证 Pi 提供的生命周期与消息接口；其他 harness 按实际能力做可选薄适配。tmux 和 SSH 提供工位与传输，但本身不能识别所有 harness 的任务完成边界。Pi 的 [coms / coms-net](https://github.com/disler/pi-vs-claude-code/blob/main/README.md) 可供参考，并非 Fleet 的依赖。
5. 比较短任务上下文和同步完整对话的实际 token、耗时、结果质量；缓存、模型、对话轮数和重读文件都会影响费用，不能假设固定节省倍数。

第一项验收：Mac 工位给手机 Pi 发出真实短任务，手机自动接收和执行，并返回带同一请求 ID 的结果；Mac 核实结果，反向重复。测试忙碌、草稿、并发及连接失败的实际行为。当前 CLI 尚无自动通知、自动回复或持久化；任何可靠性扩展都应由实际需要驱动。
