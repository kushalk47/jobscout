"""Weekly email report over Wellfound and jobscout (LinkedIn), sent on Sundays.

Scheduled by a systemd user timer on Linux, or Task Scheduler on Windows.

  uv run wellfound-mcp-weekly-report            send this week's report (once per week)
  uv run wellfound-mcp-weekly-report --dry-run  print it instead of sending
  uv run wellfound-mcp-weekly-report --force    send even if this week's already went
  uv run wellfound-mcp-weekly-report --install  set up the Sunday timer
  uv run wellfound-mcp-weekly-report --uninstall

The timer is Persistent, so if the computer is off on Sunday evening the
report goes out the next time you log in.
"""

import os
import shutil
import subprocess
import sys
from datetime import date, timedelta
from email.message import EmailMessage
from pathlib import Path

from . import config, dashboard, notify

SENT_MARKER = "last-weekly-report"  # holds the ISO week last reported, e.g. 2026-W45
UNIT = "wellfound-weekly-report"
WHEN = "Sun *-*-* 18:00:00"


def build(today: date | None = None) -> EmailMessage:
    today = today or date.today()
    start = today - timedelta(days=6)
    s = dashboard.stats()
    in_week = [r for r in s["rows"] if start.isoformat() <= r["updated_at"][:10] <= today.isoformat()]
    sent = [r for r in in_week if r["status"] == "submitted"]
    skipped = [r for r in in_week if r["status"] == "skipped"]
    pending = [r for r in s["rows"] if r["status"] == "drafted"]
    held = [r for r in s["rows"] if r["status"] == "held"]
    shortlisted = [r for r in s["rows"] if r["status"] == "shortlisted"]
    span = f"{start:%d %b} – {today:%d %b %Y}"

    lines = [f"Your job-hunt week, {span}", ""]
    lines += [f"Applied this week:   {len(sent)}",
              f"  LinkedIn / career sites: {sum(r['source'] == 'linkedin' for r in sent)}",
              f"  Wellfound:               {sum(r['source'] == 'wellfound' for r in sent)}",
              f"Applied in total:    {s['totals']['submitted']}",
              f"Shortlisted, not applied: {len(shortlisted)}",
              f"Skipped this week:   {len(skipped)}",
              f"Held for review:     {len(held)}",
              f"Drafted, not sent:   {len(pending)}",
              f"Wellfound daily cap: {s['cap']['limit_today']} (max {s['cap_max']})"]

    if sent:
        lines += ["", "Applications sent"]
        for r in sorted(sent, key=lambda r: r["updated_at"]):
            lines += [f"  {r['updated_at'][:10]}  {r['title'] or r['job_id']} at {r['company'] or '?'}  ({r['source']})",
                      f"              {r['url']}"]
    else:
        lines += ["", "No applications sent this week."]

    if held:
        lines += ["", "Held for your review (not sent)"]
        for r in held[:10]:
            lines += [f"  {r['title'] or r['job_id']} at {r['company'] or '?'}  ({r['notes'] or 'no reason'})",
                      f"              {r['url']}"]

    if pending:
        lines += ["", "Drafted but not sent (finish or skip these)"]
        lines += [f"  {r['title'] or r['job_id']} at {r['company'] or '?'}  {r['url']}" for r in pending[:10]]

    if skipped:
        reasons: dict[str, int] = {}
        for r in skipped:
            reasons[r["notes"] or "No reason given"] = reasons.get(r["notes"] or "No reason given", 0) + 1
        lines += ["", "Why jobs were skipped"]
        lines += [f"  {n}× {why}" for why, n in sorted(reasons.items(), key=lambda kv: -kv[1])]

    lines += ["", "Wellfound recruiter replies: https://wellfound.com/jobs/applications",
              "Full stats: run `uv run wellfound-mcp-dashboard`", "", "Sent by hireloop."]

    msg = EmailMessage()
    msg["Subject"] = f"[hireloop] Weekly report: {len(sent)} applied ({span})"
    msg["From"] = config.SMTP_USER
    msg["To"] = config.EMAIL_TO
    msg.set_content("\n".join(lines))
    return msg


