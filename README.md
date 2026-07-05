# Bee Agent — 设计文档

基于 Anthropic 官方 Go SDK (`github.com/anthropics/anthropic-sdk-go`) 实现的agent.

## 架构概览

```
cmd/bee-agent/      CLI 入口
internal/agent/     agent 实现，只供本项目内部使用
  main.go           REPL 入口 + 主 agent 薄包装
  agent_runner.go   agent/subagent 统一执行状态机与 agentSpec
  runtime.go        显式运行时状态、配置、hooks、UI 事件、prompt 缓存
  module.go         内部模块共通 API、moduleManager、prompt/tool/turn hook 收集
  constants.go      环境变量读取、终端颜色辅助
  tools.go          基础工具定义（JSON Schema）与处理函数
  permission.go     三层权限门控：拒绝列表 → 规则检查 → 用户确认
  hooks.go          事件钩子系统：PreToolUse / PostToolUse / Stop 等
  compact.go        四层上下文压缩：snip → micro → persist → LLM 摘要
  memory.go         持久记忆：加载相关记忆、提取新记忆、维护 MEMORY.md 索引
  recovery.go       错误恢复：context overflow、max_tokens、rate limit、overload
  task_system.go    持久任务：维护 .agents/.tasks/*.json、依赖检查和 TASKS.md 索引
  background.go     后台命令：启动后台 bash 并注入完成通知
  cron.go           定时调度：cron 表达式、持久任务、自动交付队列
  team.go           多 agent 协作：JSONL 消息总线、协议状态、请求/响应匹配
  todo.go           todo 模块：todo_write 工具、当前任务列表、提醒 hook
  subagent.go       子 agent 生成（独立消息、受限工具、30 轮上限）
  skills.go         技能扫描与加载（.agents/skills/）
  system_prompt.go  系统提示词上下文收集、缓存与组装
  messaging.go      消息平台中间层：Feishu/Telegram payload 与统一消息格式互转
  coc.go            CoC 跑团工具：骰子、技能检定、对抗检定、San Check
```

## 运行方式

```bash
# 确保 .env 中有 ANTHROPIC_API_KEY（和可选的 ANTHROPIC_BASE_URL）
go run ./cmd/bee-agent
```

## TUI 快捷键

项目现在使用 Bubble Tea 提供交互式终端界面：

| 快捷键 | 说明 |
|--------|------|
| 1 | 切换到对话 / 日志页 |
| 2 | 切换到 Debug 页 |
| Enter | 在输入框中换行 |
| Tab | 输入 `/` 命令时补全第一个匹配项 |
| Ctrl+S | 发送当前输入 |
| Ctrl+C | 退出 |
| y / n | 权限确认时允许 / 拒绝工具调用 |

输入框以 `/` 开头时会先按本地命令处理，不会发送给 agent。内置命令包括 `/help`、`/clear`、`/new`、`/resume`、`/debug`、`/chat`、`/mode`、`/quit`。

## Session 恢复

TUI 会把当前对话保存到 `.agents/sessions/*.jsonl`。退出时终端会打印完整 session id：

```text
Session saved: sess_20260704_120000_a1b2c3
```

再次启动时，如果已有 session，会先在终端列出可恢复项；输入编号或完整 id 恢复，直接回车则新建。自动化场景可设置 `BEE_AGENT_RESUME_PROMPT=0` 跳过启动询问。

TUI 内也可以切换：

- `/new`: 保存当前 session，然后创建空的新 session。
- `/resume`: 列出可恢复 session。
- `/resume 1` 或 `/resume sess_...`: 保存当前 session，然后恢复目标 session。

## Mode 配置

`/mode` 查看当前可用模式，`/mode plan`、`/mode build` 或 `/mode coc` 切换模式。

内置模式：

- `plan`: 规划模式，不暴露 `bash`、`write_file`、`edit_file` 等可写工具，倾向先分析方案。
- `build`: 构建模式，使用当前启用模块贡献的完整工具集。
- `coc`: Call of Cthulhu 跑团模式，注入守秘人提示词，只暴露跑团、消息和少量只读工具。

可以在 `.agents/modes.json` 中添加自定义模式：

```json
{
  "default": "review",
  "modes": [
    {
      "name": "review",
      "description": "read-only review mode",
      "prompt": "Review only. Report findings first.",
      "tools": ["read_file", "glob", "bash"]
    },
    {
      "name": "safe-build",
      "description": "build without shell",
      "prompt": "Implement changes without shell commands.",
      "disable_tools": ["bash", "background_bash"]
    }
  ]
}
```

