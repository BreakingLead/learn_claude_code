from anthropic.types import ContentBlock, ToolUseBlock
from loguru import logger
from rich import print
from rich.prompt import Confirm
from typing_extensions import Literal

from .constants import WORKDIR

DENY_LIST = [
    "rm -rf / --",
    "sudo",
    "shutdown",
    "reboot",
    "mkfs",
    "dd if=",
    "> /dev/",
]


# Gate 1: Deny List
def check_deny_list(command: str) -> str | None:
    for pattern in DENY_LIST:
        if pattern in command:
            return f"Blocked: '{pattern}' is on the deny list"
    return None


# 权限规则列表，每条规则包含：适用工具、检查函数（lambda）、拦截提示
PERMISSION_RULES = [
    {
        "tools": ["write_file", "edit_file"],
        # 将参数中的 path 拼接到工作目录后，resolve() 解析为绝对路径，
        # 再用 is_relative_to() 判断是否仍在 WORKDIR 内；
        # 若不在（not ...），说明试图写入工作区之外，返回 True 触发拦截
        "check": lambda args: (
            not (WORKDIR / args.get("path", "")).resolve().is_relative_to(WORKDIR)
        ),
        "message": "Writing outside workspace",
    },
    {
        "tools": ["bash"],
        # 从参数中取出 command 字符串，用 any() 逐一检查是否包含危险关键词；
        # 只要命中任意一个（"rm "、"> /etc/"、"chmod 777"），返回 True 触发拦截
        "check": lambda args: any(
            kw in args.get("command", "") for kw in ["rm ", "> /etc/", "chmod 777"]
        ),
        "message": "Potentially destructive command",
    },
]


def check_rules(tool_name: str, args: dict) -> str | None:
    """Gate 2: 遍历 PERMISSION_RULES，若当前工具命中某条规则且 check 返回 True，则返回拦截信息"""
    for rule in PERMISSION_RULES:
        if tool_name in rule["tools"] and rule["check"](args):
            return rule["message"]
    return None


def ask_user(tool_name: str, args: dict, reason: str) -> Literal["allow", "deny"]:
    """
    Gate 3: User approval — wait for confirmation after rule match
    """
    logger.warning("⚠  {}: {}({})", reason, tool_name, args)
    allowed = Confirm.ask("   Allow?", default=False)
    return "allow" if allowed else "deny"


# Pipeline: all three gates chained
def permission_hook(block: ToolUseBlock) -> str | None:
    # 1
    if block.name == "bash":
        reason = check_deny_list(block.input.get("command", "").__str__())
        if reason:
            logger.error("Permission denied: {}", reason)
            return "Permission denied by deny list."
    # 2
    reason = check_rules(block.name, block.input)
    if reason is not None:
        # 3
        decision = ask_user(block.name, block.input, reason)
        if decision == "deny":
            return "Permission denied by user."
    return None
