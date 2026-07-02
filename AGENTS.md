# Repository Guidelines

## Project Structure & Module Organization

This is a Go CLI/TUI coding-agent project. The executable entrypoint lives in `cmd/go-agent/main.go`. Internal implementation belongs under `internal/agent/`, which is intentionally private to this module.

Key files:

- `internal/agent/main.go`: main agent loop and runtime entry.
- `internal/agent/runtime.go`: explicit runtime state for hooks, UI events, approvals, todos, and reminders.
- `internal/agent/tui.go`: Bubble Tea two-pane terminal UI.
- `internal/agent/tools.go`: tool schemas and handlers.
- `internal/agent/permission.go`: deny-list, rule checks, and user approval.
- `internal/agent/compact.go`: context compaction and transcript persistence.

There are currently no dedicated test directories; place Go unit tests next to the code they cover as `*_test.go`.

## Build, Test, and Development Commands

Run locally:

```bash
go run ./cmd/go-agent
```

Run all tests and vet checks triggered by `go test`:

```bash
go test ./...
```

Format all Go code before committing:

```bash
gofmt -w cmd internal
```

Refresh module metadata after dependency changes:

```bash
go mod tidy
```

## Coding Style & Naming Conventions

Use standard Go formatting with tabs via `gofmt`. Prefer small packages, explicit error handling, and short names where context is clear. Keep project-private code in `internal/agent`.

Avoid package-level mutable state for runtime behavior. Initialize state explicitly through `agentRuntime` or another narrow struct. Prefer methods when behavior depends on runtime state, for example `rt.agentLoop(...)` or `rt.triggerHooks(...)`.

## Testing Guidelines

Use Go’s standard `testing` package unless a specific dependency is justified. Add focused table-driven tests for pure logic such as path safety, frontmatter parsing, permission rules, and compaction behavior.

Name tests by behavior:

```go
func TestParseFrontmatterReturnsMetadata(t *testing.T) {}
```

Always run `go test ./...` after changes.

## Commit & Pull Request Guidelines

Recent commits are short and informal. Prefer concise imperative messages that describe the change, for example `add bubble tea tui` or `remove global runtime state`.

For pull requests, include:

- What changed and why.
- Commands run, especially `go test ./...`.
- Screenshots or terminal captures for TUI changes.
- Notes for config or environment changes.

## Security & Configuration Tips

Keep secrets in `.env`; do not commit API keys. The app expects `ANTHROPIC_API_KEY` and optionally `ANTHROPIC_BASE_URL` or `MODEL`. Preserve workspace path checks in `safePath` and permission gates when modifying file or shell tools.