`tools` 是允许列表；未设置时继承完整工具集。`disable_tools` 会从当前工具集中移除指定工具。也可以用 `BEE_AGENT_MODE=plan` 设置启动默认模式。

## Agent Builder

Agent Builder 使用 JSON Blueprint 描述节点、typed ports 和 edges。默认配置位于 `.agents/blueprints/agents/default.json`，首次运行缺失时会自动生成。

启动 Web UI：

```bash
BEE_AGENT_NODE_EDITOR=1 go run ./cmd/bee-agent
```

默认地址是 `http://127.0.0.1:8787`，可以用 `BEE_AGENT_NODE_EDITOR_ADDR=:8787` 修改。当前 Web UI 从后端 node template registry 加载 Agent/Prompt/Skill/Toolset/Memory 节点模板，支持切换、新建、复制 blueprint，新增节点、编辑节点 label、用表单编辑 source/path/prompt/tools、拖动节点保存位置、按 typed ports 连线、Agent prompt 输入自动续位、创建 composite、展开 composite 引用、编辑 JSON、保存和校验。校验 API 返回结构化 runtime payload；右侧校验面板会显示运行命令、policy 过滤后的有效工具列表、policy trace、prompt preview 和 diagnostics。

Skill Node 支持两种配置：`{"source":"inline","prompt":"..."}` 直接写提示词，或 `{"source":"skill_file","path":".agents/skills/name/SKILL.md"}` 引用本地 skill 文件。

Memory Node 支持 `{"source":"default_memory"}`，运行时会读取当前 `.agents/.memory/MEMORY.md` 并作为 system prompt context 注入。

Policy Node 是能力转换节点：从上游 `toolset` 输入收集工具，用 `allow_tools` / `deny_tools` 过滤后输出新的 `toolset`；同时可以通过 `prompt_out` 注入约束提示词，例如要求先计划再修改文件。

Composite Node 定义存放在 `.agents/blueprints/composites/*.json`，可以把一组节点打包成可复用节点。Web UI 支持多选节点后点击 `Create Composite`，后端会根据选择集生成 composite 的内部节点、内部边和跨边界 typed ports，然后把当前图里的选择集替换成一个 composite 实例；右侧面板也可以切换到 `Composites` 直接查看和编辑 composite JSON。仓库自带 `safe-readonly-workspace.json` 作为示例；后端会在校验和实验性 runtime assembly 时展开 composite，并检测循环引用和最大展开深度。

Workflow schema 目前在后端建模：`internal/agent/node_editor/workflow.go` 定义多 Agent 消息流 DAG、默认 review pipeline、typed message ports、环检测、agent blueprint 引用检查、拓扑执行计划、断连节点诊断和不调用模型的消息流模拟。Workflow agent node 用 `agent_blueprint` 复用能力定义，用 `config.instruction` 表达它在当前 DAG 里的局部职责。Workflow JSON 存放在 `.agents/blueprints/workflows/*.json`，示例为 `review-pipeline.json`；后端提供 `/api/workflows` 的 list/get/put/delete/validate/simulate/compile/compiled-plan 接口，也提供 `/api/workflow-plans` 的 list/get/refresh/run/delete 和 `/api/workflow-runs` 的 list/get/rerun/delete/report 接口。validate 会返回执行顺序、每步输入输出、agent binding 摘要、每个 agent 展开后的工具和 prompt blocks，以及 diagnostics，simulate 会展示样例输入如何流经各个 agent 节点，compile 会生成后续 runner 可消费的 agent run 清单，compiled-plan 会写入 `.agents/blueprints/workflow_plans/{id}.json`。Compiled plan 包含 `source_hash`，workflow plan list 会对比当前 workflow JSON 并标记 stale plan。run 通过 `PlanExecutor` 消费 saved plan，再由 `AgentInvoker` 生成每个 agent 节点输出；当前支持 `dry_run` 和 `external_command` 两种 execution mode，后者把 `AgentInvocation` JSON 写入外部命令 stdin，并从 stdout 读取 `AgentInvocationResult` JSON。run 默认 30 秒超时，可通过 `timeout_ms` 覆盖；超时会取消外部命令。仓库自带 `./scripts/workflow-dry-invoker` 作为不调用模型的外部执行器示例。每次 run 都会保存到 `.agents/blueprints/workflow_runs/{workflow_id}/{run_id}.json`，成功和失败分别记录为 `completed` / `failed`，并包含 execution mode、external command、timeout、rerun_of、plan_snapshot、每步 status/error 和 duration，用于回放、复现、排查和演示执行证据；run history 列表还会汇总 step 数、失败 step 和耗时，并可下载 Markdown evidence report。Web UI 的 `Workflows` 面板支持新建、复制、删除、选择、编辑、保存、验证、模拟、编译、保存、回看、刷新、dry-run、外部命令执行、加载/复跑/删除 run history 和删除 compiled plan，并在左侧画布预览 DAG、添加 input/agent/output 节点、编辑节点 label、agent blueprint 和局部 instruction、拖动保存节点位置、按 typed message ports 连线和删边。

