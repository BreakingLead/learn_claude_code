# Bee Agent

Bee Agent is a local agent runtime for experimenting with customizable agents, runtime modules, and observable execution.

## Language

**Agent Blueprint**:
A declarative node-and-edge graph describing one agent's capabilities and policies. It defines which prompt modules, tool providers, memory modules, modes, and policies are assembled into the agent.
_Avoid_: Agent workflow, task graph

**Agent Builder**:
A visual or declarative editor for creating an Agent Blueprint.
_Avoid_: Workflow editor

**Agent Builder Primary Path**:
The default first-run workflow for creating, validating, and dry-running a single Agent Blueprint.
_Avoid_: Workflow orchestration, JSON-first editing

**Agent Node**:
The explicit aggregation node in an Agent Blueprint. Capability Nodes connect into an Agent Node to define one runnable agent instance.
_Avoid_: Hidden root, implicit agent

**Agent Workflow**:
A multi-agent dataflow graph that routes prompts, intermediate outputs, and summaries between agents and outputs.
_Avoid_: Blueprint, todo list

**Capability Module**:
A pluggable unit that contributes one or more capabilities to an Agent Blueprint, such as prompt blocks, tools, turn hooks, memory behavior, policies, or output adapters.
_Avoid_: Plugin, system

**Capability Node**:
An instance of a Capability Module inside an Agent Blueprint. Multiple nodes may use the same module type with different names, configuration, files, or inline prompt content.
_Avoid_: Module type

**Capability Transformer**:
A Capability Node that consumes one capability type and emits transformed capabilities, such as filtering a toolset and emitting prompt instructions.
_Avoid_: Agent policy slot

**Composite Node**:
A reusable Capability Node packaged from a subgraph of other nodes. It exposes selected input and output ports while hiding the internal graph.
_Avoid_: Macro, copy-paste group

**Inspector**:
The contextual editor panel for the selected object in Agent Builder. With no node selected it edits the Agent Blueprint; with a node selected it edits that node.
_Avoid_: Right JSON panel, properties dump

**JSON Drawer**:
The advanced bottom drawer that exposes the underlying Agent Builder JSON without occupying the default editing surface.
_Avoid_: Main editor, primary panel

**Asset Library**:
The left-side Agent Builder palette organized by user intent, such as instructions, capabilities, constraints, logic, and reusable composites.
_Avoid_: Node type list, toolbar

**Port**:
A typed input or output socket on a Blueprint node. Edges connect output ports to input ports only when their port types are compatible.
_Avoid_: Edge kind

**Skill Node**:
A Capability Node that contributes skill instructions. It may reference a local skill file or contain inline prompt text directly in the blueprint.
_Avoid_: Skill system

**Tool Provider**:
A module that contributes one or more tools to an agent runtime.
_Avoid_: Tool node, plugin

**Prompt Module**:
A module that contributes system prompt content or context blocks to an agent runtime.
_Avoid_: Prompt string, skill
