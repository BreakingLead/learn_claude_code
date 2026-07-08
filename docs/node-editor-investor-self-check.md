# Node Editor 投资人视角产品自查

## 一个故事

一个团队想把 AI Agent 用进研发流程：一个 Agent 负责读代码，一个 Agent 负责改代码，一个 Agent 负责审查，一个 Agent 负责总结。最开始他们用 prompt、脚本和配置文件拼起来，能跑，但每次出问题都很难回答三个问题：这个 Agent 到底能用哪些工具？哪些提示词生效了？为什么 Reviewer 没有等 Developer 的输出就开始审查？

这就是 Bee Agent Node Editor 要解决的问题：把 Agent 的能力、约束、记忆、提示词和多 Agent 数据流从隐藏配置变成可视化、可验证、可运行、可复盘的图。

## 产品受众

第一目标用户是正在构建 Agent 工作流的开发者、AI Infra 工程师、技术团队负责人和研究型团队。他们懂模型和工具调用，但需要更可控的方式管理 Agent 能力边界、提示词组合、多 Agent 数据流和运行证据。

第二目标用户是希望把 Agent 能力产品化的团队：他们需要让非核心工程师也能调整 Agent 行为，而不是每次改 prompt 或工具权限都进入代码。

## 产品定位

Bee Agent Node Editor 是一个面向 Agent Runtime 的可视化配置与编排层。它不是通用低代码平台，也不是单纯聊天界面，而是把 Agent 定义为可版本化 JSON 图结构，并将图结构解析为真实 runtime 行为。

定位一句话：**面向开发者的 Agent Blueprint 与 Workflow 操作台。**

## 核心功能

- Agent Blueprint：用节点图定义单个 Agent 的提示词、工具、记忆和 policy。
- Typed Ports：用端口类型限制非法连接，避免无意义图进入运行时。
- Prompt 顺序：通过 Agent 节点的多个 prompt 输入表达稳定组装顺序。
- Policy Tool Filtering：policy 节点过滤 toolset，并注入约束提示词。
- Composite：把常用子图打包成可复用节点。
- Workflow：定义多 Agent 消息流 DAG，支持 validate、simulate、compile、run history。
- Execution Modes：支持 `dry_run`、`external_command` 和最小闭环 `bee_agent` 模型执行。
- Evidence：保存 workflow run、plan snapshot、step 状态、耗时和 Markdown report。

## 解决的痛点

1. **Agent 行为不可见**：传统实现把 prompt、tools、memory、mode 分散在代码里。Node Editor 把它们显式连接到 Agent 节点。
2. **能力边界不可控**：工具权限容易通过代码或 prompt 隐式变化。Toolset + Policy 让工具能力成为图上的可审查资产。
3. **多 Agent 编排难调试**：并发、依赖和消息流不清晰。Workflow DAG 用 typed message ports 表达执行顺序和数据依赖。
4. **配置不可复盘**：一次运行失败后很难知道当时的配置。Compiled plan 和 run history 保存运行证据。
5. **复用成本高**：常见能力组合需要复制粘贴。Composite 提供可复用节点组。

## 竞品与差异

参考对象包括代码型 Agent 工具、工作流编排工具、可视化 AI 应用平台和图式 Agent 框架。当前尚未完成系统化竞品调研，因此以下是基于产品形态的初步判断：

- 相比聊天式 coding agent，Node Editor 更强调可视化能力边界、可验证配置和可复盘运行记录。
- 相比通用 AI workflow 平台，Bee Agent 更贴近本地开发者工作区、工具权限、代码上下文和工程化运行证据。
- 相比代码优先的 graph framework，Node Editor 降低了理解和调试 Agent 结构的门槛，同时保留 JSON 作为底层格式。

核心竞争点不是“也能画节点”，而是：**图结构直接服务于 Agent Runtime，能从设计、验证、执行到证据沉淀形成闭环。**

## 场景适配自查

