"""Local SQLite log of applications, so nothing gets applied to twice."""

import sqlite3
from datetime import date, datetime

from . import config

SCHEMA = """
CREATE TABLE IF NOT EXISTS applications (
    job_id     TEXT PRIMARY KEY,
    title      TEXT,
    company    TEXT,
    url        TEXT,
    status     TEXT NOT NULL,      -- drafted | submitted | skipped
    updated_at TEXT NOT NULL,
    notes      TEXT
);
-- Days Wellfound said its limit on active applications was reached.
CREATE TABLE IF NOT EXISTS limit_hits (day TEXT PRIMARY KEY);
"""


def _conn() -> sqlite3.Connection:
    config.DATA_DIR.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(config.DB_PATH)
    conn.row_factory = sqlite3.Row
    conn.executescript(SCHEMA)
    return conn


def record(job_id: str, status: str, title: str = "", company: str = "", url: str = "", notes: str = "") -> None:
    with _conn() as c:
        c.execute(
            """INSERT INTO applications (job_id, title, company, url, status, updated_at, notes)
               VALUES (?, ?, ?, ?, ?, ?, ?)
               ON CONFLICT(job_id) DO UPDATE SET
                 status = excluded.status, updated_at = excluded.updated_at,
                 title = COALESCE(NULLIF(excluded.title, ''), title),
                 company = COALESCE(NULLIF(excluded.company, ''), company),
                 url = COALESCE(NULLIF(excluded.url, ''), url),
                 notes = COALESCE(NULLIF(excluded.notes, ''), notes)""",
            (job_id, title, company, url, status, datetime.now().isoformat(timespec="seconds"), notes),
        )


def get(job_id: str) -> dict | None:
    with _conn() as c:
        row = c.execute("SELECT * FROM applications WHERE job_id = ?", (job_id,)).fetchone()
    return dict(row) if row else None


def list_all(status: str | None = None, limit: int = 50) -> list[dict]:
    q, args = "SELECT job_id, title, company, status, updated_at FROM applications", []
    if status:
        q += " WHERE status = ?"
        args.append(status)
    q += " ORDER BY updated_at DESC LIMIT ?"
    args.append(limit)
    with _conn() as c:
        return [dict(r) for r in c.execute(q, args)]


def submitted_today() -> int:
    with _conn() as c:
        return c.execute(
            "SELECT COUNT(*) FROM applications WHERE status = 'submitted' AND updated_at LIKE ?",
            (f"{date.today().isoformat()}%",),
        ).fetchone()[0]


def record_limit_hit() -> None:
    with _conn() as c:
        c.execute("INSERT OR IGNORE INTO limit_hits (day) VALUES (?)", (date.today().isoformat(),))


def daily_limit() -> dict:
    """Today's cap: DAILY_START plus one for each clean past day of applying since
    the last time Wellfound's limit was hit, up to DAILY_MAX."""
    today = date.today().isoformat()
    with _conn() as c:
        last_hit = c.execute("SELECT MAX(day) FROM limit_hits").fetchone()[0] or ""
        clean_days = c.execute(
            """SELECT COUNT(DISTINCT substr(updated_at, 1, 10)) FROM applications
               WHERE status = 'submitted' AND substr(updated_at, 1, 10) > ?
                 AND substr(updated_at, 1, 10) < ?""",
            (last_hit, today),
        ).fetchone()[0]
    limit = min(config.DAILY_MAX, config.DAILY_START + clean_days)
    return {"submitted_today": submitted_today(), "limit_today": limit}


def delete(job_id: str) -> None:
    with _conn() as c:
        c.execute("DELETE FROM applications WHERE job_id = ?", (job_id,))
