# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A step-by-step learning project that builds a coding agent using the Anthropic Python SDK. Each `s0X_*` directory is an incremental lesson that adds one concept on top of the previous one:

- **s01_agent_loop** — Minimal agent: single-tool (bash) REPL with an LLM loop
- **s02_tool_dispatch** — Multi-tool dispatch (bash, read_file, write_file, edit_file, glob)
- **s03_permissions** — Adds a 3-gate permission system (deny list → rule check → user approval)
- **s04_hooks** — Adds a hook/event system (UserPromptSubmit, PreToolUse, PostToolUse, Stop)

Each section beyond s01 is a Python package (`__init__.py`) with a shared structure:
- `constants.py` — MODEL, WORKDIR, SYSTEM prompt
- `tools.py` — Tool handler functions, TOOL_HANDLERS dict, TOOLS schema list
- `permission.py` — Permission gates
- `hooks.py` — Hook registry (s04+)
- `code.py` — Agent loop and REPL entry point

## Commands

```bash
# Install dependencies (uses uv, not pip)
uv sync

# Run a specific section
uv run python -m s01_agent_loop.code
uv run python -m s04_hooks.code

# Run the earlier standalone sections
uv run python s01_agent_loop/code.py
uv run python s02_tool_dispatch/code.py
```

## Conventions

- Python ≥3.14 — use modern syntax: `X | Y` unions, `type` statement for aliases, `list[T]`/`dict[K,V]` builtins (no `typing.List`/`typing.Dict`)
- Import `Callable` from `collections.abc`, not `typing`
- Comments and docstrings are in Chinese
- Model is configured as `deepseek-v4-pro` (via OpenAI-compatible Anthropic client)
- API key loaded via `python-dotenv` from `.env`
- When asked to modify code, always work in the **latest (highest-numbered) s0X directory** unless told otherwise