| 场景 | 当前满足度 | 说明 |
| --- | --- | --- |
| 单 Agent 能力配置 | 高 | Blueprint 已支持 prompt/toolset/memory/policy/dataflow/composite。 |
| 单 Agent 真实运行 | 中 | TUI 可通过 `--use-blueprint` 使用 Blueprint。 |
| 多 Agent DAG 设计 | 中高 | Workflow 支持 typed DAG、validate、simulate、compile。 |
| 多 Agent 真实模型执行 | 中 | `bee_agent` 已接入最小单次模型调用，暂不支持工具循环。 |
| 企业级权限治理 | 低 | 已有 policy 概念，但还没有角色、审计、审批流和租户模型。 |
| 非技术用户使用 | 低到中 | 可视化降低门槛，但概念仍偏开发者。 |
| 演示与教学 | 高 | 能展示 Agent Runtime 从硬编码到模块化、图式化、可执行的演进。 |

## 需求调研自查

目前没有完成正式用户访谈、付费意愿验证或系统竞品矩阵。已有输入主要来自：

- 自己构建 Agent runtime 时遇到的真实工程问题。
- 对 coding agent、workflow DAG、memory、skills、policy、session、cron、多 Agent 协议等模块的实现经验。
- 对 Blender 式节点编辑、typed ports、composite/node group 等交互范式的借鉴。

结论：**产品方向有明确工程痛点支撑，但还需要补充真实用户调研和竞品分析，尤其是 AI Infra 团队、Agent 平台团队和自动化研发团队的访谈。**

## 业务理解自查

已理解的业务逻辑：

- Agent 不是一个 prompt，而是 prompt、tools、memory、policy、mode、runtime state 的组合。
- 多 Agent 不是简单并发，而是有输入输出、依赖关系、失败证据和可重放需求的 DAG。
- 开发者愿意接受 JSON 作为底层格式，但不希望把 JSON 当作主要交互界面。
- Agent 执行必须区分 dry-run、外部执行、内置模型执行和未来完整工具循环。

仍需补充的业务理解：

- 企业团队如何审批 Agent 工具权限。
- Agent workflow 在真实研发流程中如何与 CI、代码评审、任务系统集成。
- 用户更愿意按 Agent 模板、Workflow 模板还是 Composite 资产来复用能力。

## 历史逻辑自查

这个需求不是凭空出现的。项目历史路径是：

1. 先实现 CLI/TUI coding agent。
2. 引入 memory、skills、todo、task、cron、team protocol 等能力。
3. 发现主循环硬编码能力会越来越难维护。
4. 抽象 agent/subagent 统一 runner 和模块 API。
5. 设计 Agent Blueprint，把能力依赖改成图结构。
6. 设计 Workflow，把多 Agent 消息流从单 Agent 能力定义中拆出来。
7. 引入 Web Node Editor，让这些 runtime 概念可以被看见、编辑、验证和执行。

因此 Node Editor 是 runtime 模块化之后的自然产物，不是单独为了“做一个漂亮 UI”。

## 产品目标

短期目标：

- 让用户能在 5 分钟内创建一个 Agent Blueprint，并用 TUI 或 dry-run 验证它。
- 让用户能创建一个多 Agent Workflow，并看到消息如何流过 DAG。
- 让每次 Workflow run 都留下可复盘证据。

中期目标：

- 让 `bee_agent` execution mode 支持完整工具循环、权限审批和失败恢复。
- 让 Composite 成为可管理的 Agent 能力资产。
- 支持模板库、版本迁移、权限审计和团队协作。

长期目标：

- 成为开发者构建、调试、治理 Agent Runtime 的本地优先操作台。

## 投资人视角结论

这个产品切中的不是“又一个 Agent 聊天框”，而是 Agent 工程化之后必然出现的配置、治理、可观测和复用问题。当前原型已经证明了核心技术路径：Blueprint 可解析为 runtime，Workflow 可编译和执行，运行证据可保存。

最大的风险是市场验证还不充分：需要证明足够多团队愿意为“可视化 Agent Runtime 操作台”付费，而不是继续用代码、脚本或现有 workflow 平台解决。下一步应该围绕 5 到 10 个目标用户访谈，验证他们在 Agent 配置、权限治理、多 Agent 编排和运行复盘上的真实痛点强度。