def _week(today: date) -> str:
    y, w, _ = today.isocalendar()
    return f"{y}-W{w:02d}"


def send_weekly(force: bool = False, today: date | None = None) -> str:
    today = today or date.today()
    marker = config.DATA_DIR / SENT_MARKER
    if not force and marker.exists() and marker.read_text(encoding="utf-8").strip() == _week(today):
        return "already sent this week"
    if not notify.enabled():
        return "off (set WELLFOUND_EMAIL_TO and WELLFOUND_SMTP_PASSWORD in .env)"
    try:
        notify.send(build(today))
    except Exception as e:  # noqa: BLE001
        return f"failed: {type(e).__name__}: {e}"
    config.DATA_DIR.mkdir(parents=True, exist_ok=True)
    marker.write_text(_week(today), encoding="utf-8")
    return f"sent to {config.EMAIL_TO}"


# ---------- systemd user timer ----------

def _unit_dir() -> Path:
    return Path.home() / ".config/systemd/user"


TASK = "hireloop-weekly-report"


def _install_windows() -> None:
    """Task Scheduler: Sundays 18:00, and catch up at next logon if the PC was off."""
    uv = shutil.which("uv") or "uv"
    ps = (f"$a = New-ScheduledTaskAction -Execute '{uv}' "
          f"-Argument 'run --directory \"{config.PROJECT_ROOT}\" wellfound-mcp-weekly-report'; "
          "$t = New-ScheduledTaskTrigger -Weekly -DaysOfWeek Sunday -At 18:00; "
          "$s = New-ScheduledTaskSettingsSet -StartWhenAvailable; "
          f"Register-ScheduledTask -TaskName '{TASK}' -Action $a -Trigger $t -Settings $s -Force | Out-Null")
    subprocess.run(["powershell", "-NoProfile", "-Command", ps], check=True)
    print(f"Installed Task Scheduler task '{TASK}' (Sundays 18:00).")


def _uninstall_windows() -> None:
    subprocess.run(["powershell", "-NoProfile", "-Command",
                    f"Unregister-ScheduledTask -TaskName '{TASK}' -Confirm:$false"])
    print("Removed the weekly report task.")


def install() -> None:
    if os.name == "nt":
        return _install_windows()
    uv = shutil.which("uv") or "uv"
    d = _unit_dir()
    d.mkdir(parents=True, exist_ok=True)
    (d / f"{UNIT}.service").write_text(f"""[Unit]
Description=Wellfound MCP weekly email report

[Service]
Type=oneshot
ExecStart={uv} run --directory {config.PROJECT_ROOT} wellfound-mcp-weekly-report
""")
    (d / f"{UNIT}.timer").write_text(f"""[Unit]
Description=Send the Wellfound weekly report on Sundays

[Timer]
OnCalendar={WHEN}
Persistent=true

[Install]
WantedBy=timers.target
""")
    subprocess.run(["systemctl", "--user", "daemon-reload"], check=True)
    subprocess.run(["systemctl", "--user", "enable", "--now", f"{UNIT}.timer"], check=True)
    print(f"Installed. Next run:")
    subprocess.run(["systemctl", "--user", "list-timers", f"{UNIT}.timer", "--no-pager"])


def uninstall() -> None:
    if os.name == "nt":
        return _uninstall_windows()
    subprocess.run(["systemctl", "--user", "disable", "--now", f"{UNIT}.timer"])
    for ext in ("service", "timer"):
        (_unit_dir() / f"{UNIT}.{ext}").unlink(missing_ok=True)
    subprocess.run(["systemctl", "--user", "daemon-reload"])
    print("Removed the weekly report timer.")


def main() -> None:
    args = set(sys.argv[1:])
    if "--install" in args:
        install()
    elif "--uninstall" in args:
        uninstall()
    elif "--dry-run" in args:
        msg = build()
        print(f"Subject: {msg['Subject']}\nTo: {msg['To']}\n\n{msg.get_content()}")
    else:
        status = send_weekly(force="--force" in args)
        print(status)
        sys.exit(1 if status.startswith(("failed", "off")) else 0)