实验性 runtime assembly：

```bash
BEE_AGENT_USE_BLUEPRINT=1 go run ./cmd/bee-agent
```

启用后，主 agent 会从 Blueprint 解析 prompt 顺序和 toolset；未启用时仍使用现有模块装配逻辑。默认读取 `default`，也可以选择 Agent Builder 中创建的配置：

```bash
BEE_AGENT_USE_BLUEPRINT=1 BEE_AGENT_BLUEPRINT_ID=review-agent go run ./cmd/bee-agent
BEE_AGENT_USE_BLUEPRINT=1 BEE_AGENT_BLUEPRINT_PATH=.agents/blueprints/agents/review-agent.json go run ./cmd/bee-agent
```

仓库自带 `readonly-policy-agent` 示例：

```bash
BEE_AGENT_USE_BLUEPRINT=1 BEE_AGENT_BLUEPRINT_ID=readonly-policy-agent go run ./cmd/bee-agent
```

## 消息平台中间层

`messaging` 模块把外部平台 payload 统一为 `UnifiedMessage` 风格的数据：`platform`、`chat_id`、`sender_id`、`sender_name`、`text`、`message_type`、`timestamp`、`metadata`。当前内置 adapter：

- `feishu`: 支持飞书事件回调文本消息归一化，并构造飞书文本 outbound payload。
- `telegram`: 支持 Telegram update 文本消息归一化，并构造 Telegram sendMessage payload。

相关工具：`messaging_platforms`、`messaging_normalize`、`messaging_build_outbound`。后续接入新平台时实现同样的 adapter 接口即可，agent 其它部分不需要依赖平台原始字段。

### Telegram 接入

第一版 Telegram connector 使用 long polling。启用后程序运行 Telegram 入口，不启动 TUI：

```bash
BEE_AGENT_TELEGRAM=1 \
TELEGRAM_BOT_TOKEN=123456:xxx \
TELEGRAM_ALLOWED_CHATS=-1001234567890 \
BEE_AGENT_MODE=coc \
go run ./cmd/bee-agent
```

可选配置：

- `TELEGRAM_ALLOWED_CHATS`: 逗号分隔的 chat id 允许列表；为空时允许所有 chat。
- `TELEGRAM_POLL_INTERVAL`: 轮询间隔，默认 `2s`。
- `TELEGRAM_TIMEOUT`: Telegram long polling timeout，默认 `30s`。
- `TELEGRAM_BASE_URL`: 测试或代理用 API base URL，默认 `https://api.telegram.org`。

每个 Telegram chat 维护独立对话历史。收到 update 后会先归一化为统一消息，再交给 agent；agent 的最后回复通过 `sendMessage` 回到原 chat。

## CoC 跑团模块

`/mode coc` 会启用跑团提示词和常用工具：

- `coc_roll_dice`: 掷骰表达式，如 `1d100`、`2d6+3`、`d3-1`。
- `coc_skill_check`: CoC 7e D100 技能检定，支持奖励骰/惩罚骰。
- `coc_opposed_check`: 对抗检定，先比较成功等级，再比较低点数。
- `coc_sanity_check`: 理智检定，并根据成功/失败表达式计算 SAN 损失。

工具输出 JSON，方便直接回传到 TUI、飞书或 Telegram。

## 核心流程

