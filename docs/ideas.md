# Ideas

## Visual Agent Builder and Workflow Editor

Date: 2026-07-05

Idea: build a visual editor for customizing agents and composing multi-agent workflows.

Current framing:

- There are two graph layers, and they should not be collapsed into one concept.
- First version should focus on the Agent Builder: a visual/declarative editor for defining one Agent Blueprint.
- An Agent Blueprint graph defines a single agent's capability dependencies: ToolProvider nodes, Prompt Module nodes, memory modules, modes, permission policies, and output channels.
- Like Blender nodes, Blueprint nodes are module instances, not module types. Users can add multiple nodes backed by the same capability type, each with different configuration.
- A Skill Node can reference a local skill file or contain inline prompt text directly in the node.
- An Agent Workflow graph defines dataflow between agents: for example `Prompt -> Agent A / Agent B -> Agent C summary -> Output`.
- The long-term goal is to let users customize their own agent by wiring capabilities together instead of editing Go code or hidden prompt text, then compose those agents into higher-level workflows.
- In the Agent Blueprint layer, nodes expose typed input/output ports. Edges connect compatible ports and encode capability dependency and runtime assembly relationships.
- In the Agent Workflow layer, edges encode message/data flow and execution order.
- The editor could make Bee Agent easier to demonstrate as an agent runtime, because the workflow becomes inspectable instead of hidden in prompt text or imperative code.

Refactor direction:

- Replace scattered feature-specific wiring with a simple unified interface for defining an agent.
- Memory, skills, modes, tools, policies, hooks, and output adapters should be represented as Capability Modules.
- Existing module concepts map roughly to this shape: prompt contribution, tool contribution, turn hooks, runtime snapshots, and policy contribution.
- The Agent Builder should produce a serializable Agent Blueprint. The runtime should instantiate an `agentSpec` from that blueprint.
- The Blueprint should contain an explicit Agent Node as the aggregation point. Capability Nodes connect into the Agent Node to define one runnable agent instance.
- The schema may contain multiple Agent Nodes, but the first runtime resolver should execute only the `root_agent` selected by the blueprint.
- The editor should expose node instance configuration explicitly: name, capability type, config fields, provided capabilities, required capabilities, and source references.
- The editor should model Blender-style typed ports. Different port types should have distinct colors, and only compatible colors/types can connect.
- `prompt` and `memory` are distinct port types. `prompt` contributes static or semi-static context blocks; `memory` contributes runtime memory behavior such as retrieval, writeback, or extraction.
- Input ports should declare cardinality. Ports like `prompt_in`, `tool_in`, and `policy_in` can allow multiple incoming edges; ports like `model_in` or singular runtime settings can reject multiple incoming edges.
- Ordered prompt composition should use multiple ordered Agent input ports instead of edge order. When one prompt input is connected, the editor can expose the next empty prompt input, similar to dynamic sockets in Blender.
- Tool inputs do not need ordering. Tool contributions form a set, and the resolver can sort tool names deterministically after collecting connected tool ports.
- Policy Nodes should be modeled as Capability Transformers, not primarily as a special Agent input. For example, a ReadOnly Policy consumes a `toolset`, emits a filtered `toolset`, and may also emit a `prompt` explaining the constraint.
- Users should be able to package reusable node groups as Composite Nodes. A Composite Node wraps an internal subgraph and exposes selected typed input/output ports for reuse.
- Composite Nodes should reference reusable definitions by default. If a user needs to customize one instance, they can manually copy/expand the composite into ordinary nodes and edit that copy.
- Composite Nodes may reference other Composite Nodes, but the resolver must detect reference cycles and enforce a maximum expansion depth.
- The Agent Builder implementation can live under `internal/agent/node_editor/`. That package should own Blueprint JSON models, validation, Web UI serving/API boundaries, and conversion into runtime-facing definitions.
- User-editable definitions should live under `.agents/blueprints/`, not under `internal/`. Suggested layout: `.agents/blueprints/agents/*.json` for Agent Blueprints and `.agents/blueprints/composites/*.json` for Composite Node definitions.
- Blueprint files should be git-trackable project configuration. Node configs must not inline secrets; secret-like values should reference environment variables or a future secret reference mechanism.
- First version should provide a simple default Agent Blueprint that mirrors the current Bee Agent behavior. This gives the Web UI something useful to display and gives the runtime refactor a compatibility target.
- The default Blueprint should be written to `.agents/blueprints/agents/default.json` on first run and should not overwrite user edits on later runs.
- The existing agent loop should consume resolved Agent Blueprints through a narrow interface instead of depending on Web UI details.
- The Blueprint JSON stores nodes and edges, not only the final flattened agent configuration. A resolver derives the runtime configuration from the graph.

Open questions:

- Should the first version implement Agent Blueprint editing, Agent Workflow execution, or only visualization/export?
- How does it relate to the existing task system, todo module, cron scheduler, modes, and team protocol?
- First version should use a Web UI with a mature node editor library. Do not build the editor in the TUI.
- JSON should be the underlying Agent Blueprint format, so the runtime can load blueprints independently of the UI.
- What is the Agent Blueprint port contract: does a port carry prompt text, tools, messages, runtime hooks, policy, output adapters, or typed data?
- What is the Agent Workflow node contract: does a node consume/produce messages, artifacts, structured JSON, or generic events?
- How should Blueprint schema migrations work after `version` changes? Defer this until the first schema proves useful.
