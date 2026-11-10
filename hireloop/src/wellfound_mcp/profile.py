"""profile.md: your background and standard answers, read once per session.

Answers to questions the profile didn't cover are appended under
"## Saved answers", so each new question only has to be asked once.
"""

import json
from datetime import date

from . import config

SAVED = "## Saved answers"
MISSING = {"error": "No profile.md yet.",
           "next": "Ask the user to copy profile.example.md to profile.md in the project folder and fill it in."}


def read() -> dict:
    p = config.PROFILE_PATH
    if not p.exists():
        return {**MISSING, "path": str(p)}
    return {"path": str(p), "profile": p.read_text(encoding="utf-8").strip()}


def save_answer(question: str, answer: str) -> dict:
    p = config.PROFILE_PATH
    if not p.exists():
        return {**MISSING, "path": str(p)}
    text = p.read_text(encoding="utf-8")
    if SAVED not in text:
        text = text.rstrip() + f"\n\n{SAVED}\n"
    entry = f"- **{question.strip()}** ({date.today().isoformat()}): {answer.strip()}\n"
    p.write_text(text.rstrip("\n") + "\n" + entry, encoding="utf-8")
    return {"saved": True, "path": str(p)}


def _flatten(value) -> list[str]:
    if isinstance(value, dict):
        return [s for k, v in value.items() if not str(k).startswith("_") for s in [str(k)] + _flatten(v)]
    if isinstance(value, list):
        return [s for v in value for s in _flatten(v)]
    return [] if value is None else [str(value)]


def facts_text() -> str:
    """profile.md plus every value in jobscout's personal_details.json: what answers may claim."""
    parts = []
    if config.PROFILE_PATH.exists():
        parts.append(config.PROFILE_PATH.read_text(encoding="utf-8"))
    if config.PERSONAL_DETAILS.exists():
        try:
            parts += _flatten(json.loads(config.PERSONAL_DETAILS.read_text(encoding="utf-8")))
        except ValueError:
            pass  # a broken file just means fewer known facts, so more answers get flagged
    return "\n".join(parts)
