from pathlib import Path


def _default_skills_dir(workdir: Path | None = None) -> Path:
    return (workdir or Path.cwd()) / ".agents" / "skills"


def _parse_frontmatter(raw: str) -> tuple[dict[str, str], str]:
    lines = raw.splitlines()
    if not lines or lines[0].strip() != "---":
        return {}, raw

    try:
        end = next(i for i, line in enumerate(lines[1:], start=1) if line.strip() == "---")
    except StopIteration:
        return {}, raw

    meta: dict[str, str] = {}
    i = 1
    while i < end:
        line = lines[i]
        if ":" not in line:
            i += 1
            continue

        key, value = line.split(":", 1)
        key = key.strip()
        value = value.strip()
        if value == "|":
            block: list[str] = []
            i += 1
            while i < end and (lines[i].startswith(" ") or not lines[i].strip()):
                block.append(lines[i].strip())
                i += 1
            meta[key] = " ".join(part for part in block if part)
            continue

        meta[key] = value.strip("\"'")
        i += 1

    return meta, "\n".join(lines[end + 1 :])


def scan_skills(skills_dir: Path | None = None) -> dict[str, dict[str, str]]:
    found: dict[str, dict[str, str]] = {}
    directory = skills_dir or _default_skills_dir()
    if not directory.exists():
        return found

    for skill_dir in sorted(directory.iterdir()):
        if not skill_dir.is_dir():
            continue

        manifest = skill_dir / "SKILL.md"
        if not manifest.exists():
            continue

        raw = manifest.read_text()
        meta, body = _parse_frontmatter(raw)
        name = meta.get("name", skill_dir.name)
        description = meta.get("description", body.split("\n")[0].lstrip("#").strip())
        found[name] = {"name": name, "description": description, "content": raw}

    return found


def list_skills(skills: dict[str, dict[str, str]] | None = None) -> str:
    registry = skills if skills is not None else scan_skills()
    return "\n".join(
        f"- **{skill['name']}**: {skill['description']}"
        for skill in registry.values()
    )


def build_system(
    workdir: Path | None = None,
    skills: dict[str, dict[str, str]] | None = None,
) -> str:
    current_workdir = workdir or Path.cwd()
    catalog = list_skills(skills)
    return (
        f"You are a coding agent at {current_workdir}. "
        f"Skills available:\n{catalog}\n"
        "Use load_skill to get full details when needed."
    )


def get_system_prompt(workdir: Path | None = None) -> str:
    return build_system(workdir=workdir, skills=scan_skills(_default_skills_dir(workdir)))


def load_skill(name: str, skills: dict[str, dict[str, str]] | None = None) -> str:
    registry = skills if skills is not None else scan_skills()
    skill = registry.get(name)
    if not skill:
        return f"Skill not found: {name}"
    return skill["content"]
