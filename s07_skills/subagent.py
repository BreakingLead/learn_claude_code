import anthropic
from anthropic.types import MessageParam
from dotenv import load_dotenv
from rich import print

from .constants import MODEL, SYSTEM
from .hooks import trigger_hooks

load_dotenv(override=True)

client = anthropic.Anthropic()


# 子 agent 输出的前缀：竖线 + 缩进，模拟嵌套层级
_PREFIX = "[dim]  │[/dim] "


def extract_text(content) -> str:
    """从消息 content blocks 中提取纯文本。"""
    if not isinstance(content, list):
        return str(content)
    return "\n".join(
        getattr(b, "text", "") for b in content if getattr(b, "type", None) == "text"
    )


def spawn_subagent(description: str) -> str:
    """启动一个子 agent，独立对话历史，仅返回最终摘要。"""
    # 延迟导入，避免 tools <-> subagent 循环依赖
    from .tools import TOOLS, run_bash, run_edit, run_glob, run_read, run_write

    sub_tools = list(filter(lambda t: t["name"] != "task", TOOLS))
    sub_handlers = {
        "bash": run_bash,
        "read_file": run_read,
        "write_file": run_write,
        "edit_file": run_edit,
        "glob": run_glob,
    }
    print(f"[dim]  ┌── [bold cyan]Subagent[/bold cyan] spawned ──[/dim]")

    messages: list[MessageParam] = [{"role": "user", "content": description}]

    for turn in range(30):  # 安全上限，防止无限循环
        response = client.messages.create(
            model=MODEL,
            system=SYSTEM,
            messages=messages,
            tools=sub_tools,
            max_tokens=8000,
        )
        messages.append({"role": "assistant", "content": response.content})
        if response.stop_reason != "tool_use":
            break
        results = []
        for block in response.content:
            if block.type == "tool_use":
                # 子 agent 同样受 hook 管控（权限检查等）
                blocked = trigger_hooks("PreToolUse", block)
                if blocked:
                    results.append(
                        {
                            "type": "tool_result",
                            "tool_use_id": block.id,
                            "content": str(blocked),
                        }
                    )
                    continue
                handler = sub_handlers.get(block.name)
                output = handler(**block.input) if handler else f"Unknown: {block.name}"
                trigger_hooks("PostToolUse", block, output)
                # 带竖线缩进的工具调用日志
                preview = str(output)[:100].replace("\n", " ")
                print(f"{_PREFIX}[dim]{block.name}:[/dim] {preview}")
                results.append(
                    {"type": "tool_result", "tool_use_id": block.id, "content": output}
                )
        messages.append({"role": "user", "content": results})

    # 提取最终回复文本；若安全上限耗尽则回溯查找
    result = extract_text(messages[-1]["content"])
    if not result:
        for msg in reversed(messages):
            if msg["role"] == "assistant":
                result = extract_text(msg["content"])
                if result:
                    break
        if not result:
            result = "Subagent stopped after 30 turns without final answer."

    print(f"[dim]  └── [bold cyan]Subagent[/bold cyan] done ──[/dim]")
    return result  # 仅返回摘要，子 agent 的完整对话历史被丢弃
