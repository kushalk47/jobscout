"""profile.md: your background and standard answers, read once per session.

Answers to questions the profile didn't cover are appended under
"## Saved answers", so each new question only has to be asked once.
"""

from datetime import date

from . import config

SAVED = "## Saved answers"

TEMPLATE = f"""# Profile

Fill this in once. Claude answers application questions from it, and asks
you whenever something isn't covered. Keep it short: it's read every session.

## Basics
- Name:
- Email:
- Phone:
- Location (city, country):
- LinkedIn:
- GitHub:
- Portfolio / website:

## Work
- Current role and company:
- Years of experience:
- Main skills / stack:
- Highlights (2–4 bullets with numbers if you have them):

## Preferences
- Roles you want:
- Remote / hybrid / onsite:
- Willing to relocate:
- Needs visa sponsorship:
- Expected salary:
- Notice period / earliest start:

## Standard answers
- Why are you looking?:
- What are you looking for in your next role?:

{SAVED}
"""


def read() -> dict:
    p = config.PROFILE_PATH
    if not p.exists():
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(TEMPLATE)
        return {"path": str(p), "created": True, "profile": None,
                "next": "A blank profile was created. Ask the user to fill it in before applying."}
    return {"path": str(p), "profile": p.read_text().strip()}


def save_answer(question: str, answer: str) -> dict:
    p = config.PROFILE_PATH
    text = p.read_text() if p.exists() else TEMPLATE
    if SAVED not in text:
        text = text.rstrip() + f"\n\n{SAVED}\n"
    entry = f"- **{question.strip()}** ({date.today().isoformat()}): {answer.strip()}\n"
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(text.rstrip("\n") + "\n" + entry)
    return {"saved": True, "path": str(p)}
