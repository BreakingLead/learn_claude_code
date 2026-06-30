import os
from pathlib import Path

MODEL = "deepseek-v4-pro"
WORKDIR = Path.cwd()
SYSTEM = f"You are a coding agent at {os.getcwd()}. Use bash to solve tasks. Act, don't explain."
