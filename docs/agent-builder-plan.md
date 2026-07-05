# Agent Builder Implementation Plan

## Phase 1: Blueprint Backend

- Define Agent Blueprint JSON as nodes, typed ports, and edges.
- Generate `.agents/blueprints/agents/default.json` on first run.
- Validate root Agent Node, port existence, port type compatibility, and input cardinality.
- Resolve the root Agent Node into a minimal runtime-facing definition.

## Phase 2: Runtime Assembly

- Map resolved prompt nodes into ordered prompt blocks. Initial support is available behind `BEE_AGENT_USE_BLUEPRINT=1`.
- Map resolved toolset nodes into tool definitions and handlers. Initial support is available behind `BEE_AGENT_USE_BLUEPRINT=1`.
- Rework mode and policy behavior into Capability Transformer nodes where practical.
- Keep existing hard-coded runtime behavior as fallback until default Blueprint parity is proven.

## Phase 3: Web Node Editor

- Add a Web UI under `internal/agent/node_editor/` backed by the Blueprint JSON API.
- Use a mature node editor library with typed, color-coded ports.
- Support loading, saving, validating, and exporting Agent Blueprints.
- Start with Agent Blueprint editing only; defer multi-agent workflow execution.

## Phase 4: Composite Nodes

- Store reusable Composite Node definitions under `.agents/blueprints/composites/`.
- Resolve nested composites with cycle detection and a maximum expansion depth.
- Provide a manual copy/expand action for local customization.

## Phase 5: Multi-Agent Workflow

- Reuse Agent Nodes as workflow participants.
- Add dataflow node/port types for messages, artifacts, summaries, and outputs.
- Define failure, retry, concurrency, and approval semantics before execution.
