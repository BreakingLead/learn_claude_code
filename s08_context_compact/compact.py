import json
import time
from pathlib import Path
from typing import Any

import anthropic
from dotenv import load_dotenv

from .constants import MODEL, WORKDIR

# 触发自动摘要压缩的粗略上下文阈值。这里用字符串长度估算，足够做教学版保护。
CONTEXT_LIMIT = 50_000

# L2 压缩时保留最近几个 tool_result 的完整内容，旧结果替换成占位符。
KEEP_RECENT = 3

# 单个工具结果超过这个阈值时，优先把完整内容落盘，只给模型保留预览。
PERSIST_THRESHOLD = 30_000

# 大输出和完整 transcript 都写到工作区内，避免写到用户主目录或不可写缓存目录。
COMPACT_DIR = WORKDIR / ".agent_state" / "compact"
TOOL_RESULTS_DIR = COMPACT_DIR / "tool_results"
TRANSCRIPT_DIR = COMPACT_DIR / "transcripts"


def estimate_size(messages: list[dict[str, Any]]) -> int:
    """粗略估算 messages 当前占用的上下文大小。"""
    return len(str(messages))


def _get_client() -> anthropic.Anthropic:
    """延迟创建 Anthropic client，避免导入 compact.py 时就要求环境变量齐全。"""
    load_dotenv(override=True)
    return anthropic.Anthropic()


def _block_type(block: Any) -> str | None:
    """兼容 dict block 和 Anthropic SDK 返回的对象 block。"""
    return (
        block.get("type") if isinstance(block, dict) else getattr(block, "type", None)
    )


def _message_has_tool_use(message: dict[str, Any]) -> bool:
    """判断 assistant 消息中是否包含 tool_use block。"""
    if message.get("role") != "assistant":
        return False

    content = message.get("content")
    if not isinstance(content, list):
        return False

    return any(_block_type(block) == "tool_use" for block in content)


def _is_tool_result_message(message: dict[str, Any]) -> bool:
    """判断 user 消息是否是工具调用结果消息。"""
    if message.get("role") != "user":
        return False

    content = message.get("content")
    if not isinstance(content, list):
        return False

    return any(
        isinstance(block, dict) and block.get("type") == "tool_result"
        for block in content
    )


def _safe_filename(value: Any) -> str:
    """把 tool_use_id 变成安全文件名，防止路径穿越或特殊字符写盘失败。"""
    text = str(value) or "unknown"
    safe = "".join(ch if ch.isalnum() or ch in "-_" else "_" for ch in text)
    return safe or "unknown"


# L1: snipCompact - 裁掉对话中间部分，只保留开头、结尾和一条占位说明。
def snip_compact(
    messages: list[dict[str, Any]], max_messages: int = 50
) -> list[dict[str, Any]]:
    """当消息数量过多时，删除中间历史，同时尽量不切断 tool_use/tool_result 配对。"""
    if len(messages) <= max_messages:
        return messages

    keep_head = 3
    keep_tail = max_messages - keep_head
    head_end = keep_head
    tail_start = len(messages) - keep_tail

    # 如果头部最后一条是 tool_use，必须把紧随其后的 tool_result 一并保留。
    if head_end > 0 and _message_has_tool_use(messages[head_end - 1]):
        while head_end < len(messages) and _is_tool_result_message(messages[head_end]):
            head_end += 1

    # 如果尾部第一条是 tool_result，必须把它前面的 assistant tool_use 一并保留。
    if (
        tail_start > 0
        and tail_start < len(messages)
        and _is_tool_result_message(messages[tail_start])
        and _message_has_tool_use(messages[tail_start - 1])
    ):
        tail_start -= 1

    if head_end >= tail_start:
        return messages

    snipped = tail_start - head_end
    return (
        messages[:head_end]
        + [{"role": "user", "content": f"[snipped {snipped} messages]"}]
        + messages[tail_start:]
    )


def collect_tool_results(
    messages: list[dict[str, Any]],
) -> list[tuple[int, int, dict[str, Any]]]:
    """收集所有 tool_result block 的位置和对象引用，供 L2/L3 复用。"""
    blocks: list[tuple[int, int, dict[str, Any]]] = []
    for message_index, message in enumerate(messages):
        if message.get("role") != "user" or not isinstance(message.get("content"), list):
            continue

        for block_index, block in enumerate(message["content"]):
            if isinstance(block, dict) and block.get("type") == "tool_result":
                blocks.append((message_index, block_index, block))

    return blocks


