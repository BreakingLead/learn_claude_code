# Repository Guidelines

## Project Structure & Module Organization

Bee Agent is a Go CLI/TUI coding agent. The executable entrypoint is `cmd/bee-agent/main.go`; implementation lives in `internal/agent/` so it remains private to this module.

Important files:

- `internal/agent/main.go`: agent loop and CLI mode.
- `internal/agent/runtime.go`: explicit runtime state, hooks, UI channels, prompt cache, and approvals.
- `internal/agent/tui.go`: Bubble Tea interface, panes, tabs, markdown rendering, and input handling.
- `internal/agent/tools.go`: tool schemas and runtime-bound handlers.
- `internal/agent/memory.go`, `skills.go`, `task_system.go`: persistent modules used by the agent.

Keep tests next to the code they cover as `*_test.go`.

## Build, Test, and Development Commands

Run the TUI locally:

```bash
go run ./cmd/bee-agent
```

Run the full test suite:

```bash
go test ./...
```

Format edited Go files before committing:

```bash
gofmt -w cmd internal
```

Refresh dependency metadata after changing imports:

```bash
go mod tidy
```

## Coding Style & Naming Conventions

Use standard Go formatting and idioms. Prefer small functions, explicit error handling, and narrow structs over package-level mutable state. Runtime behavior should be initialized explicitly through `agentRuntime`, TUI models, registries, or module constructors.

Use clear Go names: exported identifiers need comments when they become public API; unexported helpers should describe behavior without repeating their package name. Keep Chinese comments where they explain project-specific design or state-machine behavior.

## Testing Guidelines

Use Go’s standard `testing` package. Add focused tests for pure logic: command parsing, task dependency checks, memory frontmatter parsing, permission rules, prompt assembly, and recovery behavior. Prefer behavior-based names such as `TestSlashCommandRegistryCompletesByPrefix`.

Always run `go test ./...` before committing.

## Commit & Pull Request Guidelines

Use short imperative commits with a bracketed type when useful, for example `[feat] add tui slash commands` or `[docs] document memory design`.

Pull requests should include what changed, why it changed, commands run, and screenshots or terminal captures for TUI changes. Mention config changes such as new `.env` keys.

## Security & Configuration Tips

Keep secrets in `.env`; never commit API keys. The app expects `ANTHROPIC_API_KEY` and optionally `ANTHROPIC_BASE_URL` or `MODEL`. Preserve workspace path checks and approval gates when editing file or shell tools.
