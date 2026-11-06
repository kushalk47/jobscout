"""Email alert after each submitted application, so you can track them in Gmail.

Subjects start with "[Wellfound] Applied:" so a Gmail filter can label them.
Run `uv run wellfound-mcp-test-email` to check the setup.
"""

import smtplib
import ssl
import sys
from datetime import datetime
from email.message import EmailMessage

from . import config


def enabled() -> bool:
    return bool(config.EMAIL_TO and config.SMTP_PASSWORD)


def build_message(job: dict, answers: list[dict]) -> EmailMessage:
    title, company = job.get("title") or "Unknown role", job.get("company") or "Unknown company"
    when = datetime.now().strftime("%a %d %b %Y, %H:%M")
    lines = [
        f"You applied to {title} at {company} on Wellfound.",
        "",
        f"When:      {when}",
        f"Link:      {job.get('url') or ''}",
        f"Salary:    {job.get('salary') or 'not listed'}",
        f"Location:  {job.get('location') or 'not listed'}",
        f"Job id:    {job.get('id') or ''}",
    ]
    if answers:
        lines += ["", "Your answers:"]
        for a in answers:
            lines += ["", f"Q: {a['question']}", f"A: {a['answer'] or '(left blank)'}"]
    lines += ["", "Track all applications: https://wellfound.com/jobs/applications",
              "", "Sent by wellfound-mcp."]

    msg = EmailMessage()
    msg["Subject"] = f"[Wellfound] Applied: {title} at {company}"
    msg["From"] = config.SMTP_USER
    msg["To"] = config.EMAIL_TO
    msg.set_content("\n".join(lines))
    return msg


def send(msg: EmailMessage) -> None:
    with smtplib.SMTP_SSL(config.SMTP_HOST, config.SMTP_PORT, context=ssl.create_default_context(), timeout=20) as s:
        s.login(config.SMTP_USER, config.SMTP_PASSWORD)
        s.send_message(msg)


def application_sent(job: dict, answers: list[dict]) -> str:
    """Send the alert. Returns a short status; never raises (the application already went through)."""
    if not enabled():
        return "off (set WELLFOUND_EMAIL_TO and WELLFOUND_SMTP_PASSWORD in .env)"
    try:
        send(build_message(job, answers))
        return f"sent to {config.EMAIL_TO}"
    except smtplib.SMTPAuthenticationError:
        return "failed: Gmail rejected the login. Use an app password, not your normal password."
    except Exception as e:  # noqa: BLE001 - report, don't fail the tool
        return f"failed: {type(e).__name__}: {e}"


def application_held(job: dict, answers: list[dict], reason: str) -> str:
    """Email a draft that was NOT sent, so the user can decide. Never raises."""
    if not enabled():
        return "off (set WELLFOUND_EMAIL_TO and WELLFOUND_SMTP_PASSWORD in .env)"
    title, company = job.get("title") or "Unknown role", job.get("company") or "Unknown company"
    lines = [f"An application to {title} at {company} was NOT sent. It needs your review.", "",
             f"Why it was held: {reason}", "",
             f"Link:      {job.get('url') or ''}",
             f"Salary:    {job.get('salary') or 'not listed'}",
             f"Location:  {job.get('location') or 'not listed'}",
             f"Job id:    {job.get('id') or ''}"]
    if answers:
        lines += ["", "Draft answers:"]
        for a in answers:
            lines += ["", f"Q: {a['question']}", f"A: {a['answer'] or '(left blank)'}"]
    lines += ["", "To send it, open Claude Code and say e.g. \"send the held application to "
              f"{company}\" (or edit the answer first). To drop it, ask Claude to skip it.",
              "", "Sent by wellfound-mcp."]
    msg = EmailMessage()
    msg["Subject"] = f"[Wellfound] Needs review: {title} at {company}"
    msg["From"] = config.SMTP_USER
    msg["To"] = config.EMAIL_TO
    msg.set_content("\n".join(lines))
    try:
        send(msg)
        return f"sent to {config.EMAIL_TO}"
    except Exception as e:  # noqa: BLE001
        return f"failed: {type(e).__name__}: {e}"


def test_main() -> None:
    """`uv run wellfound-mcp-test-email`: send a sample alert to check the setup."""
    if not enabled():
        print("Email alerts are off. Copy .env.example to .env and fill in WELLFOUND_EMAIL_TO "
              "and WELLFOUND_SMTP_PASSWORD.", file=sys.stderr)
        sys.exit(1)
    sample = {"id": "0-test", "title": "Test alert", "company": "wellfound-mcp",
              "url": "https://wellfound.com", "salary": "n/a", "location": "n/a"}
    status = application_sent(sample, [{"question": "Is this working?", "answer": "Yes, setup works."}])
    print(status)
    sys.exit(0 if status.startswith("sent") else 1)