# L2: microCompact - 把较旧的长工具结果替换成短占位符。
def micro_compact(messages: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """保留最近 KEEP_RECENT 个 tool_result，其余较长结果压成占位文本。"""
    tool_results = collect_tool_results(messages)
    if len(tool_results) <= KEEP_RECENT:
        return messages

    for _, _, block in tool_results[:-KEEP_RECENT]:
        content = str(block.get("content", ""))
        if len(content) > 120:
            block["content"] = "[Earlier tool result compacted. Re-run if needed.]"

    return messages


# L3: toolResultBudget - 当最近一批工具结果总量太大时，把最大结果落盘。
def persist_large_output(tool_use_id: Any, output: Any) -> str:
    """把超大工具输出写入文件，并返回包含路径和预览的轻量内容。"""
    text = str(output)
    if len(text) <= PERSIST_THRESHOLD:
        return text

    TOOL_RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    path = TOOL_RESULTS_DIR / f"{_safe_filename(tool_use_id)}.txt"
    if not path.exists():
        path.write_text(text)

    return (
        "<persisted-output>\n"
        f"Full output: {path}\n"
        f"Preview:\n{text[:2000]}\n"
        "</persisted-output>"
    )


def tool_result_budget(
    messages: list[dict[str, Any]], max_bytes: int = 200_000
) -> list[dict[str, Any]]:
    """落盘最后一条消息中的大工具结果，并限制这批结果的总体大小。"""
    last = messages[-1] if messages else None
    if (
        not last
        or last.get("role") != "user"
        or not isinstance(last.get("content"), list)
    ):
        return messages

    blocks = [
        (index, block)
        for index, block in enumerate(last["content"])
        if isinstance(block, dict) and block.get("type") == "tool_result"
    ]

    # 先处理单个超大结果，保证 L2 占位符替换前仍有完整内容可写入文件。
    for _, block in blocks:
        content = str(block.get("content", ""))
        if len(content) > PERSIST_THRESHOLD:
            block["content"] = persist_large_output(block.get("tool_use_id", "unknown"), content)

    total = sum(len(str(block.get("content", ""))) for _, block in blocks)
    if total <= max_bytes:
        return messages

    ranked = sorted(
        blocks, key=lambda pair: len(str(pair[1].get("content", ""))), reverse=True
    )
    for _, block in ranked:
        if total <= max_bytes:
            break

        content = str(block.get("content", ""))
        if len(content) <= PERSIST_THRESHOLD:
            continue

        block["content"] = persist_large_output(block.get("tool_use_id", "unknown"), content)
        total = sum(len(str(block.get("content", ""))) for _, block in blocks)

    return messages


def apply_lightweight_compaction(
    messages: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    """执行不需要额外 LLM 调用的三层压缩：L3 -> L1 -> L2。"""
    messages = tool_result_budget(messages)
    messages = snip_compact(messages)
    return micro_compact(messages)


# L4: autoCompact - 仍然超限时，用模型把完整历史总结成一条可延续上下文。
def write_transcript(messages: list[dict[str, Any]]) -> Path:
    """把压缩前完整对话写成 jsonl，方便之后排查或恢复上下文。"""
    TRANSCRIPT_DIR.mkdir(parents=True, exist_ok=True)
    path = TRANSCRIPT_DIR / f"transcript_{int(time.time())}.jsonl"
    with path.open("w") as file:
        for message in messages:
            file.write(json.dumps(message, ensure_ascii=False, default=str) + "\n")
    return path


def summarize_history(
    messages: list[dict[str, Any]], client: anthropic.Anthropic | None = None
) -> str:
    """调用模型总结历史，保留继续工作的关键信息。"""
    conversation = json.dumps(messages, ensure_ascii=False, default=str)[:80_000]
    prompt = (
        "Summarize this coding-agent conversation so work can continue.\n"
        "Preserve: 1. current goal, 2. key findings/decisions, 3. files read/changed, "
        "4. remaining work, 5. user constraints.\nBe compact but concrete.\n\n"
        + conversation
    )
    active_client = client or _get_client()
    response = active_client.messages.create(
        model=MODEL,
        messages=[{"role": "user", "content": prompt}],
        max_tokens=2000,
    )
    summary = "\n".join(
        getattr(block, "text", "")
        for block in response.content
        if getattr(block, "type", None) == "text"
    ).strip()
    return summary or "(empty summary)"


def compact_history(messages: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """保存 transcript 后，把整段历史压成一条 summary 消息。"""
    transcript_path = write_transcript(messages)
    print(f"[transcript saved: {transcript_path}]")
    summary = summarize_history(messages)
    return [{"role": "user", "content": f"[Compacted]\n\n{summary}"}]


def maybe_compact_history(messages: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """先做轻量压缩；如果仍超过阈值，再升级到 LLM 摘要压缩。"""
    messages = apply_lightweight_compaction(messages)
    if estimate_size(messages) <= CONTEXT_LIMIT:
        return messages
    return compact_history(messages)


# Emergency: reactiveCompact - API 已经因为上下文超限失败时使用。
def reactive_compact(messages: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """保留完整 transcript、生成摘要，并带上最近几条消息继续重试。"""
    transcript_path = write_transcript(messages)
    print(f"[reactive transcript saved: {transcript_path}]")
    summary = summarize_history(messages)

    tail_start = max(0, len(messages) - 5)
    if (
        tail_start > 0
        and tail_start < len(messages)
        and _is_tool_result_message(messages[tail_start])
        and _message_has_tool_use(messages[tail_start - 1])
    ):
        tail_start -= 1

    return [
        {"role": "user", "content": f"[Reactive compact]\n\n{summary}"},
        *messages[tail_start:],
    ]
