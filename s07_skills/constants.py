from pathlib import Path

from .skills import get_system_prompt

MODEL = "deepseek-v4-flash"
WORKDIR = Path.cwd()
SYSTEM = get_system_prompt(WORKDIR)
