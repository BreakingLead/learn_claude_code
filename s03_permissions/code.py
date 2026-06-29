from typing import List

import anthropic
from anthropic.types import MessageParam, ThinkingBlock
from dotenv import load_dotenv

from s03_permissions.permission import check_permission

from .constants import MODEL, SYSTEM, TOOLS
from .tools import TOOL_HANDLERS

load_dotenv(override=True)

client = anthropic.Anthropic()


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
            if block.type != "tool_use":
                continue

            print(f"\033[36m> Tool Calling: {block.name}\033[0m")

            if block.name not in TOOL_HANDLERS:
                print(f"Unknown tool: {block.name}")
                continue

            if not check_permission(block):
                results.append(
                    {
                        "type": "tool_result",
                        "tool_use_id": block.id,
                        "content": "Permission denied.",
                    }
                )
                continue

            output = (
                TOOL_HANDLERS[block.name](**block.input)
                if block.name in TOOL_HANDLERS
                else f"Unknown tool {block.name}"
            )

            print("Tool Output: " + str(output)[:200])

            results.append(
                {"type": "tool_result", "tool_use_id": block.id, "content": output}
            )
        messages.append({"role": "user", "content": results})


# ── Entry point ──────────────────────────────────────────
def main():
    print("s03: Permissions — 在 s02 基础上加了权限控制")
    print("输入问题，回车发送。输入 q 退出。\n")

    # history 保存完整的对话历史（user/assistant 交替），供多轮对话使用
    history = []

    # REPL 主循环：读取用户输入 -> 调用 agent_loop -> 打印回复
    while True:
        try:
            # 显示带颜色的提示符，等待用户输入
            query = input("\033[36ms03 >> \033[0m")
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


if __name__ == "__main__":
    main()
