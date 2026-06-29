#!/usr/bin/env python3
"""Parse a single line from a JSONL proxy log and output human-readable markdown."""

import json
import sys
import argparse
import textwrap


def try_parse_json(s):
    """Try to parse a string as JSON; return parsed object or None."""
    if not isinstance(s, str):
        return None
    s = s.strip()
    if s and s[0] in ('{', '[', '"'):
        try:
            return json.loads(s)
        except (json.JSONDecodeError, ValueError):
            return None
    return None


def truncate(s, maxlen=200):
    if len(s) <= maxlen:
        return s
    return s[:maxlen] + f"... ({len(s)} chars total)"


def redact_auth(s):
    """Redact authorization header values."""
    if isinstance(s, str) and len(s) > 15:
        return s[:15] + "..."
    return s


def format_headers(headers, indent=0):
    """Format HTTP headers as a markdown table."""
    if not headers:
        return ""
    lines = ["\n| Header | Value |", "|--------|-------|"]
    sensitive = {"authorization", "cookie", "x-api-key"}
    for k, v in sorted(headers.items()):
        display_v = redact_auth(v) if k.lower() in sensitive else v
        # escape pipes in values
        display_v = str(display_v).replace("|", "\\|")
        lines.append(f"| `{k}` | {display_v} |")
    return "\n".join(lines)


def format_content_block(block, block_idx):
    """Format a single content block (text, tool_use, tool_result, image, etc.)."""
    btype = block.get("type", "unknown")
    lines = [f"\n##### Block {block_idx} — `{btype}`\n"]

    if btype == "text":
        text = block.get("text", "")
        # try to parse text as JSON (double-encoded content)
        parsed = try_parse_json(text)
        if parsed:
            lines.append("```json\n" + json.dumps(parsed, indent=2, ensure_ascii=False)[:3000] + "\n```")
        elif len(text) > 2000:
            lines.append(f"<details><summary>Text content ({len(text)} chars)</summary>\n")
            lines.append("```\n" + text[:5000])
            if len(text) > 5000:
                lines.append(f"\n... truncated ({len(text)} chars total)")
            lines.append("\n```\n</details>")
        else:
            lines.append("```\n" + text + "\n```")

    elif btype == "tool_use":
        lines.append(f"- **Tool:** `{block.get('name', '?')}`")
        lines.append(f"- **ID:** `{block.get('id', '?')}`")
        inp = block.get("input", {})
        lines.append("\n**Input:**\n```json\n" + json.dumps(inp, indent=2, ensure_ascii=False)[:3000] + "\n```")

    elif btype == "tool_result":
        lines.append(f"- **Tool Use ID:** `{block.get('tool_use_id', '?')}`")
        content = block.get("content", "")
        if isinstance(content, str):
            lines.append("```\n" + truncate(content, 2000) + "\n```")
        elif isinstance(content, list):
            for sub in content:
                lines.append(format_content_block(sub, block_idx))

    elif btype == "image":
        src = block.get("source", {})
        lines.append(f"- **Media type:** `{src.get('media_type', '?')}`")
        data = src.get("data", "")
        lines.append(f"- **Data:** ({len(data)} chars base64)")

    elif btype == "thinking":
        text = block.get("thinking", "")
        lines.append(f"<details><summary>Thinking ({len(text)} chars)</summary>\n")
        lines.append("```\n" + text[:3000])
        if len(text) > 3000:
            lines.append(f"\n... truncated ({len(text)} chars total)")
        lines.append("\n```\n</details>")

    else:
        lines.append("```json\n" + json.dumps(block, indent=2, ensure_ascii=False)[:2000] + "\n```")

    cache = block.get("cache_control")
    if cache:
        lines.append(f"- **Cache control:** `{json.dumps(cache)}`")

    return "\n".join(lines)


def format_message(msg, msg_idx):
    """Format a single message (role + content blocks)."""
    role = msg.get("role", "unknown")
    lines = [f"\n#### Message {msg_idx} — `{role}`\n"]

    content = msg.get("content", "")

    if isinstance(content, str):
        parsed = try_parse_json(content)
        if parsed:
            lines.append("```json\n" + json.dumps(parsed, indent=2, ensure_ascii=False)[:3000] + "\n```")
        elif len(content) > 2000:
            lines.append(f"<details><summary>Content ({len(content)} chars)</summary>\n")
            lines.append("```\n" + content[:5000])
            if len(content) > 5000:
                lines.append(f"\n... truncated ({len(content)} chars total)")
            lines.append("\n```\n</details>")
        else:
            lines.append("```\n" + content + "\n```")
    elif isinstance(content, list):
        lines.append(f"{len(content)} content block(s):\n")
        for i, block in enumerate(content):
            lines.append(format_content_block(block, i))
    return "\n".join(lines)


