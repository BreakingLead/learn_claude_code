from typing import List

import anthropic
from anthropic.types import (
    ContentBlock,
    MessageParam,
    ThinkingBlock,
    ToolResultBlockParam,
)
from dotenv import load_dotenv
from loguru import logger
from rich import print
from rich.markdown import Markdown
from rich.prompt import Prompt

from .constants import MODEL, SYSTEM
from .hooks import trigger_hooks
from .permission import check_permission
from .tools import TOOL_HANDLERS, TOOLS

load_dotenv(override=True)

client = anthropic.Anthropic()


rounds_since_todo = 0


# messages 是完整的对话历史，每次调用 agent_loop 时会更新
def agent_loop(messages: List[MessageParam]):
    global rounds_since_todo
    while True:
        # s05: nag reminder — inject if model hasn't updated todos for 3 rounds
        if rounds_since_todo >= 3 and messages:
            messages.append(
                {"role": "user", "content": "<reminder>Update your todos.</reminder>"}
            )
            rounds_since_todo = 0

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
            # end
            force_continue_msg = trigger_hooks("Stop", messages)
            if force_continue_msg is not None:
                messages.append({"role": "user", "content": force_continue_msg})
                continue
            else:
                return

        rounds_since_todo += 1

        tool_results: List[ToolResultBlockParam] = []

        for block in res.content:
            if block.type != "tool_use":
                continue

            deny_reason = trigger_hooks("PreToolUse", block)
            if deny_reason is not None:
                tool_results.append(
                    {
                        "type": "tool_result",
                        "tool_use_id": block.id,
                        "content": deny_reason,
                    }
                )
                continue

            if block.name not in TOOL_HANDLERS:
                logger.warning("Unknown tool: {}", block.name)
                continue

            tool_output_content = TOOL_HANDLERS[block.name](**block.input)
            logger.debug("Tool Output: {}", str(tool_output_content)[:200])

            trigger_hooks("PostToolUse", block, tool_output_content)

            # s05: reset nag counter when todo_write is called
            if block.name == "todo_write":
                rounds_since_todo = 0

            tool_results.append(
                {
                    "type": "tool_result",
                    "tool_use_id": block.id,
                    "content": tool_output_content,
                }
            )

        messages.append({"role": "user", "content": tool_results})


# ── Entry point ──────────────────────────────────────────
def main():
    print("[bold]s05: Hooks[/bold]")
    print("输入问题，回车发送。输入 q 退出。\n")

    # history 保存完整的对话历史（user/assistant 交替），供多轮对话使用
    history = []

    # REPL 主循环：读取用户输入 -> 调用 agent_loop -> 打印回复
    while True:
        try:
            # 显示带颜色的提示符，等待用户输入
            query = Prompt.ask("[cyan]s05 >>[/cyan]")
            trigger_hooks("UserPromptSubmit", query)
            # 空输入、"q"、"exit" 均退出
            if query.strip().lower() in ("q", "exit", ""):
                break
        except EOFError, KeyboardInterrupt:
            # Ctrl+D 或 Ctrl+C 时优雅退出
            break

        # 将用户消息追加到历史，然后启动 agent loop
        # agent_loop 会原地修改 history，追加 assistant 回复和工具调用结果
        history.append({"role": "user", "content": query})

        # Agent Loop
        agent_loop(history)

        # 从 history 末尾取出模型的最终回复并打印其中的文本部分
        # content 可能是 ContentBlock 列表（含 text / tool_use 等类型）
        response_content = history[-1]["content"]
        if isinstance(response_content, list):
            for block in response_content:
                # if getattr(block, "type", None) == "text":
                if isinstance(block, ThinkingBlock):
                    print(f"[u]Thinking: {block.thinking}[/u]")
                    print()
                else:
                    print(Markdown(block.text))
        print()


if __name__ == "__main__":
    main()
