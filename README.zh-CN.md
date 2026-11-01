<div align="center">

# toyauro-fleet

**为黑灯软件工厂打造自主的 Agent 工位。**

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

## 我为什么做这个

我希望有一支真正能工作的 Agent fleet：我给出目标，Agent 使用现有工具探索、协作、编程和验证，工作可以在我离开后继续。我回来时能看到进展、结果和需要判断的问题，而不必亲自充当每个 Agent 之间的消息中转站。

手机也是一个独立的 Agent 工作环境。它可以运行自己的 Agent 和手机工具，处理手机上的任务；Mac mini 同样运行自己的 Agent 和软件工具。两台设备应能平等地交换信息、请求协助，各自保持自主。手机需要软件调试时可以找 Mac；Mac 需要真机验证时也可以找手机。

我相信 Agent 的能力会持续提高，因此想保持系统足够简单：**复用 Agent 已经会用的工具，把理解、选择工具和协作交给 Agent；只有真实使用出现重复问题时，才把必要能力沉淀为 CLI。**

## 想要建成什么

ToyaUro Fleet 是一个面向黑灯软件工厂的 monorepo：让多个独立 Agent 在可观察的工位中工作，并逐步完成从目标、协作、执行到验证和结果交付的闭环。人定义目标和验收标准，Agent 推进工作；人可以查看现场、参与判断或接手。

```text
手机 ToyaUro                              Mac mini
┌──────────────────────────┐              ┌──────────────────────────┐
│ tmux                     │              │ tmux                     │
│ Pi / 其他 Agent 工位     │◄── SSH ─────►│ Codex / 其他 Agent 工位  │
│ 手机工具、Shell、Git     │  Tailscale   │ 软件工具、Shell、Git     │
└──────────────────────────┘              └──────────────────────────┘
```

每个 tmux panel 都是平等的通用工位：任何 panel 都可以按任务命名为 `phone use`、`dev` 或其他名称，也可以随上下文改变职责。通信原型复用 tmux 与 SSH，让 Agent 按工位读取消息、发送请求和返回结果；适配不同 harness 的状态灯由 `tmux-agent-adon` 提供。

例如，一个 Mac 工位构建 App 后，请手机工位在真机上打开并检查；手机工位返回观察到的结果，Mac 工位继续修改。**这个跨设备示例是目标，尚未作为本仓库的 fleet 通信功能完成端到端验收。**

我们从 tmux、Shell、SSH 和 Git 开始，不预设数据库、消息队列或中心控制器。如果未来确实出现当前工具无法可靠解决的问题，再按需要补充；Agent 仍可直接使用底层工具。

## 近期要完成什么

1. 先在同一台设备的 tmux panel 中，验证两个不同的 Agent harness 能互相发现、发送请求并明确回复。
2. 再通过 SSH 验证手机与 Mac 两个方向的交流。
3. 用真实软件任务验证协作结果，只把实际需要重复使用的步骤沉淀成命令。

向 tmux 送入文字本身不能证明 Agent 已接收或完成请求；每一步都需要看到接收方的实际回应。

## 组件

| 组件 | 用途 | 当前状态 |
| --- | --- | --- |
| [tmux-agent-adon](packages/tmux-agent-adon/) | panel 名称、Agent 状态灯和可选适配器。 | 已从[独立 adon 仓库](https://github.com/toyauro/tmux-agent-adon)导入。 |
| [Fleet 通信](packages/fleet/) | 发现 pane，复用 tmux 命名 buffer 和 SSH 交换请求与回复。 | 实验原型；Agent 需主动查看收件箱，跨设备 Agent 实际验收见组件文档。 |
| 软件工厂 | 让协作的 Agent 构建和验证软件。 | 未来以真实任务驱动，尚未在本仓库实现。 |

独立 adon 仓库继续保留；其 MIT 许可证和提交历史保留在 `packages/tmux-agent-adon` 子树中。克隆本仓库即可获得组件源码，不需要初始化 Git submodule。

两个仓库不会自动同步。需要导入独立 adon 仓库的后续版本时，可先审阅变更，再执行：

```sh
git subtree pull --prefix=packages/tmux-agent-adon https://github.com/toyauro/tmux-agent-adon.git main
```

## 试用状态灯

克隆本仓库后，在目标 tmux server 的 shell 中执行：

```sh
export PATH="$PWD/packages/tmux-agent-adon/bin:$PATH"
tmux run-shell "$PWD/packages/tmux-agent-adon/agent-adon.tmux"
```

加载插件会注册显示格式。要在当前窗口试用内置标签：

```sh
tmux set-option -w pane-border-status top
tmux set-option -w pane-border-format '#{E:@agent_adon_label_format}'
```

这会更改当前窗口的边框格式。如需保留已有主题，请参照[显示集成指南](packages/tmux-agent-adon/examples/README.md)。插件可以独立于未来的 fleet 通信使用。

## 当前边界

- 状态灯反映报告，不证明任务完成，也不保证报告方仍然存活。
- 通信原型要求 Agent 主动查看收件箱；自动唤醒、持久化和软件工厂执行尚未实现。
- 各设备保持自主；本仓库目前不要求中心控制器、数据库或消息代理。

## 许可证

仓库使用 [MIT 许可证](LICENSE)，组件目录中保留各自许可证。
