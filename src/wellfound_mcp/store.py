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
)
"""


def _conn() -> sqlite3.Connection:
    config.DATA_DIR.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(config.DB_PATH)
    conn.row_factory = sqlite3.Row
    conn.execute(SCHEMA)
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
