# Agent Interface Design

## Goal

Bee Agent treats the main agent and subagents as the same execution primitive. They differ by configuration, not by control flow: each receives messages, assembles a system prompt, calls the model, executes allowed tools, and feeds tool results back into the loop.

## Core Interface

`agentSpec` is the boundary between identity and execution:

- `ID` and `DisplayName`: logging and debugging identity.
- `ToolNames`: the visible tool surface for this agent.
- `MaxTurns` and `MaxTokens`: lifecycle limits.
- `UseRecovery`: whether to use the full recovery state machine.
- `InjectRelevantMemories`, `InjectBackgroundNotifications`, `UseTodoReminder`: optional runtime behaviors.
- `UseCompaction`, `UseStopHooks`, `ExtractMemoriesOnStop`: main-agent lifecycle integrations.
- `ToolLogPrefix` and `ToolPreviewLimit`: display policy.

`runAgent(ctx, client, spec, messages)` owns the shared state machine. It filters tools, builds the prompt from the selected tool names, calls the model, runs hooks, executes tools, and returns `agentRunResult`.

## Current Specs

The main agent uses all tools and enables recovery, memory injection, background notifications, todo reminders, compaction, stop hooks, and memory extraction.

The subagent uses only `bash`, `read_file`, `write_file`, `edit_file`, and `glob`. It has a 30-turn limit, no recursive `task` tool, no background tools, no memory extraction, and returns only its final text to the caller.

## Design Considerations

This keeps capability boundaries explicit. A subagent cannot call another subagent unless its spec includes `task`. A future specialized agent can be added by constructing another `agentSpec` instead of copying the loop.

The runner still depends on `agentRuntime` for shared services: tool handlers, hooks, prompt cache, permissions, compaction, memory, and logging. That is intentional for now; modules can later contribute tools and prompt blocks through the same spec boundary.
