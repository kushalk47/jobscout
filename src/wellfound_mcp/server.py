"""MCP server: five small tools that return compact JSON.

Talks to Claude over stdio, so never print() to stdout here. Log to stderr.
"""

import logging
import sys
from contextlib import asynccontextmanager

from mcp.server.fastmcp import FastMCP

from . import config, store, wellfound
from .browser import Browser

logging.basicConfig(stream=sys.stderr, level=logging.INFO)
browser = Browser()


@asynccontextmanager
async def lifespan(_server: FastMCP):
    try:
        yield
    finally:
        await browser.close()


mcp = FastMCP("wellfound", lifespan=lifespan)


@mcp.tool()
async def check_login() -> dict:
    """Check whether the saved browser session is logged in to Wellfound."""
    ok = await wellfound.is_logged_in(browser)
    return {"logged_in": ok, "hint": None if ok else "Run `uv run wellfound-mcp-login` and sign in."}


@mcp.tool()
async def search_jobs(role: str, location: str | None = None, remote: bool = False, limit: int = 10) -> list[dict]:
    """Search Wellfound jobs. Returns compact rows (id, title, company, salary, one-line summary).
    Jobs already in the local log are marked with their status."""
    rows = await wellfound.search_jobs(browser, role, location, remote, min(limit, 25))
    for r in rows:
        if seen := store.get(r["id"]):
            r["already"] = seen["status"]
    return rows


@mcp.tool()
async def get_job(job_id: str) -> dict:
    """Get one job's details: requirements, stack, compensation, trimmed description."""
    return await wellfound.get_job(browser, job_id)


@mcp.tool()
async def draft_application(job_id: str) -> dict:
    """List the questions on a job's application form, so answers can be written.
    Nothing is submitted."""
    if (seen := store.get(job_id)) and seen["status"] == "submitted":
        return {"error": "Already applied to this job.", "on": seen["updated_at"]}
    questions = await wellfound.application_questions(browser, job_id)
    store.record(job_id, "drafted")
    return {"job_id": job_id, "questions": questions}


@mcp.tool()
async def submit_application(job_id: str, answers: dict[str, str], confirm: bool = False) -> dict:
    """Submit an application. With confirm=False (default) this only returns a preview.
    Only call it with confirm=True after the user has explicitly approved this exact preview."""
    if (seen := store.get(job_id)) and seen["status"] == "submitted":
        return {"error": "Already applied to this job.", "on": seen["updated_at"]}
    if not confirm:
        return {"preview": True, "job_id": job_id, "answers": answers,
                "next": "Show this to the user. Resubmit with confirm=True only if they approve."}
    if store.submitted_today() >= config.DAILY_APPLY_LIMIT:
        return {"error": f"Daily limit of {config.DAILY_APPLY_LIMIT} submissions reached."}
    await wellfound.submit(browser, job_id, answers)
    store.record(job_id, "submitted")
    return {"submitted": True, "job_id": job_id}


@mcp.tool()
def list_applications(status: str | None = None, limit: int = 50) -> list[dict]:
    """List jobs from the local log. status: drafted | submitted | skipped."""
    return store.list_all(status, limit)


def main() -> None:
    mcp.run()


if __name__ == "__main__":
    main()
