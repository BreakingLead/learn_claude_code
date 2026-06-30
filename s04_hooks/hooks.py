from collections.abc import Callable
from typing import Any, Literal

from loguru import logger
from rich import print

from s04_hooks.constants import WORKDIR
from s04_hooks.permission import permission_hook

type HookEventType = Literal["UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop"]
type HookCallback = Callable[..., str | None]

HOOKS: dict[HookEventType, list[HookCallback]] = {
    "UserPromptSubmit": [],
    "PreToolUse": [],
    "PostToolUse": [],
    "Stop": [],
}


def register_hook(event: HookEventType, callback: HookCallback) -> None:
    HOOKS[event].append(callback)


def trigger_hooks(event: HookEventType, *args: Any) -> str | None:
    for callback in HOOKS[event]:
        result = callback(*args)
        if result is not None:  # 返回值 != None → hook 说"停"
            return result
    return None


# --------------------- DEFAULT HOOKS ----------------------


def _context_inject_hook(query: str) -> str | None:
    """Inject current working directory info into every prompt."""
    logger.debug("[HOOK] UserPromptSubmit: working in {}", WORKDIR)
    return None  # return None = no modification, let prompt through


def _log_hook(block):
    """PreToolUse: log every tool call."""
    args_preview = str(list(block.input.values())[:2])[:60]
    print(f"[dim] [HOOK] use tool {block.name}({args_preview})[/dim]")
    return None


def _large_output_hook(block, output):
    """PostToolUse: warn on large output."""
    if len(str(output)) > 100000:
        print(
            f"[yellow] [HOOK] ⚠ Large output from {block.name}: {len(str(output))} chars[/yellow]"
        )
    return None


# Stop hook: print summary when loop is about to exit
def _summary_hook(messages: list):
    tool_count = sum(
        1
        for m in messages
        for b in (m.get("content") if isinstance(m.get("content"), list) else [])
        if isinstance(b, dict) and b.get("type") == "tool_result"
    )
    print(f"[dim] [HOOK] Stop: session used {tool_count} tool calls[/dim]")
    return None


register_hook("UserPromptSubmit", _context_inject_hook)
register_hook("PreToolUse", permission_hook)
register_hook("PreToolUse", _log_hook)
register_hook("PostToolUse", _large_output_hook)
register_hook("Stop", _summary_hook)
