import os
import subprocess
from pathlib import Path
from typing import Callable, List

import anthropic
from anthropic.types import MessageParam, ThinkingBlock, ToolParam
from dotenv import load_dotenv
from typing_extensions import Dict

MODEL = "deepseek-v4-pro"
WORKDIR = Path.cwd()
SYSTEM = f"You are a coding agent at {os.getcwd()}. Use bash to solve tasks. Act, don't explain."
TOOLS: List[ToolParam] = [
    {
        "name": "bash",
        "description": "Run a shell command.",
        "input_schema": {
            "type": "object",
            "properties": {"command": {"type": "string"}},
            "required": ["command"],
        },
    },
    {
        "name": "read_file",
        "description": "Read file contents.",
        "input_schema": {
            "type": "object",
            "properties": {"path": {"type": "string"}, "limit": {"type": "integer"}},
            "required": ["path"],
        },
    },
    {
        "name": "write_file",
        "description": "Write content to a file.",
        "input_schema": {
            "type": "object",
            "properties": {"path": {"type": "string"}, "content": {"type": "string"}},
            "required": ["path", "content"],
        },
    },
    {
        "name": "edit_file",
        "description": "Replace exact text in a file once.",
        "input_schema": {
            "type": "object",
            "properties": {
                "path": {"type": "string"},
                "old_text": {"type": "string"},
                "new_text": {"type": "string"},
            },
            "required": ["path", "old_text", "new_text"],
        },
    },
    {
        "name": "glob",
        "description": "Find files matching a glob pattern.",
        "input_schema": {
            "type": "object",
            "properties": {"pattern": {"type": "string"}},
            "required": ["pattern"],
        },
    },
]


load_dotenv(override=True)

client = anthropic.Anthropic()


# ── Tool execution ────────────────────────────────────────
def safe_path(p: str) -> Path:
    path = (WORKDIR / p).resolve()
    if not path.is_relative_to(WORKDIR):
        raise ValueError(f"Path escapes workspace: {p}")
    return path


def run_bash(command: str) -> str:
    dangerous = ["rm -rf /", "sudo", "shutdown", "reboot", "> /dev/"]
    if any(d in command for d in dangerous):
        return "Error: Dangerous command blocked"
    try:
        r = subprocess.run(
            command,
            shell=True,
            cwd=os.getcwd(),
            capture_output=True,
            text=True,
            timeout=120,
        )
        out = (r.stdout + r.stderr).strip()
        return out[:50000] if out else "(no output)"
    except subprocess.TimeoutExpired:
        return "Error: Timeout (120s)"
    except (FileNotFoundError, OSError) as e:
        return f"Error: {e}"


def run_read(path, limit=None):
    try:
        lines = safe_path(path).read_text().splitlines()
        if limit:
            lines = lines[:limit]
        return "\n".join(lines)
    except Exception as e:
        return f"Error: {e}"


def run_write(path, content):
    safe_path(path).write_text(content)
    return f"Wrote {len(content)} bytes to {path}"


def run_edit(path, old_text, new_text):
    text = safe_path(path).read_text()
    if old_text not in text:
        return "Error: text not found"
    safe_path(path).write_text(text.replace(old_text, new_text, 1))
    return f"Edited {path}"


def run_glob(pattern):
    import glob as g

    try:
        results = []
        for match in g.glob(pattern, root_dir=WORKDIR):
            if (WORKDIR / match).resolve().is_relative_to(WORKDIR):
                results.append(match)
        return "\n".join(results) if results else "(no matches)"
    except Exception as e:
        return f"Error: {e}"


TOOL_HANDLERS: Dict[str, Callable] = {
    "bash": run_bash,
    "read_file": run_read,
    "write_file": run_write,
    "edit_file": run_edit,
    "glob": run_glob,
}


# messages 是完整的对话历史，每次调用 agent_loop 时会更新
def agent_loop(messages: List[MessageParam]):
    while True:
        res = client.messages.create(
            model=MODEL,
            max_tokens=8000,
            system=SYSTEM,
            tools=TOOLS,
            messages=messages,
        )
        messages.append(
            {
                "role": "assistant",
                "content": res.content,
            }
        )
        if res.stop_reason != "tool_use":
            return
        results = []
        for block in res.content:
            if block.type == "tool_use":
                if block.name not in TOOL_HANDLERS:
                    print(f"Unknown tool: {block.name}")
                    continue
                output = TOOL_HANDLERS[block.name](**block.input)
                results.append(
                    {"type": "tool_result", "tool_use_id": block.id, "content": output}
                )
        messages.append({"role": "user", "content": results})


# ── Entry point ──────────────────────────────────────────
if __name__ == "__main__":
    print("s02: Tool Use — 在 s01 基础上加了 4 个工具")
    print("输入问题，回车发送。输入 q 退出。\n")

    # history 保存完整的对话历史（user/assistant 交替），供多轮对话使用
    history = []

    # REPL 主循环：读取用户输入 -> 调用 agent_loop -> 打印回复
    while True:
        try:
            # 显示带颜色的提示符，等待用户输入
            query = input("\033[36ms01 >> \033[0m")
        except EOFError, KeyboardInterrupt:
            # Ctrl+D 或 Ctrl+C 时优雅退出
            break

        # 空输入、"q"、"exit" 均退出
        if query.strip().lower() in ("q", "exit", ""):
            break

        # 将用户消息追加到历史，然后启动 agent loop
        # agent_loop 会原地修改 history，追加 assistant 回复和工具调用结果
        history.append({"role": "user", "content": query})
        agent_loop(history)

        # 从 history 末尾取出模型的最终回复并打印其中的文本部分
        # content 可能是 ContentBlock 列表（含 text / tool_use 等类型）
        response_content = history[-1]["content"]
        if isinstance(response_content, list):
            for block in response_content:
                # if getattr(block, "type", None) == "text":
                if isinstance(block, ThinkingBlock):
                    print(block.thinking)
                else:
                    print(block.text)
        print()
