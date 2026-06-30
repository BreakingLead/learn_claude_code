import rich
from loguru import logger
from rich.markdown import Markdown

CURRENT_TODOS: list[dict] = []


def run_todo_write(todos: list) -> str:
    global CURRENT_TODOS
    CURRENT_TODOS = todos

    lines = ["\n## Current Tasks"]
    for t in CURRENT_TODOS:
        icon = {"pending": " ", "in_progress": "▸", "completed": "✓"}[t["status"]]
        lines.append(f"- [{icon}] {t['content']}")

    rich.print(Markdown("\n".join(lines)))

    return f"Updated {len(CURRENT_TODOS)} tasks"