def format_tools(tools):
    """Format the tools field."""
    if isinstance(tools, str):
        # often just "[29 tools]" placeholder
        return f"**Tools:** {tools}\n"

    if isinstance(tools, list):
        lines = [f"**Tools:** {len(tools)} tool(s)\n"]
        lines.append("<details><summary>Tool list</summary>\n")
        for t in tools:
            name = t.get("name", "?")
            desc = t.get("description", "")
            first_line = desc.split("\n")[0][:120] if desc else ""
            lines.append(f"- `{name}` — {first_line}")
        lines.append("\n</details>")
        return "\n".join(lines)

    return f"**Tools:** `{json.dumps(tools)}`\n"


def format_entry(data):
    """Format a full JSONL entry into markdown."""
    entry_type = data.get("type", "unknown")
    lines = [f"# Log Entry: `{entry_type}`\n"]

    # Top-level metadata
    meta_fields = [
        ("requestId", "Request ID"),
        ("timestamp", "Timestamp"),
        ("service", "Service"),
        ("keyId", "Key ID"),
        ("clientIp", "Client IP"),
        ("method", "Method"),
        ("path", "Path"),
        ("status", "Status"),
        ("model", "Model"),
    ]
    lines.append("## Metadata\n")
    for key, label in meta_fields:
        if key in data:
            lines.append(f"- **{label}:** `{data[key]}`")

    # Usage
    usage = data.get("usage")
    if usage:
        lines.append("\n## Usage\n")
        for k, v in usage.items():
            lines.append(f"- **{k}:** `{v}`")

    # Headers
    headers = data.get("headers")
    if headers:
        lines.append("\n## Headers\n")
        lines.append(format_headers(headers))

    # System prompt
    system = data.get("system")
    if system:
        lines.append("\n## System Prompt\n")
        if isinstance(system, str):
            lines.append("```\n" + truncate(system, 3000) + "\n```")
        elif isinstance(system, list):
            lines.append(f"{len(system)} block(s):\n")
            for i, block in enumerate(system):
                lines.append(format_content_block(block, i))

    # Messages
    messages = data.get("messages")
    if messages:
        lines.append(f"\n## Messages ({len(messages)})\n")
        for i, msg in enumerate(messages):
            lines.append(format_message(msg, i))

    # Tools
    tools = data.get("tools")
    if tools is not None:
        lines.append("\n## Tools\n")
        lines.append(format_tools(tools))

    # Content (for response entries with top-level content)
    content = data.get("content")
    if content and not messages:
        lines.append("\n## Content\n")
        parsed = try_parse_json(content)
        if parsed:
            lines.append("```json\n" + json.dumps(parsed, indent=2, ensure_ascii=False) + "\n```")
        elif isinstance(content, str):
            lines.append("```\n" + content + "\n```")
        elif isinstance(content, list):
            for i, block in enumerate(content):
                lines.append(format_content_block(block, i))

    # Stream flag
    if "stream" in data:
        lines.append(f"\n**Stream:** `{data['stream']}`")

    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description="Parse a JSONL line into readable markdown")
    parser.add_argument("file", help="Path to the .jsonl file")
    parser.add_argument("line", type=int, help="Line number (1-based)")
    parser.add_argument("-o", "--output", help="Output .md file (default: stdout)")
    args = parser.parse_args()

    with open(args.file, "r", encoding="utf-8") as f:
        for i, raw_line in enumerate(f, 1):
            if i == args.line:
                break
        else:
            print(f"Error: file has fewer than {args.line} lines", file=sys.stderr)
            sys.exit(1)

    data = json.loads(raw_line)
    md = format_entry(data)

    if args.output:
        with open(args.output, "w", encoding="utf-8") as f:
            f.write(md + "\n")
        print(f"Written to {args.output}", file=sys.stderr)
    else:
        print(md)


if __name__ == "__main__":
    main()