```
main() → TUI 读取用户输入
  ↓
agentLoop(messages)
  ├── mainAgentSpec()           # 主 agent 能力配置
  └── runAgent(spec, messages)  # agent/subagent 共用状态机
  ├── getSystemPrompt()         # 由 promptContext 组装并按 context key 缓存
  ├── injectRelevantMemories()   # 从 .agents/.memory/ 加载与当前请求相关的持久记忆
  ├── injectBackgroundNotifications()
  ├── maybeCompactHistory()     # 自动上下文压缩
  ├── callModelWithRecovery()   # 调用 API 并处理可恢复错误
  ├── triggerHooks(PreToolUse)   # 权限检查
  ├── toolHandlers()[name]()    # 执行当前 runtime 绑定的工具
  ├── triggerHooks(PostToolUse)  # 输出检查
  ├── 收集 tool_result → 追加到消息
  ├── extractMemories()          # 停止后提取稳定偏好/事实/决策
  └── StopReason != ToolUse → 触发 Stop 钩子 → 返回
```

agent/subagent 的统一接口和设计记录见 `docs/agent-interface.md`。

## 持久记忆

`.agents/.memory/` 保存跨会话记忆：

```text
.agents/.memory/
├── MEMORY.md        # 记忆索引
└── *.md             # 单条记忆，包含 YAML frontmatter
```

每轮开始时会按关键词从单条记忆中选取相关内容注入上下文；每轮结束后会请求模型提取稳定偏好、项目事实、决策和约束，写入新的记忆文件并重建索引。

## 持久任务与后台命令

`.agents/.tasks/` 保存跨会话任务图：

```text
.agents/.tasks/
├── TASKS.md         # 任务索引，注入 system prompt
└── *.json           # 单个任务：id、subject、description、status、owner、blockedBy
```

相关工具：`task_create`、`task_list`、`task_get`、`task_claim`、`task_complete`。`blockedBy` 中的依赖任务必须全部 `completed` 后，任务才能被认领。

后台命令通过 `background_bash` 启动，`background_status` 查看单个 job，`background_list` 列出所有 job。后台 job 完成后会在下一次模型调用前注入 `<background>` 内部消息，并在右侧日志显示完成状态。

定时任务通过 `schedule_cron` 注册五段式 cron 表达式，`list_crons` 查看，`cancel_cron` 取消。默认 durable 的任务写入 `.scheduled_tasks.json`，Agent 进程运行时每秒检查一次，到点后在空闲时自动注入 `[Scheduled] ...` 消息并启动一轮执行。

## 多 Agent 协作

`team` 模块使用 append-only JSONL 文件传递结构化消息：

```text
.agents/team/
└── messages.jsonl   # sender、target、type、content、metadata
```

相关工具：`team_send_message`、`team_check_inbox`、`team_request_shutdown`、`team_request_plan_approval`、`team_respond_protocol`、`team_protocol_status`。

协议请求会生成 `request_id` 并进入 `pending` 状态；响应消息通过相同 `request_id` 匹配，并按协议类型校验后更新为 `approved` 或 `rejected`。当前实现聚焦 Lead 侧协调和协议状态机，后续可在同一 JSONL 总线上扩展长期 teammate worker、idle loop 和执行门控。

## 错误恢复

模型调用统一走 `callModelWithRecovery()`：

- context overflow：保存 transcript 后 reactive compact 并重试
- rate limit / overload：指数退避重试
- max_tokens：提升输出 token 上限并自动续写一次

## 工具列表

| 工具 | 说明 |
|------|------|
| bash | 执行 shell 命令（120s 超时） |
| read_file | 读取文件内容 |
| write_file | 写入文件 |
| edit_file | 精确替换文件中的文本 |
| glob | 按模式搜索文件 |
| todo_write | 管理任务列表 |
| task | 启动子 agent 处理子任务 |
| load_skill | 加载技能详细说明 |

## 权限系统（三层门控）

1. **拒绝列表** — `rm -rf /`, `sudo`, `shutdown` 等硬性禁止
2. **规则检查** — 写入工作区外、`rm` 命令、`chmod 777` 等模式匹配
3. **用户确认** — 规则命中后交互式确认（y/N）

## 上下文压缩（四层递进）

| 层级 | 策略 | 说明 |
|------|------|------|
| L1 | snipCompact | 保留头 3 + 尾 N 条，中间裁掉 |
| L2 | microCompact | 旧 tool_result 替换为占位符 |
| L3 | toolResultBudget | 超 30KB 的结果落盘 |
| L4 | compactHistory | 调用 LLM 生成摘要 |
| 紧急 | reactiveCompact | API 400 时保存 + 摘要 + 保留尾部 |
