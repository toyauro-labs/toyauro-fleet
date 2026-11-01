# Fleet：基于 tmux 的工位收件箱

[English](README.md) · 简体中文

这是实验性通信 CLI：每个 pane 都可以通过 tmux 命名 buffer 收取消息，通过 SSH 与其他设备通信。没有中心服务、数据库或常驻进程。Adon 保持独立，`fleet list` 读取它的工位名称和状态。

每台设备需要 Python 3、tmux 和非交互 SSH。地址格式为 `SSH别名/tmux服务器/%pane`：SSH 别名必须是**对方机器**上能访问发送方的别名。同一台设备内发消息可以用 `.`。pane 编号只在对应的 tmux server 存活期间有效，名称和职责可以改变，不能作为唯一地址。

配置 `~/.config/toyauro-fleet/config.json` 中的设备和 tmux server 后，`fleet list` 会同时查询它们。默认列出有 Adon 名称或已接入 Adon 的 pane；`fleet list --all` 查看所有 pane，`fleet list --json` 用于脚本。宽终端显示表格，窄终端按设备分组；状态灯只在交互终端着色，设置 `NO_COLOR` 可关闭颜色。不可达设备会报错，不会静默当成没有工位。列表实时读取 pane 的 `@pane_name` 和状态；显示名称不是唯一地址。配置样例：

```json
{"nodes":[
  {"name":"macmini","server":"vibe"},
  {"name":"phone","host":"ace-ts","server":"default"}
]}
```

其中没有 `host` 的节点是本机，其余 `host` 是本机 SSH 配置中的别名；在手机上使用相反方向的配置。节点需要显式登记，Tailscale 不自动注册 tmux server。

```sh
# 在 Mac 上统一查看本机和手机工位
fleet list

# 只查看手机一个 server
fleet --host ace-ts --server default list

# 发给手机 %2：手机上用 macmini 这个 SSH 别名能找到 Mac
fleet --host ace-ts --server default send --to %2 \
  --from 'macmini/vibe/%127' --body '请检查 Android 构建。'

# 手机 Agent 在 %2 里读取和回复
fleet inbox --pane %2
fleet reply --pane %2 --from 'ace-ts/default/%2' \
  --body '已检查，构建能够启动。' 消息ID
fleet ack --pane %2 消息ID

# Mac Agent 查看自己的收件箱
fleet --server vibe inbox --pane %127
```

`reply` 带原消息 ID 和线程 ID，发送回复后不会自动删除原消息；`ack` 是收件方明确删除已处理的消息。`--body -` 可从标准输入读取多行文字。

**现在的边界：** `stored: true` 只表示消息写入了对方 tmux，不表示 Agent 已读或完成。Agent 需要主动运行 `inbox`；当前不会自动唤醒 Agent 或注入输入框。tmux server 退出后消息会消失。pane、SSH 或 server 不可用时会报错；网络异常后的重复发送会产生新 ID。SSH 凭据及 tmux server 的访问权限决定谁能读写消息。需要持久化、去重和自动通知时，再根据实际需求补充。

仓库根目录执行测试：`python3 -m unittest discover -s packages/fleet/tests -v`。

## 本次实测

2026-10-29：Mac 的 `vibe` server 工位与手机 `toyauro-fleet` 工位通过 Tailscale / SSH 双向发起、读取并回复；手机 Pi 实际读取并确认了消息。Agent 是经人工提示后查看收件箱的，这次没有验证自动通知。另用独立 tmux server 验证了跨 window、跨 session、原消息关联和错误 pane 拒绝。

## 下一步：通信解耦与上下文边界

目标是让独立 pane Agent 直接协作，而不复制彼此的完整对话。每次请求只传当前任务的目标、必要约束、可访问的文件或 commit 引用，以及希望返回的结果；缺信息时 Agent 可以直接追问。Adon 提供名称和报告状态，不能证明消息被读或任务完成。引用需要能被对方访问，不能仅发送本机路径。

**待实现，不是现有功能**：

1. 按工位别名发现目标；别名重名时明确报错，最终定位仍包含设备、tmux server 和 pane。
2. 保留请求 ID，区分已存入、已接收、完成和失败；目标忙碌或用户正在输入时不破坏当前任务和草稿。
3. 在接收 Agent 的可用接口上触发处理，并把对应处理回合的最终结果关联请求 ID 返回；发送方可等待或稍后查询。需要区分取消、失败和重试，不能将其他回合的回复误认作结果。
4. 首先验证 Pi 提供的生命周期与消息接口；其他 harness 按实际能力做可选薄适配。tmux 和 SSH 提供工位与传输，但本身不能识别所有 harness 的任务完成边界。Pi 的 [coms / coms-net](https://github.com/disler/pi-vs-claude-code/blob/main/README.md) 可供参考，并非 Fleet 的依赖。
5. 比较短任务上下文和同步完整对话的实际 token、耗时、结果质量；缓存、模型、对话轮数和重读文件都会影响费用，不能假设固定节省倍数。

第一项验收：Mac 工位给手机 Pi 发出真实短任务，手机自动接收和执行，并返回带同一请求 ID 的结果；Mac 核实结果，反向重复。测试忙碌、草稿、并发及连接失败的实际行为。当前 CLI 尚无自动通知、自动回复或持久化；任何可靠性扩展都应由实际需要驱动。
