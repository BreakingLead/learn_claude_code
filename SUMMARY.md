# Project Summary: learn-claude-code

## Overview
- **Project name:** learn-claude-code
- **Version:** 0.1.0
- **Python requirement:** >=3.14
- **Description:** A learning project for Claude Code (no description provided in pyproject.toml).

## README.md Status
The README.md file exists but is currently **empty** (no content).

## requirements.txt Status
No `requirements.txt` file exists. This project uses **uv** (Python package manager) with `pyproject.toml` and `uv.lock` for dependency management.

## Dependencies (from pyproject.toml)
| Package         | Version     |
|-----------------|-------------|
| anthropic       | >=0.112.0   |
| python-dotenv   | >=1.2.2     |

## Project Structure
- `main.py` — Entry point that prints "Hello from learn-claude-code!"
- `pyproject.toml` — Project configuration and dependencies
- `uv.lock` — Lock file for deterministic dependency resolution
- `parse_jsonl_line.py` — Utility for parsing JSONL files
- `2026-06-24.jsonl` — JSONL data file
- `s01_agent_loop/` — Agent loop module/package
- `s02_tool_dispatch/` — Tool dispatch module/package
- `s03_permissions/` — Permissions module/package

## Key Observations
1. The project uses **Anthropic's Python SDK** (`anthropic>=0.112.0`), suggesting it interacts with Claude/Anthropic APIs.
2. **python-dotenv** is used for environment variable management (likely for API keys).
3. The directory structure suggests a step-by-step learning approach: agent loop → tool dispatch → permissions.
4. Python 3.14 is required, which is notably a very recent/future Python version.
