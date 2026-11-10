"""Read-only view of jobscout's LinkedIn database (~/.jobscout/jobscout.db).

jobscout (the Go MCP server) records what happened to each LinkedIn job with
jobs_mark. The dashboard and weekly report show those marks next to the
Wellfound log, in the same row shape as store.applications.
"""

import sqlite3
from datetime import datetime
from pathlib import Path

from . import config

# jobscout status -> dashboard status. "seen" (scored below the shortlist bar) is left out.
STATUS = {
    "applied": "submitted",
    "shortlisted": "shortlisted",
    "skipped": "skipped",
    "not_interested": "skipped",
}

QUERY = """
SELECT m.id, m.status, m.reason, m.updated_at,
       COALESCE(d.title, c.title, '') AS title,
       COALESCE(d.company, c.company, '') AS company,
       COALESCE(d.apply, '') AS apply
FROM marks m
LEFT JOIN details d ON d.id = m.id
LEFT JOIN cards c ON c.id = m.id
WHERE m.status IN ({})
ORDER BY m.updated_at DESC
"""


def rows(db_path: Path | None = None) -> list[dict]:
    """Marked LinkedIn jobs as dashboard rows; [] if jobscout isn't set up."""
    path = db_path or config.JOBSCOUT_DB
    if not path.exists():
        return []
    try:
        # Read-only, so a running jobscout server is never blocked or changed.
        conn = sqlite3.connect(f"{path.resolve().as_uri()}?mode=ro", uri=True)
    except sqlite3.Error:
        return []
    conn.row_factory = sqlite3.Row
    try:
        has_apply = any(r[1] == "apply" for r in conn.execute("PRAGMA table_info(details)"))
        q = QUERY.format(",".join("?" * len(STATUS)))
        if not has_apply:  # databases from before jobscout v0.2
            q = q.replace("COALESCE(d.apply, '')", "''")
        found = conn.execute(q, list(STATUS)).fetchall()
    except sqlite3.Error:
        return []
    finally:
        conn.close()
    out = []
    for r in found:
        apply = r["apply"] if r["apply"].startswith("http") else ""
        out.append({
            "job_id": r["id"],
            "title": r["title"],
            "company": r["company"],
            "url": apply or f"https://www.linkedin.com/jobs/view/{r['id']}/",
            "status": STATUS[r["status"]],
            "updated_at": datetime.fromtimestamp(r["updated_at"]).isoformat(timespec="seconds"),
            "notes": r["reason"] or "",
            "source": "linkedin",
        })
    return out
