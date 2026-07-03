# Go Agent — 设计文档

基于 Anthropic 官方 Go SDK (`github.com/anthropics/anthropic-sdk-go`) 实现的编码代理（coding agent），从 Python 版 `s08_context_compact` 迁移而来。

## 架构概览

```
cmd/bee-agent/      CLI 入口
internal/agent/     agent 实现，只供本项目内部使用
  main.go           REPL 入口 + agentLoop 核心循环
  runtime.go        显式运行时状态、配置、hooks、UI 事件、prompt 缓存
  module.go         内部模块共通 API、moduleManager、prompt block 收集
  constants.go      环境变量读取、终端颜色辅助
  tools.go          8 个工具的定义（JSON Schema）与处理函数
  permission.go     三层权限门控：拒绝列表 → 规则检查 → 用户确认
  hooks.go          事件钩子系统：PreToolUse / PostToolUse / Stop 等
  compact.go        四层上下文压缩：snip → micro → persist → LLM 摘要
  memory.go         持久记忆：加载相关记忆、提取新记忆、维护 MEMORY.md 索引
  recovery.go       错误恢复：context overflow、max_tokens、rate limit、overload
  task_system.go    持久任务：维护 .agents/.tasks/*.json、依赖检查和 TASKS.md 索引
  background.go     后台命令：启动后台 bash 并注入完成通知
  todo.go           任务列表管理
  subagent.go       子 agent 生成（独立对话、30 轮上限）
  skills.go         技能扫描与加载（.agents/skills/）
  system_prompt.go  系统提示词上下文收集、缓存与组装
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

输入框以 `/` 开头时会先按本地命令处理，不会发送给 agent。内置命令包括 `/help`、`/clear`、`/debug`、`/chat`、`/quit`。

## 核心流程

```
main() → REPL 读取用户输入
  ↓
agentLoop(messages)
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

## 与 Python 版的差异

- **依赖**：仅需 Go SDK + godotenv，无需 rich/loguru（用 ANSI 转义码替代）
- **类型安全**：工具输入通过 `json.RawMessage` → struct 解析，编译期类型检查
- **并发**：Go 天然支持，未来可并行执行多个工具调用
- **错误处理**：Go 的显式错误处理替代 Python 的 try/except
